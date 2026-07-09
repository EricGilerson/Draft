//go:build integration

package deploy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"Draft/internal/networking"
	"Draft/internal/store"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
)

// setupMultiEnvIntegration creates a project with a default Main environment
// and a fresh engine/router. Unlike setupIntegration, nodes must be created
// with a real EnvironmentID.
func setupMultiEnvIntegration(t *testing.T) (*Engine, *store.Store, *eventCollector, *store.Project, uint) {
	t.Helper()
	cli := requireDocker(t)
	t.Cleanup(func() { cli.Close() })

	s := openTestStore(t)
	r := networking.NewRouter(s, "127.0.0.1:0")
	if err := r.Start(); err != nil {
		t.Fatalf("start router: %v", err)
	}
	t.Cleanup(func() { r.Stop() })

	logDir := filepath.Join(t.TempDir(), "logs")
	col := &eventCollector{}
	e := New(s, r, logDir, col.emit)

	projectDir := t.TempDir()
	p, err := s.CreateProject(integrationProjectName("link"), projectDir, "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	mainEnv, err := s.GetDefaultEnvironment(p.ID)
	if err != nil {
		t.Fatalf("GetDefaultEnvironment: %v", err)
	}

	t.Cleanup(func() {
		// Tear down all deployments for this project.
		var deps []store.Deployment
		_ = s.DB.Where("project_id = ?", p.ID).Find(&deps).Error
		cleanupContainers(t, cli, deps)
		cleanupDraftNetworks(t, cli, p.ID, p.Name)
		// Remove any draft-clone staging volumes left behind.
		args := filters.NewArgs()
		args.Add("label", "draft.managed=true")
		args.Add("label", fmt.Sprintf("draft.project=%d", p.ID))
		if list, err := cli.VolumeList(context.Background(), volume.ListOptions{Filters: args}); err == nil {
			for _, v := range list.Volumes {
				_ = cli.VolumeRemove(context.Background(), v.Name, true)
			}
		}
	})

	return e, s, col, p, mainEnv.ID
}

// TestIntegrationSharedServiceMultiAttachCrossEnv verifies the VPC-style
// multi-homing path: a root container on Main is NetworkConnect'd onto the
// Staging bridge with the alias node's DNS names, so a Staging consumer can
// reach it via @{Service} hostnames — while a Main-only non-shared service
// remains unreachable from Staging.
func TestIntegrationSharedServiceMultiAttachCrossEnv(t *testing.T) {
	cli := requireDocker(t)
	defer cli.Close()

	e, s, col, p, mainEnvID := setupMultiEnvIntegration(t)

	// Root: HTTP server on Main (image mode, no build).
	// Use python http.server via a custom image would need pull; use nginx alpine
	// or the same pattern as other tests: build from Dockerfile is more reliable
	// for a known response body. Prefer image alpine + custom cmd via settings.
	// Simplest: build-mode tiny server like existing integration tests.
	projectDir := p.Path
	rootDir := filepath.Join(projectDir, "root-http")
	if err := os.MkdirAll(rootDir, 0755); err != nil {
		t.Fatal(err)
	}
	writeDockerfile(t, rootDir, `FROM alpine:3.20
RUN apk add --no-cache python3 && mkdir -p /www && echo -n "shared-root-ok" > /www/index.html
CMD ["python3", "-m", "http.server", "8080", "--directory", "/www"]
`)
	root, err := s.CreateNode(&store.CanvasNode{
		ID: "root-http", ProjectID: p.ID, EnvironmentID: mainEnvID, Label: "api",
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.SetNodeSetting(root.ID, "service_root", "root-http")
	_ = s.SetNodeSetting(root.ID, "dockerfile", "Dockerfile")
	_ = s.SetNodeSetting(root.ID, "service_port", "8080")

	// Main-only private service (not shared).
	privDir := filepath.Join(projectDir, "private")
	if err := os.MkdirAll(privDir, 0755); err != nil {
		t.Fatal(err)
	}
	writeDockerfile(t, privDir, `FROM alpine:3.20
RUN apk add --no-cache python3 && mkdir -p /www && echo -n "private-main" > /www/index.html
CMD ["python3", "-m", "http.server", "8080", "--directory", "/www"]
`)
	priv, err := s.CreateNode(&store.CanvasNode{
		ID: "priv-http", ProjectID: p.ID, EnvironmentID: mainEnvID, Label: "private",
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.SetNodeSetting(priv.ID, "service_root", "private")
	_ = s.SetNodeSetting(priv.ID, "dockerfile", "Dockerfile")
	_ = s.SetNodeSetting(priv.ID, "service_port", "8080")

	// Staging env with alias of root + a consumer.
	staging, err := s.CreateEnvironment(p.ID, "Staging")
	if err != nil {
		t.Fatal(err)
	}
	alias, err := s.CreateNode(&store.CanvasNode{
		ID: "alias-api", ProjectID: p.ID, EnvironmentID: staging.ID, Label: "api",
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.SetNodeSetting(alias.ID, "service_port", "8080")
	if err := e.SetServiceLink(alias.ID, root.ID); err != nil {
		t.Fatalf("SetServiceLink: %v", err)
	}

	consDir := filepath.Join(projectDir, "consumer")
	if err := os.MkdirAll(consDir, 0755); err != nil {
		t.Fatal(err)
	}
	writeDockerfile(t, consDir, "FROM alpine:3.20\nCMD [\"sleep\", \"3600\"]\n")
	consumer, err := s.CreateNode(&store.CanvasNode{
		ID: "consumer", ProjectID: p.ID, EnvironmentID: staging.ID, Label: "worker",
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.SetNodeSetting(consumer.ID, "service_root", "consumer")
	_ = s.SetNodeSetting(consumer.ID, "dockerfile", "Dockerfile")
	_ = s.SetNodeSetting(consumer.ID, "service_port", "80")

	// Deploy root + private on Main, consumer on Staging.
	if err := e.Deploy(context.Background(), root.ID); err != nil {
		t.Fatal(err)
	}
	if waitForStatus(col, root.ID, "running", 90*time.Second) == nil {
		t.Fatal("root not running")
	}
	if err := e.Deploy(context.Background(), priv.ID); err != nil {
		t.Fatal(err)
	}
	if waitForStatus(col, priv.ID, "running", 90*time.Second) == nil {
		t.Fatal("private not running")
	}
	if err := e.Deploy(context.Background(), consumer.ID); err != nil {
		t.Fatal(err)
	}
	if waitForStatus(col, consumer.ID, "running", 90*time.Second) == nil {
		t.Fatal("consumer not running")
	}

	// Sync link: multi-attach root onto Staging network.
	if err := e.Deploy(context.Background(), alias.ID); err != nil {
		t.Fatal(err)
	}
	// Alias emit status mirrors root (running) — give it a moment.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if col.findStatus(alias.ID, "running") != nil || col.findStatus(alias.ID, "stopped") != nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	rootDep, _ := s.ActiveDeployment(root.ID)
	privDep, _ := s.ActiveDeployment(priv.ID)
	consDep, _ := s.ActiveDeployment(consumer.ID)
	if rootDep == nil || consDep == nil {
		t.Fatal("missing deployments")
	}

	// Inspect: root container should be on BOTH main and staging networks.
	inspect, err := cli.ContainerInspect(context.Background(), rootDep.ContainerID)
	if err != nil {
		t.Fatal(err)
	}
	mainNet := draftNetworkName(p.ID, p.Name, "main")
	stagingNet := draftNetworkName(p.ID, p.Name, staging.Slug)
	if inspect.NetworkSettings == nil || inspect.NetworkSettings.Networks == nil {
		t.Fatal("no network settings on root")
	}
	if _, ok := inspect.NetworkSettings.Networks[mainNet]; !ok {
		// Network names might be slightly different if sanitize differs — list keys.
		keys := make([]string, 0, len(inspect.NetworkSettings.Networks))
		for k := range inspect.NetworkSettings.Networks {
			keys = append(keys, k)
		}
		t.Fatalf("root not on main network %q; networks=%v", mainNet, keys)
	}
	if _, ok := inspect.NetworkSettings.Networks[stagingNet]; !ok {
		keys := make([]string, 0, len(inspect.NetworkSettings.Networks))
		for k := range inspect.NetworkSettings.Networks {
			keys = append(keys, k)
		}
		t.Fatalf("root not multi-attached to staging network %q; networks=%v", stagingNet, keys)
	}

	// Alias hostname (Staging-native) must resolve from consumer.
	aliasAddr, err := e.computeNodeAddress(alias)
	if err != nil {
		t.Fatal(err)
	}
	url := fmt.Sprintf("http://%s:8080/", aliasAddr.InternalHostname)
	stdout, exitCode, err := execInContainer(t, cli, consDep.ContainerID, []string{"wget", "-T", "5", "-qO-", url})
	if err != nil {
		t.Fatalf("exec wget: %v", err)
	}
	if exitCode != 0 {
		// Also try short name alias.
		url2 := "http://api:8080/"
		stdout2, exit2, err2 := execInContainer(t, cli, consDep.ContainerID, []string{"wget", "-T", "5", "-qO-", url2})
		t.Fatalf("wget to alias hostname %s exit=%d out=%q; short-name try exit=%d err=%v out=%q",
			url, exitCode, stdout, exit2, err2, stdout2)
	}
	if !strings.Contains(stdout, "shared-root-ok") {
		t.Fatalf("expected shared-root-ok, got %q", stdout)
	}

	// Short DNS name on staging network.
	stdout, exitCode, err = execInContainer(t, cli, consDep.ContainerID, []string{"wget", "-T", "5", "-qO-", "http://api:8080/"})
	if err != nil || exitCode != 0 || !strings.Contains(stdout, "shared-root-ok") {
		t.Fatalf("short-name wget failed: err=%v exit=%d out=%q", err, exitCode, stdout)
	}

	// Private Main service must NOT be reachable from Staging consumer.
	if privDep != nil && privDep.Hostname != "" {
		privURL := fmt.Sprintf("http://%s:8080/", privDep.Hostname)
		_, exitCode, err = execInContainer(t, cli, consDep.ContainerID, []string{"wget", "-T", "3", "-qO-", privURL})
		if err == nil && exitCode == 0 {
			t.Fatalf("staging consumer should not reach Main-only private service at %s", privURL)
		}
	}

	// Disconnect on unlink: staging network attachment removed.
	if err := e.UnlinkService(context.Background(), alias.ID, "fresh"); err != nil {
		t.Fatalf("UnlinkService: %v", err)
	}
	inspect, err = cli.ContainerInspect(context.Background(), rootDep.ContainerID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := inspect.NetworkSettings.Networks[stagingNet]; ok {
		t.Fatalf("root should be disconnected from staging network after unlink")
	}
}

// TestIntegrationVolumeCloneSafePromote copies data into a staging volume and
// rewires the target mount without destroying the previous volume on success.
func TestIntegrationVolumeCloneSafePromote(t *testing.T) {
	cli := requireDocker(t)
	defer cli.Close()

	e, s, _, p, mainEnvID := setupMultiEnvIntegration(t)
	ctx := context.Background()

	// Ensure alpine helper image is present (clone uses it).
	if _, err := cli.ImageInspect(ctx, cloneHelperImage); err != nil {
		reader, err := cli.ImagePull(ctx, cloneHelperImage, image.PullOptions{})
		if err != nil {
			t.Fatalf("pull alpine: %v", err)
		}
		_, _ = reader.Read(make([]byte, 1))
		_ = reader.Close()
	}

	srcNode, err := s.CreateNode(&store.CanvasNode{
		ID: "vol-src", ProjectID: p.ID, EnvironmentID: mainEnvID, Label: "src",
	})
	if err != nil {
		t.Fatal(err)
	}
	tgtNode, err := s.CreateNode(&store.CanvasNode{
		ID: "vol-tgt", ProjectID: p.ID, EnvironmentID: mainEnvID, Label: "tgt",
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.SetNodeSetting(srcNode.ID, "volume_mounts", `[{"type":"volume","containerPath":"/data"}]`)
	_ = s.SetNodeSetting(tgtNode.ID, "volume_mounts", `[{"type":"volume","containerPath":"/data"}]`)

	srcSettings, _ := s.GetNodeSettings(srcNode.ID)
	tgtSettings, _ := s.GetNodeSettings(tgtNode.ID)
	srcVol, err := e.resolveVolumeNameForPath(srcNode, srcSettings, "/data")
	if err != nil {
		t.Fatal(err)
	}
	tgtVol, err := e.resolveVolumeNameForPath(tgtNode, tgtSettings, "/data")
	if err != nil {
		t.Fatal(err)
	}

	// Create volumes and seed source with a marker file.
	for _, name := range []string{srcVol, tgtVol} {
		if _, err := cli.VolumeCreate(ctx, volume.CreateOptions{
			Name: name,
			Labels: map[string]string{
				"draft.managed": "true",
				"draft.project": fmt.Sprintf("%d", p.ID),
			},
		}); err != nil {
			t.Fatalf("VolumeCreate %s: %v", name, err)
		}
	}
	// Seed source
	if err := writeFileToVolume(ctx, cli, srcVol, "marker.txt", "clone-payload-v1"); err != nil {
		t.Fatalf("seed source: %v", err)
	}
	// Seed target with different content so we can prove replace + orphan.
	if err := writeFileToVolume(ctx, cli, tgtVol, "marker.txt", "old-target-data"); err != nil {
		t.Fatalf("seed target: %v", err)
	}

	result, err := e.CloneVolumeData(ctx, tgtNode.ID, srcNode.ID, "/data", CloneQuick)
	if err != nil {
		t.Fatalf("CloneVolumeData: %v", err)
	}
	if result.NewVolumeName == "" {
		t.Fatal("expected new volume name")
	}
	if result.OrphanedVolumeName != tgtVol {
		t.Errorf("orphaned = %q, want previous target %q", result.OrphanedVolumeName, tgtVol)
	}

	// Target settings now point at promoted staging volume.
	tgtSettings, _ = s.GetNodeSettings(tgtNode.ID)
	newName, err := e.resolveVolumeNameForPath(tgtNode, tgtSettings, "/data")
	if err != nil {
		t.Fatal(err)
	}
	if newName != result.NewVolumeName {
		t.Errorf("settings source = %q, want %q", newName, result.NewVolumeName)
	}

	// New volume has source payload.
	got, err := readFileFromVolume(ctx, cli, result.NewVolumeName, "marker.txt")
	if err != nil {
		t.Fatalf("read new volume: %v", err)
	}
	if !strings.Contains(got, "clone-payload-v1") {
		t.Fatalf("new volume content = %q", got)
	}

	// Old volume still exists with old data (orphaned, not deleted).
	if _, err := cli.VolumeInspect(ctx, tgtVol); err != nil {
		t.Fatalf("old target volume should still exist: %v", err)
	}
	old, err := readFileFromVolume(ctx, cli, tgtVol, "marker.txt")
	if err != nil {
		t.Fatalf("read old volume: %v", err)
	}
	if !strings.Contains(old, "old-target-data") {
		t.Fatalf("old volume should keep prior data, got %q", old)
	}
}

// TestIntegrationVolumeCloneFailureLeavesTargetIntact ensures a bad source
// does not rewire target settings.
func TestIntegrationVolumeCloneFailureLeavesTargetIntact(t *testing.T) {
	cli := requireDocker(t)
	defer cli.Close()

	e, s, _, p, mainEnvID := setupMultiEnvIntegration(t)
	ctx := context.Background()

	src, _ := s.CreateNode(&store.CanvasNode{ID: "s1", ProjectID: p.ID, EnvironmentID: mainEnvID, Label: "s"})
	tgt, _ := s.CreateNode(&store.CanvasNode{ID: "t1", ProjectID: p.ID, EnvironmentID: mainEnvID, Label: "t"})
	_ = s.SetNodeSetting(src.ID, "volume_mounts", `[{"type":"volume","containerPath":"/data"}]`)
	_ = s.SetNodeSetting(tgt.ID, "volume_mounts", `[{"type":"volume","source":"keep-me-vol","containerPath":"/data"}]`)

	// Target has explicit volume that exists; source volume does NOT exist.
	if _, err := cli.VolumeCreate(ctx, volume.CreateOptions{Name: "keep-me-vol", Labels: map[string]string{"draft.managed": "true"}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cli.VolumeRemove(ctx, "keep-me-vol", true) })

	before, _ := s.GetNodeSettings(tgt.ID)
	_, err := e.CloneVolumeData(ctx, tgt.ID, src.ID, "/data", CloneQuick)
	if err == nil {
		t.Fatal("expected clone to fail when source volume missing")
	}
	after, _ := s.GetNodeSettings(tgt.ID)
	if before["volume_mounts"] != after["volume_mounts"] {
		t.Fatalf("target mounts changed on failure: before=%s after=%s", before["volume_mounts"], after["volume_mounts"])
	}
}

func TestIntegrationVolumeCloneConsistentRestartsSourceWithoutFalseFailure(t *testing.T) {
	cli := requireDocker(t)
	defer cli.Close()

	e, s, col, p, mainEnvID := setupMultiEnvIntegration(t)
	ctx := context.Background()

	projectDir := p.Path
	for _, dir := range []string{"clone-src", "clone-tgt"} {
		root := filepath.Join(projectDir, dir)
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
		writeDockerfile(t, root, `FROM alpine:3.20
RUN apk add --no-cache python3 && mkdir -p /www && echo ok > /www/index.html
CMD ["python3", "-m", "http.server", "8080", "--directory", "/www"]
`)
	}

	src, err := s.CreateNode(&store.CanvasNode{ID: "src-live", ProjectID: p.ID, EnvironmentID: mainEnvID, Label: "src-live"})
	if err != nil {
		t.Fatal(err)
	}
	tgt, err := s.CreateNode(&store.CanvasNode{ID: "tgt-live", ProjectID: p.ID, EnvironmentID: mainEnvID, Label: "tgt-live"})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		nodeID string
		root   string
	}{
		{src.ID, "clone-src"},
		{tgt.ID, "clone-tgt"},
	} {
		_ = s.SetNodeSetting(item.nodeID, "service_root", item.root)
		_ = s.SetNodeSetting(item.nodeID, "dockerfile", "Dockerfile")
		_ = s.SetNodeSetting(item.nodeID, "service_port", "8080")
		_ = s.SetNodeSetting(item.nodeID, "volume_mounts", `[{"type":"volume","containerPath":"/data"}]`)
	}

	if err := e.Deploy(ctx, src.ID); err != nil {
		t.Fatalf("deploy source: %v", err)
	}
	if waitForStatus(col, src.ID, "running", 90*time.Second) == nil {
		t.Fatal("source did not reach running")
	}
	if err := e.Deploy(ctx, tgt.ID); err != nil {
		t.Fatalf("deploy target: %v", err)
	}
	if waitForStatus(col, tgt.ID, "running", 90*time.Second) == nil {
		t.Fatal("target did not reach running")
	}

	srcDep, err := s.ActiveDeployment(src.ID)
	if err != nil || srcDep == nil {
		t.Fatalf("source active deployment missing: %v", err)
	}

	srcSettings, _ := s.GetNodeSettings(src.ID)
	tgtSettings, _ := s.GetNodeSettings(tgt.ID)
	srcVol, err := e.resolveVolumeNameForPath(src, srcSettings, "/data")
	if err != nil {
		t.Fatal(err)
	}
	tgtVol, err := e.resolveVolumeNameForPath(tgt, tgtSettings, "/data")
	if err != nil {
		t.Fatal(err)
	}
	if err := writeFileToVolume(ctx, cli, srcVol, "marker.txt", "consistent-clone"); err != nil {
		t.Fatalf("seed source: %v", err)
	}
	if err := writeFileToVolume(ctx, cli, tgtVol, "marker.txt", "old-target"); err != nil {
		t.Fatalf("seed target: %v", err)
	}

	result, err := e.CloneVolumeData(ctx, tgt.ID, src.ID, "/data", CloneConsistent)
	if err != nil {
		t.Fatalf("CloneVolumeData consistent: %v", err)
	}
	if result == nil || result.NewVolumeName == "" {
		t.Fatal("expected new volume from consistent clone")
	}

	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		dep, err := s.ActiveDeployment(src.ID)
		if err == nil && dep != nil && dep.ID != srcDep.ID && dep.Status == "running" {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	dep, err := s.ActiveDeployment(src.ID)
	if err != nil || dep == nil || dep.ID == srcDep.ID || dep.Status != "running" {
		t.Fatalf("source was not restarted after consistent clone: dep=%+v err=%v", dep, err)
	}

	if got, err := readFileFromVolume(ctx, cli, result.NewVolumeName, "marker.txt"); err != nil {
		t.Fatalf("read cloned volume: %v", err)
	} else if !strings.Contains(got, "consistent-clone") {
		t.Fatalf("cloned volume content = %q", got)
	}

	time.Sleep(1500 * time.Millisecond)
	if ev := col.findStatus(src.ID, "failed"); ev != nil {
		t.Fatalf("source emitted false failed status during consistent clone: %+v", ev.Data)
	}
	if ev := col.findStatus(tgt.ID, "failed"); ev != nil {
		t.Fatalf("target emitted false failed status during consistent clone: %+v", ev.Data)
	}
}

// TestIntegrationReconcileServiceLinkNetworksReattaches verifies startup-style
// reconcile re-connects a root after a forced disconnect.
func TestIntegrationReconcileServiceLinkNetworksReattaches(t *testing.T) {
	cli := requireDocker(t)
	defer cli.Close()

	e, s, col, p, mainEnvID := setupMultiEnvIntegration(t)
	projectDir := p.Path
	rootDir := filepath.Join(projectDir, "r")
	_ = os.MkdirAll(rootDir, 0755)
	writeDockerfile(t, rootDir, "FROM alpine:3.20\nCMD [\"sleep\", \"3600\"]\n")

	root, _ := s.CreateNode(&store.CanvasNode{ID: "r1", ProjectID: p.ID, EnvironmentID: mainEnvID, Label: "db"})
	_ = s.SetNodeSetting(root.ID, "service_root", "r")
	_ = s.SetNodeSetting(root.ID, "dockerfile", "Dockerfile")
	_ = s.SetNodeSetting(root.ID, "service_port", "5432")

	staging, _ := s.CreateEnvironment(p.ID, "Staging")
	alias, _ := s.CreateNode(&store.CanvasNode{ID: "a1", ProjectID: p.ID, EnvironmentID: staging.ID, Label: "db"})
	_ = s.SetNodeSetting(alias.ID, "service_port", "5432")
	if err := e.SetServiceLink(alias.ID, root.ID); err != nil {
		t.Fatal(err)
	}

	if err := e.Deploy(context.Background(), root.ID); err != nil {
		t.Fatal(err)
	}
	if waitForStatus(col, root.ID, "running", 90*time.Second) == nil {
		t.Fatal("root not running")
	}
	if err := e.EnsureServiceLinkNetworks(context.Background(), root.ID); err != nil {
		t.Fatal(err)
	}

	dep, _ := s.ActiveDeployment(root.ID)
	stagingNet := draftNetworkName(p.ID, p.Name, staging.Slug)
	// Force disconnect.
	_ = cli.NetworkDisconnect(context.Background(), stagingNet, dep.ContainerID, true)

	inspect, _ := cli.ContainerInspect(context.Background(), dep.ContainerID)
	if _, ok := inspect.NetworkSettings.Networks[stagingNet]; ok {
		t.Fatal("expected forced disconnect")
	}

	if err := e.ReconcileServiceLinkNetworks(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	inspect, err := cli.ContainerInspect(context.Background(), dep.ContainerID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := inspect.NetworkSettings.Networks[stagingNet]; !ok {
		t.Fatalf("reconcile should re-attach root to %s", stagingNet)
	}
}

func writeFileToVolume(ctx context.Context, cli *client.Client, volName, filename, content string) error {
	if _, err := cli.ImageInspect(ctx, cloneHelperImage); err != nil {
		reader, err := cli.ImagePull(ctx, cloneHelperImage, image.PullOptions{})
		if err != nil {
			return err
		}
		buf := make([]byte, 32*1024)
		for {
			_, err := reader.Read(buf)
			if err != nil {
				break
			}
		}
		_ = reader.Close()
	}
	resp, err := cli.ContainerCreate(ctx, &container.Config{
		Image: cloneHelperImage,
		Cmd:   []string{"sh", "-c", fmt.Sprintf("echo -n '%s' > /v/%s", content, filename)},
	}, &container.HostConfig{
		Mounts: []mount.Mount{{Type: mount.TypeVolume, Source: volName, Target: "/v"}},
	}, nil, nil, "")
	if err != nil {
		return err
	}
	defer cli.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
	if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return err
	}
	statusCh, errCh := cli.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		return err
	case st := <-statusCh:
		if st.StatusCode != 0 {
			return fmt.Errorf("write helper exit %d", st.StatusCode)
		}
	}
	return nil
}

func readFileFromVolume(ctx context.Context, cli *client.Client, volName, filename string) (string, error) {
	resp, err := cli.ContainerCreate(ctx, &container.Config{
		Image: cloneHelperImage,
		Cmd:   []string{"cat", "/v/" + filename},
	}, &container.HostConfig{
		Mounts: []mount.Mount{{Type: mount.TypeVolume, Source: volName, Target: "/v", ReadOnly: true}},
	}, nil, nil, "")
	if err != nil {
		return "", err
	}
	defer cli.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
	if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return "", err
	}
	// Wait then logs
	statusCh, errCh := cli.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			return "", err
		}
	case <-statusCh:
	}
	out, err := cli.ContainerLogs(ctx, resp.ID, container.LogsOptions{ShowStdout: true, ShowStderr: true})
	if err != nil {
		return "", err
	}
	defer out.Close()
	buf := make([]byte, 4096)
	n, _ := out.Read(buf)
	// Docker multiplexes stdout; strip header if present or just search content.
	return string(buf[:n]), nil
}
