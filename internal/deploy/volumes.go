package deploy

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"Draft/internal/networking"
	"Draft/internal/store"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"gorm.io/gorm"
)

// VolumeTypeBind and VolumeTypeVolume are the canonical type strings used in
// the volume_mounts JSON. We keep them as constants so the frontend, the stamp
// path, the parser, and the deploy path all agree. The JSON uses short strings
// ("bind" / "volume") rather than Docker's mount.Type so the on-disk format is
// stable across SDK renames.
const (
	VolumeTypeBind   = "bind"
	VolumeTypeVolume = "volume"
)

// VolumeSpec is the JSON shape stored in node_settings.volume_mounts AND in
// ServiceTemplate.Volumes. One shape end-to-end: the wizard edits it, the
// Settings tab edits it, templates ship defaults in it, and the deploy path
// consumes it. A missing Type is treated as "bind" so rows written by older
// versions of Draft (which only knew about bind mounts) keep working.
type VolumeSpec struct {
	Type          string            `json:"type,omitempty"`          // "bind" | "volume"; "" => "bind"
	Source        string            `json:"source,omitempty"`        // volume: "" = auto-name (Draft-managed), or explicit name; bind: host path
	HostPath      string            `json:"hostPath,omitempty"`      // legacy bind host path (back-compat with pre-volume rows)
	ContainerPath string            `json:"containerPath"`           // mount target inside the container
	ReadOnly      bool              `json:"readOnly,omitempty"`
	SizeHint      string            `json:"sizeHint,omitempty"`      // advisory capacity, e.g. "10g"; monitored via SystemDF, NOT enforced by Docker
	Labels        map[string]string `json:"labels,omitempty"`        // optional extra labels applied to a Draft-managed volume at creation
}

// ParseVolumeSpecs decodes the volume_mounts setting into specs. Malformed JSON
// yields nil (the deploy path treats that as "no mounts" rather than failing
// the whole deploy on one bad row).
func ParseVolumeSpecs(raw string) []VolumeSpec {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var specs []VolumeSpec
	if json.Unmarshal([]byte(raw), &specs) != nil {
		return nil
	}
	return specs
}

// SpecsToMounts converts specs into Docker mounts. Auto named volumes (Type ==
// "volume" with empty Source) get an empty Source here; ensureNamedVolumes
// fills it in at deploy time when the node identity is known. Bind mounts with
// no resolvable host path (neither Source nor legacy HostPath) are dropped.
// Returns nil for nil/empty input so callers can distinguish "no mounts" from
// "one mount" without a length check (matching the pre-volume parseVolumeMounts
// contract).
func SpecsToMounts(specs []VolumeSpec) []mount.Mount {
	if len(specs) == 0 {
		return nil
	}
	out := make([]mount.Mount, 0, len(specs))
	for _, s := range specs {
		if strings.TrimSpace(s.ContainerPath) == "" {
			continue
		}
		switch s.Type {
		case VolumeTypeVolume:
			out = append(out, mount.Mount{
				Type:     mount.TypeVolume,
				Source:   strings.TrimSpace(s.Source),
				Target:   s.ContainerPath,
				ReadOnly: s.ReadOnly,
			})
		default: // "" or "bind"
			src := strings.TrimSpace(s.Source)
			if src == "" {
				src = strings.TrimSpace(s.HostPath) // back-compat
			}
			if src == "" {
				continue
			}
			out = append(out, mount.Mount{
				Type:     mount.TypeBind,
				Source:   src,
				Target:   s.ContainerPath,
				ReadOnly: s.ReadOnly,
			})
		}
	}
	return out
}

// dockerEnvironment returns the Docker env identity segment for a NodeAddress.
func dockerEnvironment(addr NodeAddress) string {
	if addr.DockerEnvironment != "" {
		return addr.DockerEnvironment
	}
	return networking.DockerEnvironment(addr.Environment, addr.Sandbox)
}

// DraftVolumeName builds the deterministic Docker volume name for an auto-named
// (empty Source) volume on a given node. The name is derived from the node's
// permanent UID + project + environment + target, so the same service keeps
// its data across redeploys while two different services never share a volume.
// The environment segment is the Docker identity (sand-{slug} for sandboxes).
func DraftVolumeName(projectID uint, projectName, environment, uid, target string) string {
	env := sanitize(environment)
	if env == "" {
		env = "default"
	}
	proj := sanitize(projectName)
	if proj == "" {
		proj = fmt.Sprintf("p%d", projectID)
	}
	uidPart := strings.TrimSpace(uid)
	if uidPart == "" {
		uidPart = "node"
	}
	tgt := slugifyPath(target)
	if tgt == "" {
		tgt = "data"
	}
	if len(tgt) > 24 {
		tgt = tgt[:24]
	}
	name := fmt.Sprintf("draft-%d-%s-%s-%s-%s", projectID, proj, env, uidPart, tgt)
	if len(name) > 63 {
		// Fall back to a hash-suffixed name that stays within Docker's practical
		// ~64-char volume name limit while remaining deterministic.
		h := shortHash(name)
		name = fmt.Sprintf("draft-%d-%s-%s-%s", projectID, env, uidPart, h)
		if len(name) > 63 {
			name = name[:63]
		}
	}
	return strings.Trim(name, "-")
}

// slugifyPath turns a container path like "/var/lib/postgresql/data" into a
// Docker-name-safe slug "var-lib-postgresql-data".
func slugifyPath(p string) string {
	s := strings.ToLower(strings.TrimSpace(p))
	s = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		return '-'
	}, s)
	return strings.Trim(s, "-")
}

func shortHash(s string) string {
	sum := sha1.Sum([]byte(s))
	return hex.EncodeToString(sum[:])[:8]
}

// volumeLabels returns the labels Draft stamps onto every managed volume it
// creates. The draft.node label is the source of truth for "which service owns
// this volume", so even after a node row is deleted we can still find and
// offer to clean up its volumes. User-supplied labels from the spec are merged
// in (Draft labels win on conflict so ownership stays discoverable).
func volumeLabels(node *store.CanvasNode, projectName, environment, target string, userLabels map[string]string) map[string]string {
	lbl := map[string]string{
		"draft.managed":     "true",
		"draft.project":     fmt.Sprintf("%d", node.ProjectID),
		"draft.projectName": projectName,
		"draft.node":        node.ID,
		"draft.environment": environment,
		"draft.target":      target,
	}
	for k, v := range userLabels {
		if strings.HasPrefix(k, "draft.") {
			continue
		}
		lbl[k] = v
	}
	return lbl
}

// ensureNamedVolumes resolves auto-named volumes in mounts against the node's
// identity and creates them in Docker (idempotently — VolumeCreate returns the
// existing volume if the name already exists). Mounts that already carry a
// Source (bind mounts, or volumes with an explicit user-provided name) pass
// through untouched. This is the only place that talks to Docker for volumes
// during a deploy; it runs right before ContainerCreate in both deploy paths.
func (e *Engine) ensureNamedVolumes(
	ctx context.Context,
	cli *client.Client,
	node *store.CanvasNode,
	addr NodeAddress,
	uid string,
	specs []VolumeSpec,
) ([]mount.Mount, error) {
	mounts := make([]mount.Mount, 0, len(specs))
	for _, s := range specs {
		if strings.TrimSpace(s.ContainerPath) == "" {
			continue
		}
		switch s.Type {
		case VolumeTypeVolume:
			name := strings.TrimSpace(s.Source)
			if name == "" {
				name = DraftVolumeName(node.ProjectID, addr.ProjectName, dockerEnvironment(addr), uid, s.ContainerPath)
			}
			if _, err := cli.VolumeCreate(ctx, volume.CreateOptions{
				Name:   name,
				Driver: "local",
				Labels: volumeLabels(node, addr.ProjectName, dockerEnvironment(addr), s.ContainerPath, s.Labels),
			}); err != nil {
				return nil, fmt.Errorf("create volume %s for %s: %w", name, s.ContainerPath, err)
			}
			mounts = append(mounts, mount.Mount{
				Type:     mount.TypeVolume,
				Source:   name,
				Target:   s.ContainerPath,
				ReadOnly: s.ReadOnly,
			})
		default: // bind
			src := strings.TrimSpace(s.Source)
			if src == "" {
				src = strings.TrimSpace(s.HostPath)
			}
			if src == "" {
				continue
			}
			mounts = append(mounts, mount.Mount{
				Type:     mount.TypeBind,
				Source:   src,
				Target:   s.ContainerPath,
				ReadOnly: s.ReadOnly,
			})
		}
	}
	return mounts, nil
}

// ManagedVolume is a Draft-managed Docker volume as seen by the management UI.
// Usage is best-effort: Docker only reports per-volume size via SystemDF, and
// not all drivers populate it, so Size may be 0 even for a non-empty volume.
type ManagedVolume struct {
	Name        string            `json:"name"`
	Labels      map[string]string `json:"labels"`
	Mountpoint  string            `json:"mountpoint"`
	Driver      string            `json:"driver"`
	CreatedAt   string            `json:"createdAt"`
	Size        int64             `json:"size"`
	RefCount    int               `json:"refCount"`
	ProjectID   uint              `json:"projectId"`
	NodeID      string            `json:"nodeId"`
	Target      string            `json:"target"`
	Environment string            `json:"environment"`
}

// ListManagedVolumes returns Draft-managed volumes filtered by an optional node
// ID and/or project ID. Filtering uses the draft.* labels stamped at creation,
// so it works even for a volume whose owning node row has been deleted. Usage
// figures come from SystemDF (best-effort; missing => 0).
func (e *Engine) ListManagedVolumes(ctx context.Context, projectID *uint, nodeID string) ([]ManagedVolume, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("connect to docker: %w", err)
	}
	defer cli.Close()

	args := filters.NewArgs()
	args.Add("label", "draft.managed=true")
	if nodeID != "" {
		args.Add("label", "draft.node="+nodeID)
	}
	if projectID != nil {
		args.Add("label", fmt.Sprintf("draft.project=%d", *projectID))
	}

	listResp, err := cli.VolumeList(ctx, volume.ListOptions{Filters: args})
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

	out := make([]ManagedVolume, 0, len(listResp.Volumes))
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
		out = append(out, mv)
	}
	return out, nil
}

// VolumeOverview enriches a ManagedVolume with the store-derived facts the
// global Volumes tab needs but Docker labels alone can't give: the owning
// node's current label, and whether that node still exists. Orphaned is the
// signal for "stale" — the service was deleted but Draft kept the data (see
// DeleteService), so it's reclaimable.
type VolumeOverview struct {
	ManagedVolume
	NodeLabel string `json:"nodeLabel"`
	Orphaned  bool   `json:"orphaned"`
	// Managed is true when the volume carries draft.managed=true. Always true
	// for ListVolumesOverview (which only ever sees Draft-managed volumes);
	// set per-row by ListAllVolumes (docker_admin.go), which also lists
	// foreign volumes.
	Managed bool `json:"managed"`
}

// ListVolumesOverview returns every Draft-managed volume across all projects,
// enriched with its owning node's label and an orphaned flag. A volume is
// orphaned when its draft.node label points at a node row that no longer
// exists — the "keep and surface" deletion policy leaves the data behind so it
// can be reclaimed here. ProjectName comes from the draft.projectName label, so
// no project lookup is needed.
func (e *Engine) ListVolumesOverview(ctx context.Context) ([]VolumeOverview, error) {
	vols, err := e.ListManagedVolumes(ctx, nil, "")
	if err != nil {
		return nil, err
	}
	out := enrichVolumes(e.store, vols)
	for i := range out {
		out[i].Managed = true
	}
	return out, nil
}

// enrichVolumes joins managed volumes against the store to set each one's
// owning-node label and orphaned flag. Split out from ListVolumesOverview (and
// its Docker call) so the orphan logic is unit-testable without a daemon.
//
// Only gorm.ErrRecordNotFound proves the node is gone. Any other GetNode error
// (e.g. a transient SQLite busy/lock error under concurrent access) is
// inconclusive, so it must not flip a live volume to "orphaned" for one
// refresh and back on the next.
func enrichVolumes(s *store.Store, vols []ManagedVolume) []VolumeOverview {
	out := make([]VolumeOverview, 0, len(vols))
	for _, v := range vols {
		ov := VolumeOverview{ManagedVolume: v}
		if v.NodeID != "" {
			node, err := s.GetNode(v.NodeID)
			switch {
			case err == nil:
				ov.NodeLabel = node.Label
			case errors.Is(err, gorm.ErrRecordNotFound):
				ov.Orphaned = true
			}
		} else {
			ov.Orphaned = true
		}
		out = append(out, ov)
	}
	return out
}

// DeleteManagedVolume removes a Docker volume by name. force=false refuses to
// remove a volume that a running container is still using. Only volumes
// labelled draft.managed=true may be removed through this path, so a user can't
// accidentally nuke an unrelated Docker volume by name.
func (e *Engine) DeleteManagedVolume(ctx context.Context, name string, force bool) error {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("connect to docker: %w", err)
	}
	defer cli.Close()

	// Verify ownership via labels before removing.
	args := filters.NewArgs()
	args.Add("label", "draft.managed=true")
	args.Add("name", name)
	listResp, err := cli.VolumeList(ctx, volume.ListOptions{Filters: args})
	if err != nil {
		return fmt.Errorf("inspect volume: %w", err)
	}
	owned := false
	for _, v := range listResp.Volumes {
		if v.Name == name {
			owned = true
			break
		}
	}
	if !owned {
		return fmt.Errorf("no Draft-managed volume named %q", name)
	}

	// Docker's VolumeRemove `force` only suppresses not-found errors — it will
	// NOT remove a volume that a container still references. So "force" here
	// means: first tear down every container using this volume (that's the only
	// way to free it), then remove it.
	if force {
		if err := removeContainersUsingVolume(ctx, cli, name); err != nil {
			return err
		}
	}
	return cli.VolumeRemove(ctx, name, force)
}

// removeContainersUsingVolume force-removes every container (running or stopped)
// that references the named volume, so the volume can then be deleted. This is
// the teardown behind a forced volume delete; the owning service's container is
// gone afterward and the daemon's reconcile will observe it as stopped.
func removeContainersUsingVolume(ctx context.Context, cli *client.Client, name string) error {
	args := filters.NewArgs()
	args.Add("volume", name)
	containers, err := cli.ContainerList(ctx, container.ListOptions{All: true, Filters: args})
	if err != nil {
		return fmt.Errorf("find containers using volume %q: %w", name, err)
	}
	for _, c := range containers {
		if err := cli.ContainerRemove(ctx, c.ID, container.RemoveOptions{Force: true}); err != nil {
			return fmt.Errorf("remove container %s using volume %q: %w", c.ID[:12], name, err)
		}
	}
	return nil
}

// SystemDF wraps the Docker SDK call so callers outside the deploy package can
// reach it without importing the SDK directly. Currently unused internally but
// kept available for the management UI's usage polling.
func (e *Engine) SystemDF(ctx context.Context) (types.DiskUsage, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return types.DiskUsage{}, err
	}
	defer cli.Close()
	return cli.DiskUsage(ctx, types.DiskUsageOptions{})
}
