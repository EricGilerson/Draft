package deploy

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
)

// draftBuildTagPattern matches the deterministic tag Draft assigns to images it
// builds itself (see draftImageTag: "draft-{project}-{environment}-{service}:{sequence}").
// Pulled images used by image-mode services (datastores, custom images) keep
// their upstream tag untouched and carry no Draft marker at all, so this
// pattern match is the only way to distinguish "Draft built this" from
// "Draft is merely running this" for images specifically — unlike containers,
// volumes, and networks, which always get real draft.* labels at creation.
// The pattern is intentionally loose on the name segment so older tags without
// an environment segment (pre multi-env) still count as Draft-built.
var draftBuildTagPattern = regexp.MustCompile(`^draft-[^:]+:\d+$`)

// ContainerSummary is a single Docker container as seen by the full Docker
// management tab. Unlike ManagedVolume/VolumeOverview this covers every
// container on the daemon, not just Draft's — Managed/ProjectName/NodeLabel
// are populated only when the draft.project/draft.node labels are present
// (see the labels map built in startContainerAndRegister, image_deploy.go).
type ContainerSummary struct {
	ID          string            `json:"id"`
	Names       []string          `json:"names"`
	Image       string            `json:"image"`
	State       string            `json:"state"`
	Status      string            `json:"status"`
	Ports       []container.Port  `json:"ports"`
	Created     int64             `json:"created"`
	SizeRw      int64             `json:"sizeRw"`
	SizeRootFs  int64             `json:"sizeRootFs"`
	Labels      map[string]string `json:"labels"`
	Managed     bool              `json:"managed"`
	ProjectID   uint              `json:"projectId,omitempty"`
	ProjectName string            `json:"projectName,omitempty"`
	NodeID      string            `json:"nodeId,omitempty"`
	NodeLabel   string            `json:"nodeLabel,omitempty"`
}

// ListContainers returns every container on the daemon (running and stopped),
// with per-container disk usage. Containers created by Draft are flagged via
// the draft.node label and, when the owning node still exists, enriched with
// its current label the same way enrichVolumes does for volumes.
func (e *Engine) ListContainers(ctx context.Context) ([]ContainerSummary, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("connect to docker: %w", err)
	}
	defer cli.Close()

	list, err := cli.ContainerList(ctx, container.ListOptions{All: true, Size: true})
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}

	out := make([]ContainerSummary, 0, len(list))
	for _, c := range list {
		cs := ContainerSummary{
			ID:         c.ID,
			Names:      c.Names,
			Image:      c.Image,
			State:      c.State,
			Status:     c.Status,
			Ports:      c.Ports,
			Created:    c.Created,
			SizeRw:     c.SizeRw,
			SizeRootFs: c.SizeRootFs,
			Labels:     c.Labels,
		}
		if nodeID := c.Labels["draft.node"]; nodeID != "" {
			cs.Managed = true
			cs.NodeID = nodeID
			fmt.Sscanf(c.Labels["draft.project"], "%d", &cs.ProjectID)
			if node, err := e.store.GetNode(nodeID); err == nil {
				cs.NodeLabel = node.Label
				if project, err := e.store.GetProject(node.ProjectID); err == nil {
					cs.ProjectName = project.Name
				}
			}
		}
		out = append(out, cs)
	}
	return out, nil
}

// StartContainer, StopContainer, RestartContainer, RemoveContainer act on any
// container on the daemon, not just Draft's — this is the generic Docker tab,
// so no ownership check gates these (unlike DeleteManagedVolume).
func (e *Engine) StartContainer(ctx context.Context, id string) error {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("connect to docker: %w", err)
	}
	defer cli.Close()
	return cli.ContainerStart(ctx, id, container.StartOptions{})
}

func (e *Engine) StopContainer(ctx context.Context, id string) error {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("connect to docker: %w", err)
	}
	defer cli.Close()
	return cli.ContainerStop(ctx, id, container.StopOptions{})
}

func (e *Engine) RestartContainer(ctx context.Context, id string) error {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("connect to docker: %w", err)
	}
	defer cli.Close()
	return cli.ContainerRestart(ctx, id, container.StopOptions{})
}

func (e *Engine) RemoveContainer(ctx context.Context, id string, force bool) error {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("connect to docker: %w", err)
	}
	defer cli.Close()
	return cli.ContainerRemove(ctx, id, container.RemoveOptions{Force: force})
}

// ImageSummary is a single Docker image as seen by the Docker management tab.
// Managed is a best-effort guess based on Draft's deterministic build-tag
// pattern (see draftBuildTagPattern) since pulled images carry no label.
//
// Size is Docker's virtual size (sum of every layer the image is composed of).
// SharedSize is the portion of those layers also used by at least one other
// image (−1 when the daemon did not compute it). Unique reclaimable space for
// a single row is therefore Size−SharedSize when SharedSize ≥ 0 — summing Size
// across rows double-counts shared layers and is never a disk total.
// ParentID lets the UI nest intermediate parent images under their head.
type ImageSummary struct {
	ID         string   `json:"id"`
	ParentID   string   `json:"parentId,omitempty"`
	RepoTags   []string `json:"repoTags"`
	Size       int64    `json:"size"`
	SharedSize int64    `json:"sharedSize"`
	Containers int64    `json:"containers"`
	Created    int64    `json:"created"`
	Dangling   bool     `json:"dangling"`
	Managed    bool     `json:"managed"`
}

// ListImages returns every image on the daemon, including untagged intermediate
// parent images, with Draft-build tags flagged via Managed. All:true matters:
// Docker's default head-only listing can make the UI look like an image ID
// "changed" after delete, when in reality the removed image exposed its
// previously hidden parent as the new top-level row. The UI collapses parents
// under their head via ParentID so the verbose list stays available without
// looking like dozens of independent multi-GB images.
func (e *Engine) ListImages(ctx context.Context) ([]ImageSummary, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("connect to docker: %w", err)
	}
	defer cli.Close()

	list, err := cli.ImageList(ctx, image.ListOptions{All: true, SharedSize: true})
	if err != nil {
		return nil, fmt.Errorf("list images: %w", err)
	}

	out := make([]ImageSummary, 0, len(list))
	for _, img := range list {
		dangling := len(img.RepoTags) == 0 || (len(img.RepoTags) == 1 && img.RepoTags[0] == "<none>:<none>")
		managed := false
		for _, tag := range img.RepoTags {
			if draftBuildTagPattern.MatchString(tag) {
				managed = true
				break
			}
		}
		out = append(out, ImageSummary{
			ID:         img.ID,
			ParentID:   img.ParentID,
			RepoTags:   img.RepoTags,
			Size:       img.Size,
			SharedSize: img.SharedSize,
			Containers: img.Containers,
			Created:    img.Created,
			Dangling:   dangling,
			Managed:    managed,
		})
	}
	return out, nil
}

// RemoveImage removes an image and waits for it to actually disappear before
// returning, following the same pattern as removeImageAndWait (engine.go):
// ImageRemove can report success while the image still briefly appears in
// ImageList/ImageInspect (observed with the containerd-backed image store),
// so a single fire-and-forget call makes deletion look flaky from the UI —
// "succeeded" but the row and the disk usage total don't change. Conflict-class
// errors (image still referenced by a stopped container, or by another tag)
// are retried with force rather than surfaced immediately, since force is the
// whole point of a user-confirmed removal from the management tab.
func (e *Engine) RemoveImage(ctx context.Context, id string, force bool) error {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("connect to docker: %w", err)
	}
	defer cli.Close()
	return removeImageAndConfirm(ctx, cli, id, force, map[string]bool{})
}

// dependentChildImagesMsg is the exact daemon error text Docker uses when an
// image can't be deleted because another image still lists it as a parent
// layer (daemon/images/image_delete.go, checkImageDeleteConflict). Draft's own
// iterative rebuilds produce this constantly: each redeploy retags a new
// image, leaving the previous build's layers behind as an untagged parent
// that the build *before that* may still depend on. Docker documents this as a
// "hard" conflict — force never bypasses it, only removing the dependent
// child first does — so retrying the same call for up to 5 seconds (the old
// behavior) was guaranteed to fail every time and just made removal look
// hung before finally surfacing the error.
const dependentChildImagesMsg = "image has dependent child images"

// removeImageAndConfirm is the shared "remove + poll until actually gone"
// path used by both RemoveImage and PruneImages' Draft-only branch. visited
// guards against pathological cycles while resolving a dependency chain.
func removeImageAndConfirm(ctx context.Context, cli *client.Client, id string, force bool, visited map[string]bool) error {
	if visited[id] {
		return fmt.Errorf("image %s: circular parent/child dependency while resolving images to remove first", id)
	}
	visited[id] = true

	deadline := time.Now().Add(5 * time.Second)
	var lastErr error
	for {
		_, rmErr := cli.ImageRemove(ctx, id, image.RemoveOptions{Force: force})
		if rmErr != nil && strings.Contains(rmErr.Error(), dependentChildImagesMsg) {
			if depErr := removeDependentImages(ctx, cli, id, visited); depErr != nil {
				return fmt.Errorf("can't remove %s: %w", shortImageID(id), depErr)
			}
			lastErr = nil
			continue // dependents are gone now; retry this image immediately
		}
		if rmErr != nil && !force &&
			(errdefs.IsConflict(rmErr) || strings.Contains(rmErr.Error(), "being used by")) {
			// Removing by image ID can require a forced retry even when the user
			// did not explicitly request force: multi-tag images report a
			// repository-reference conflict, and stopped-container references can
			// block a normal remove. The Docker tab already treats confirmation as
			// permission to remove the whole image, not just one tag.
			force = true
			lastErr = rmErr
			continue
		}
		if rmErr != nil &&
			!errdefs.IsNotFound(rmErr) &&
			!errdefs.IsConflict(rmErr) &&
			!strings.Contains(rmErr.Error(), "being used by") {
			return rmErr
		}
		lastErr = rmErr
		if _, _, err := cli.ImageInspectWithRaw(ctx, id); err != nil {
			if errdefs.IsNotFound(err) {
				return nil
			}
			return err
		}
		if time.Now().After(deadline) {
			if lastErr != nil {
				return lastErr
			}
			return fmt.Errorf("image %s still exists after removal", id)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// removeDependentImages finds every image that lists parentID as its parent
// layer. A dependent that is itself untagged and unused (0 containers) is
// just more orphaned history from the same rebuild chain — Draft removes it
// automatically so the caller's retry of parentID can then succeed. But a
// dependent that is still tagged, or in use by a container (running or
// stopped), means parentID isn't orphaned history at all — something real
// still depends on it — so removal stops there and reports exactly what's
// blocking it rather than forcing through a tag or a live container's image.
// Uses All:true because a dependent blocking removal is often an untagged
// intermediate layer, not a "head" image ImageList(All:false) would return.
func removeDependentImages(ctx context.Context, cli *client.Client, parentID string, visited map[string]bool) error {
	list, err := cli.ImageList(ctx, image.ListOptions{All: true})
	if err != nil {
		return fmt.Errorf("list images to resolve dependents of %s: %w", parentID, err)
	}
	for _, img := range list {
		if img.ID == parentID || img.ParentID != parentID {
			continue
		}
		if len(img.RepoTags) > 0 {
			return fmt.Errorf("still needed by %s", img.RepoTags[0])
		}
		if img.Containers > 0 {
			return fmt.Errorf("still needed by a container using image %s", shortImageID(img.ID))
		}
		if err := removeImageAndConfirm(ctx, cli, img.ID, false, visited); err != nil {
			return fmt.Errorf("remove dependent image %s: %w", shortImageID(img.ID), err)
		}
	}
	return nil
}

func shortImageID(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// NetworkSummary is a single Docker network as seen by the Docker management
// tab. Managed reflects the draft.managed=true label stamped by
// ensureDraftNetwork (engine.go).
type NetworkSummary struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Driver     string            `json:"driver"`
	Scope      string            `json:"scope"`
	Created    string            `json:"created"`
	Labels     map[string]string `json:"labels"`
	Containers int               `json:"containers"`
	Managed    bool              `json:"managed"`
}

func (e *Engine) ListNetworks(ctx context.Context) ([]NetworkSummary, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("connect to docker: %w", err)
	}
	defer cli.Close()

	list, err := cli.NetworkList(ctx, network.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list networks: %w", err)
	}

	out := make([]NetworkSummary, 0, len(list))
	for _, n := range list {
		out = append(out, NetworkSummary{
			ID:         n.ID,
			Name:       n.Name,
			Driver:     n.Driver,
			Scope:      n.Scope,
			Created:    n.Created.Format("2006-01-02T15:04:05Z07:00"),
			Labels:     n.Labels,
			Containers: len(n.Containers),
			Managed:    n.Labels["draft.managed"] == "true",
		})
	}
	return out, nil
}

func (e *Engine) RemoveNetwork(ctx context.Context, id string) error {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("connect to docker: %w", err)
	}
	defer cli.Close()
	return cli.NetworkRemove(ctx, id)
}

// ListAllVolumes is ListManagedVolumes without the draft.managed=true filter,
// so foreign (non-Draft) volumes show up too, still enriched the same way as
// ListVolumesOverview for the ones Draft does own.
func (e *Engine) ListAllVolumes(ctx context.Context) ([]VolumeOverview, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("connect to docker: %w", err)
	}
	defer cli.Close()

	listResp, err := cli.VolumeList(ctx, volume.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list volumes: %w", err)
	}

	usageByName := map[string]int64{}
	refCountByName := map[string]int{}
	if du, err := cli.DiskUsage(ctx, types.DiskUsageOptions{}); err == nil {
		for _, v := range du.Volumes {
			if v.UsageData != nil {
				usageByName[v.Name] = v.UsageData.Size
				refCountByName[v.Name] = int(v.UsageData.RefCount)
			}
		}
	}

	vols := make([]ManagedVolume, 0, len(listResp.Volumes))
	for _, v := range listResp.Volumes {
		if v.Name == "" {
			continue
		}
		mv := ManagedVolume{
			Name:       v.Name,
			Labels:     v.Labels,
			Mountpoint: v.Mountpoint,
			Driver:     v.Driver,
			CreatedAt:  v.CreatedAt,
			Size:       usageByName[v.Name],
			RefCount:   refCountByName[v.Name],
		}
		if v.UsageData != nil {
			mv.RefCount = int(v.UsageData.RefCount)
		}
		if v.Labels != nil {
			mv.Target = v.Labels["draft.target"]
			mv.NodeID = v.Labels["draft.node"]
			mv.Environment = v.Labels["draft.environment"]
			fmt.Sscanf(v.Labels["draft.project"], "%d", &mv.ProjectID)
		}
		vols = append(vols, mv)
	}

	out := enrichVolumes(e.store, vols)
	for i := range out {
		out[i].Managed = out[i].Labels["draft.managed"] == "true"
		if out[i].Labels["draft.managed"] != "true" {
			// Not Draft's volume — orphan tracking only makes sense for
			// volumes Draft itself created and labelled.
			out[i].Orphaned = false
		}
	}
	return out, nil
}

// RemoveVolume removes any Docker volume by name, unlike DeleteManagedVolume
// which only allows removal of draft.managed=true volumes. force tears down
// any containers still referencing it first (Docker's own force flag only
// suppresses not-found errors, it won't free an in-use volume).
func (e *Engine) RemoveVolume(ctx context.Context, name string, force bool) error {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("connect to docker: %w", err)
	}
	defer cli.Close()

	if force {
		if err := removeContainersUsingVolume(ctx, cli, name); err != nil {
			return err
		}
	}
	return cli.VolumeRemove(ctx, name, force)
}

// PruneReport is the outcome of a prune call, reported using Docker's own
// SpaceReclaimed accounting (which correctly handles shared image layers)
// rather than a before/after DiskUsage diff computed by Draft.
type PruneReport struct {
	SpaceReclaimed int64    `json:"spaceReclaimed"`
	Removed        []string `json:"removed,omitempty"`
}

// PruneContainers removes stopped containers. draftOnly scopes to
// draft.managed=true (all Draft containers get draft.project/draft.node, but
// not draft.managed, EXCEPT this prune call still only makes sense scoped by
// draft.node presence, so draftOnly instead filters on "label=draft.node").
func (e *Engine) PruneContainers(ctx context.Context, draftOnly bool) (PruneReport, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return PruneReport{}, fmt.Errorf("connect to docker: %w", err)
	}
	defer cli.Close()

	args := filters.NewArgs()
	if draftOnly {
		args.Add("label", "draft.node")
	}
	report, err := cli.ContainersPrune(ctx, args)
	if err != nil {
		return PruneReport{}, err
	}
	return PruneReport{SpaceReclaimed: int64(report.SpaceReclaimed), Removed: report.ContainersDeleted}, nil
}

// PruneImages removes dangling (and, when draftOnly is false, all unused)
// images. Because pulled images carry no Draft label, draftOnly restricts
// removal to images matching Draft's own build-tag pattern; it does so by
// filtering client-side rather than via the Docker API (which has no
// tag-pattern filter) and removing matches individually so the reported
// SpaceReclaimed still comes from Docker itself, not a guess.
func (e *Engine) PruneImages(ctx context.Context, draftOnly bool) (PruneReport, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return PruneReport{}, fmt.Errorf("connect to docker: %w", err)
	}
	defer cli.Close()

	if !draftOnly {
		args := filters.NewArgs()
		args.Add("dangling", "false")
		report, err := cli.ImagesPrune(ctx, args)
		if err != nil {
			return PruneReport{}, err
		}
		deleted := make([]string, 0, len(report.ImagesDeleted))
		for _, d := range report.ImagesDeleted {
			if d.Deleted != "" {
				deleted = append(deleted, d.Deleted)
			}
		}
		return PruneReport{SpaceReclaimed: int64(report.SpaceReclaimed), Removed: deleted}, nil
	}

	list, err := cli.ImageList(ctx, image.ListOptions{All: false})
	if err != nil {
		return PruneReport{}, fmt.Errorf("list images: %w", err)
	}
	var reclaimed int64
	var removed []string
	for _, img := range list {
		matched := false
		for _, tag := range img.RepoTags {
			if draftBuildTagPattern.MatchString(tag) {
				matched = true
				break
			}
		}
		if !matched || img.Containers > 0 {
			continue
		}
		before, _ := cli.DiskUsage(ctx, types.DiskUsageOptions{})
		if err := removeImageAndConfirm(ctx, cli, img.ID, false, map[string]bool{}); err != nil {
			continue
		}
		after, aerr := cli.DiskUsage(ctx, types.DiskUsageOptions{})
		if aerr == nil {
			reclaimed += before.LayersSize - after.LayersSize
		}
		removed = append(removed, img.ID)
	}
	return PruneReport{SpaceReclaimed: reclaimed, Removed: removed}, nil
}

func (e *Engine) PruneNetworks(ctx context.Context, draftOnly bool) (PruneReport, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return PruneReport{}, fmt.Errorf("connect to docker: %w", err)
	}
	defer cli.Close()

	args := filters.NewArgs()
	if draftOnly {
		args.Add("label", "draft.managed=true")
	}
	report, err := cli.NetworksPrune(ctx, args)
	if err != nil {
		return PruneReport{}, err
	}
	return PruneReport{Removed: report.NetworksDeleted}, nil
}

func (e *Engine) PruneVolumes(ctx context.Context, draftOnly bool) (PruneReport, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return PruneReport{}, fmt.Errorf("connect to docker: %w", err)
	}
	defer cli.Close()

	args := filters.NewArgs()
	if draftOnly {
		args.Add("label", "draft.managed=true")
	}
	report, err := cli.VolumesPrune(ctx, args)
	if err != nil {
		return PruneReport{}, err
	}
	return PruneReport{SpaceReclaimed: int64(report.SpaceReclaimed), Removed: report.VolumesDeleted}, nil
}

// PruneBuildCache clears the BuildKit build cache. There is no meaningful
// "draft only" scope for build cache (BuildKit doesn't tag cache entries by
// project), so draftOnly is accepted for a consistent call signature but
// ignored — the UI should make that limitation explicit rather than implying
// a scoped clear that isn't real.
func (e *Engine) PruneBuildCache(ctx context.Context, draftOnly bool) (PruneReport, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return PruneReport{}, fmt.Errorf("connect to docker: %w", err)
	}
	defer cli.Close()

	report, err := cli.BuildCachePrune(ctx, types.BuildCachePruneOptions{})
	if err != nil {
		return PruneReport{}, err
	}
	if report == nil {
		return PruneReport{}, nil
	}
	return PruneReport{SpaceReclaimed: int64(report.SpaceReclaimed)}, nil
}
