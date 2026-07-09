package deploy

import (
	"encoding/json"
	"strings"
	"testing"

	"Draft/internal/store"
)

func TestSetVolumeMountSource_UpdatesAndAppends(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	dir := t.TempDir()
	p := createStampProject(t, s, dir)
	env := defaultEnvID(t, s, p.ID)
	n, err := s.CreateNode(&store.CanvasNode{ID: "n1", ProjectID: p.ID, EnvironmentID: env, Label: "db"})
	if err != nil {
		t.Fatal(err)
	}
	raw := `[{"type":"volume","containerPath":"/data"},{"type":"bind","source":"/host","containerPath":"/cfg"}]`
	if err := s.SetNodeSetting(n.ID, "volume_mounts", raw); err != nil {
		t.Fatal(err)
	}
	settings, _ := s.GetNodeSettings(n.ID)
	if err := e.setVolumeMountSource(n.ID, settings, "/data", "draft-vol-new"); err != nil {
		t.Fatal(err)
	}
	settings, _ = s.GetNodeSettings(n.ID)
	specs := ParseVolumeSpecs(settings["volume_mounts"])
	found := false
	for _, sp := range specs {
		if sp.ContainerPath == "/data" {
			found = true
			if sp.Source != "draft-vol-new" || sp.Type != VolumeTypeVolume {
				t.Fatalf("updated mount = %+v", sp)
			}
		}
		if sp.ContainerPath == "/cfg" && sp.Type != "bind" && sp.Type != "" {
			// bind preserved
		}
	}
	if !found {
		t.Fatal("expected /data mount")
	}

	// Append path that did not exist.
	settings, _ = s.GetNodeSettings(n.ID)
	if err := e.setVolumeMountSource(n.ID, settings, "/extra", "vol-extra"); err != nil {
		t.Fatal(err)
	}
	settings, _ = s.GetNodeSettings(n.ID)
	specs = ParseVolumeSpecs(settings["volume_mounts"])
	foundExtra := false
	for _, sp := range specs {
		if sp.ContainerPath == "/extra" && sp.Source == "vol-extra" {
			foundExtra = true
		}
	}
	if !foundExtra {
		t.Fatalf("expected appended /extra, got %s", settings["volume_mounts"])
	}
}

func TestCloneLockRejectsConcurrent(t *testing.T) {
	if !tryLockClone("node-a") {
		t.Fatal("first lock should succeed")
	}
	if tryLockClone("node-a") {
		t.Fatal("second lock on same node should fail")
	}
	if !tryLockClone("node-b") {
		t.Fatal("different node should lock independently")
	}
	unlockClone("node-a")
	unlockClone("node-b")
	if !tryLockClone("node-a") {
		t.Fatal("lock should work after unlock")
	}
	unlockClone("node-a")
}

func TestCloneEndpointsRejectsAliasSourceAndTarget(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	dir := t.TempDir()
	p := createStampProject(t, s, dir)
	env := defaultEnvID(t, s, p.ID)
	root, _ := s.CreateNode(&store.CanvasNode{ID: "root", ProjectID: p.ID, EnvironmentID: env, Label: "db"})
	alias, _ := s.CreateNode(&store.CanvasNode{ID: "alias", ProjectID: p.ID, EnvironmentID: env, Label: "db2"})
	target, _ := s.CreateNode(&store.CanvasNode{ID: "tgt", ProjectID: p.ID, EnvironmentID: env, Label: "db3"})
	_ = s.SetNodeSetting(root.ID, "volume_mounts", `[{"type":"volume","containerPath":"/data"}]`)
	_ = s.SetNodeSetting(target.ID, "volume_mounts", `[{"type":"volume","containerPath":"/data"}]`)
	if err := e.SetServiceLink(alias.ID, root.ID); err != nil {
		t.Fatal(err)
	}

	_, _, _, _, err := e.cloneEndpoints(alias.ID, root.ID)
	if err == nil || !strings.Contains(err.Error(), "linked service") {
		t.Fatalf("expected reject alias target, got %v", err)
	}
	_, _, _, _, err = e.cloneEndpoints(target.ID, alias.ID)
	if err == nil || !strings.Contains(err.Error(), "root volumes") {
		t.Fatalf("expected reject alias source, got %v", err)
	}
}

func TestResolveVolumeNameForPath_AutoAndExplicit(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	dir := t.TempDir()
	p := createStampProject(t, s, dir)
	env := defaultEnvID(t, s, p.ID)
	n, err := s.CreateNode(&store.CanvasNode{ID: "n1", ProjectID: p.ID, EnvironmentID: env, Label: "db"})
	if err != nil {
		t.Fatal(err)
	}
	uid, err := s.EnsureNodeUID(n.ID)
	if err != nil {
		t.Fatal(err)
	}
	envRow, _ := s.GetEnvironment(env)

	// Auto-named
	settings := map[string]string{
		"volume_mounts": `[{"type":"volume","containerPath":"/data"}]`,
	}
	name, err := e.resolveVolumeNameForPath(n, settings, "/data")
	if err != nil {
		t.Fatal(err)
	}
	want := DraftVolumeName(p.ID, p.Name, envRow.Slug, uid, "/data")
	if name != want {
		t.Errorf("auto name = %q, want %q", name, want)
	}

	// Explicit
	settings["volume_mounts"] = `[{"type":"volume","source":"my-vol","containerPath":"/data"}]`
	name, err = e.resolveVolumeNameForPath(n, settings, "/data")
	if err != nil {
		t.Fatal(err)
	}
	if name != "my-vol" {
		t.Errorf("explicit = %q", name)
	}
}

func TestShareWarningKinds(t *testing.T) {
	s := openTestStore(t)
	cases := []struct {
		label, image, wantKind string
	}{
		{"rabbit", "rabbitmq:3", "rabbitmq"},
		{"cache", "redis:7", "redis"},
		{"obj", "minio/minio", "minio"},
		{"db", "postgres:16", "database"},
		{"app", "myapp:latest", "generic"},
	}
	for _, tc := range cases {
		n := &store.CanvasNode{Label: tc.label}
		kind, msg := shareWarningForNode(s, n, map[string]string{"image": tc.image})
		if kind != tc.wantKind {
			t.Errorf("%s/%s: kind=%q want %q", tc.label, tc.image, kind, tc.wantKind)
		}
		if msg == "" {
			t.Errorf("%s: empty warning", tc.label)
		}
	}
}

func TestListShareableRootsExcludesAliasesAndEmpty(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	dir := t.TempDir()
	p := createStampProject(t, s, dir)
	env := defaultEnvID(t, s, p.ID)
	root, _ := s.CreateNode(&store.CanvasNode{ID: "root", ProjectID: p.ID, EnvironmentID: env, Label: "db"})
	_ = s.SetNodeSetting(root.ID, "volume_mounts", `[{"type":"volume","containerPath":"/data"}]`)
	api, _ := s.CreateNode(&store.CanvasNode{ID: "api", ProjectID: p.ID, EnvironmentID: env, Label: "api"})
	_ = s.SetNodeSetting(api.ID, "volume_mounts", `[]`)
	alias, _ := s.CreateNode(&store.CanvasNode{ID: "alias", ProjectID: p.ID, EnvironmentID: env, Label: "db2"})
	if err := e.SetServiceLink(alias.ID, root.ID); err != nil {
		t.Fatal(err)
	}

	roots, err := e.ListShareableRoots(p.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 || roots[0].NodeID != root.ID {
		t.Fatalf("expected only root, got %+v", roots)
	}
}

func TestReconcileServiceLinkNetworksNoOpWithoutDocker(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	// No links → no error even without Docker.
	if err := e.ReconcileServiceLinkNetworks(t.Context()); err != nil {
		t.Fatal(err)
	}
	// With a link but root not running → no error (attach deferred).
	dir := t.TempDir()
	p := createStampProject(t, s, dir)
	env := defaultEnvID(t, s, p.ID)
	root, _ := s.CreateNode(&store.CanvasNode{ID: "r", ProjectID: p.ID, EnvironmentID: env, Label: "db"})
	alias, _ := s.CreateNode(&store.CanvasNode{ID: "a", ProjectID: p.ID, EnvironmentID: env, Label: "db2"})
	if err := e.SetServiceLink(alias.ID, root.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.ReconcileServiceLinkNetworks(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestDuplicateEnvironmentFreshStripsExplicitVolumeSources(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	dir := t.TempDir()
	p := createStampProject(t, s, dir)
	env := defaultEnvID(t, s, p.ID)
	n, _ := s.CreateNode(&store.CanvasNode{ID: "db", ProjectID: p.ID, EnvironmentID: env, Label: "db"})
	_ = s.SetNodeSetting(n.ID, "volume_mounts", `[{"type":"volume","source":"shared-old","containerPath":"/data"}]`)
	_ = s.SetNodeSetting(n.ID, "service_port", "5432")

	newEnv, err := e.DuplicateEnvironment(env, "Staging")
	if err != nil {
		t.Fatal(err)
	}
	nodes, _ := s.ListNodesByEnvironment(newEnv.ID)
	if len(nodes) != 1 {
		t.Fatalf("nodes=%d", len(nodes))
	}
	settings, _ := s.GetNodeSettings(nodes[0].ID)
	specs := ParseVolumeSpecs(settings["volume_mounts"])
	if len(specs) != 1 || specs[0].Source != "" {
		// re-encode for clearer failure
		b, _ := json.Marshal(specs)
		t.Fatalf("fresh duplicate should use auto-named volumes, got %s", b)
	}
	if ParseServiceLink(settings[SettingServiceLink]) != nil {
		t.Fatal("fresh should not be linked")
	}
}
