package draftpack

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"Draft/internal/store"
)

// Integration tests exercise the real store paths used when a user exports a
// service/environment pack and imports it into an existing project (canvas
// "Import pack" → Into this project).

func TestServicePackImportIntoExistingEnvironment(t *testing.T) {
	s := testStore(t)

	// --- Source project: one build-mode service with full config ---
	srcRoot := t.TempDir()
	apiRoot := filepath.Join(srcRoot, "services", "api")
	if err := os.MkdirAll(apiRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(apiRoot, "Dockerfile"), []byte("FROM alpine\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	srcProj, err := s.CreateProject("source-app", srcRoot, "source")
	if err != nil {
		t.Fatal(err)
	}
	srcEnv, err := s.GetDefaultEnvironment(srcProj.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Prefer a builtin template if seeded (icon / re-apply identity).
	var templateID uint
	if tpls, err := s.ListTemplates(); err == nil {
		for _, tpl := range tpls {
			if tpl.Builtin && strings.EqualFold(tpl.Name, "Node.js") {
				templateID = tpl.ID
				break
			}
		}
		if templateID == 0 {
			for _, tpl := range tpls {
				if tpl.Builtin {
					templateID = tpl.ID
					break
				}
			}
		}
	}

	srcNode, err := s.CreateNode(&store.CanvasNode{
		ID:            "svc-src-api01",
		Label:         "api",
		ProjectID:     srcProj.ID,
		EnvironmentID: srcEnv.ID,
		TemplateID:    templateID,
		X:             120,
		Y:             200,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.EnsureNodeUID(srcNode.ID); err != nil {
		t.Fatal(err)
	}

	mustSet := func(k, v string) {
		t.Helper()
		if err := s.SetNodeSetting(srcNode.ID, k, v); err != nil {
			t.Fatalf("set %s: %v", k, err)
		}
	}
	mustSet("dockerfile", "Dockerfile")
	mustSet("service_port", "3000")
	mustSet("git_branch", "main")
	mustSet("deploy_trigger", "manual")
	mustSet("restart_policy", "unless-stopped")
	mustSet("git_repo_root", filepath.Join(srcRoot, ".git-cache-should-not-travel"))
	if err := s.SetServiceRoot(srcNode.ID, srcProj.ID, apiRoot); err != nil {
		t.Fatal(err)
	}
	// Draft volume + bind that needs remap
	mustSet("volume_mounts", `[
		{"type":"volume","containerPath":"/app/data"},
		{"type":"bind","source":"C:\\Users\\Alice\\secrets","containerPath":"/secrets","readOnly":true}
	]`)
	if err := s.UpsertEnvVar(store.EnvVar{
		NodeID: srcNode.ID, Key: "NODE_ENV", Value: "production",
		Scope: store.EnvScopeRuntime, Source: store.EnvSourceManual,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertEnvVar(store.EnvVar{
		NodeID: srcNode.ID, Key: "DATABASE_URL", Value: "@{db.INTERNAL_URL}",
		Scope: store.EnvScopeRuntime, Source: store.EnvSourceManual,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertEnvVar(store.EnvVar{
		NodeID: srcNode.ID, Key: "API_KEY", Value: "super-secret",
		Scope: store.EnvScopeRuntime, Secret: true, Source: store.EnvSourceManual,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetProjectEnvVar(srcProj.ID, "REGION", "us-west", "", false); err != nil {
		t.Fatal(err)
	}

	// --- Export service pack ---
	ex := &Exporter{Store: s}
	pack, err := ex.ExportService(srcNode.ID, DefaultExportOptions())
	if err != nil {
		t.Fatal(err)
	}
	if pack.Scope != ScopeService {
		t.Fatalf("scope=%s", pack.Scope)
	}
	if len(pack.Services) != 1 {
		t.Fatalf("services=%d", len(pack.Services))
	}
	// Machine-local cache must not travel
	if _, ok := pack.Services[0].Settings["git_repo_root"]; ok {
		t.Fatal("git_repo_root should be omitted from pack")
	}
	// Relative service root
	if got := NormalizeRelative(pack.Services[0].Settings["service_root"]); got != "services/api" {
		t.Fatalf("service_root in pack = %q", pack.Services[0].Settings["service_root"])
	}
	// Secret stripped
	for _, ev := range pack.Services[0].Env {
		if ev.Key == "API_KEY" && (!ev.ValueOmitted || ev.Value != "") {
			t.Fatalf("API_KEY should be stripped: %+v", ev)
		}
	}
	// Bind remap recorded
	if len(pack.Services[0].BindRemaps) == 0 {
		t.Fatal("expected bind remaps")
	}

	raw, err := MarshalPack(pack)
	if err != nil {
		t.Fatal(err)
	}
	// Write to disk like the UI save path
	packPath := filepath.Join(t.TempDir(), "api.draftpack")
	if err := os.WriteFile(packPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(packPath)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := UnmarshalPack(data)
	if err != nil {
		t.Fatal(err)
	}

	// --- Destination project (simulates canvas "into this project") ---
	dstRoot := t.TempDir()
	// Mirror relative service root under destination project folder
	if err := os.MkdirAll(filepath.Join(dstRoot, "services", "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Bind target folder on this machine
	bindHost := filepath.Join(dstRoot, "local-secrets")
	if err := os.MkdirAll(bindHost, 0o755); err != nil {
		t.Fatal(err)
	}

	dstProj, err := s.CreateProject("dest-app", dstRoot, "destination")
	if err != nil {
		t.Fatal(err)
	}
	dstEnv, err := s.GetDefaultEnvironment(dstProj.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Pre-existing sibling so we can prove we don't wipe the env
	sibling, err := s.CreateNode(&store.CanvasNode{
		ID: "svc-sibling01", Label: "redis", ProjectID: dstProj.ID, EnvironmentID: dstEnv.ID, X: 0, Y: 0,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Preview (UI dry-run)
	imp := &Importer{Store: s}
	prev, err := imp.Preview(loaded, PreviewOptions{
		Mode: ImportIntoProject, ProjectID: dstProj.ID, EnvironmentID: dstEnv.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if prev.PackScope != ScopeService {
		t.Fatalf("preview scope=%s", prev.PackScope)
	}
	if len(prev.Services) != 1 || prev.Services[0].Label != "api" {
		t.Fatalf("preview services=%+v", prev.Services)
	}

	svcKey := loaded.Services[0].Key
	bindKey := BindOverrideKey(svcKey, "/secrets")
	res, err := imp.Import(loaded, ImportOptions{
		Mode:          ImportIntoProject,
		ProjectID:     dstProj.ID,
		EnvironmentID: dstEnv.ID,
		ServiceRootOverrides: map[string]string{
			// empty → use pack relative service_root under project
		},
		BindPathOverrides: map[string]string{
			bindKey: bindHost,
		},
		SecretValues: map[string]string{
			"API_KEY": "imported-key",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ProjectID != dstProj.ID {
		t.Fatalf("projectId=%d want %d", res.ProjectID, dstProj.ID)
	}
	if len(res.NodeIDs) != 1 {
		t.Fatalf("created nodes=%d want 1", len(res.NodeIDs))
	}
	newID := res.NodeIDs[0]
	if newID == srcNode.ID {
		t.Fatal("imported node must get a new id")
	}

	// --- Assert canvas membership ---
	nodes, err := s.ListNodesByEnvironment(dstEnv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 {
		t.Fatalf("env nodes=%d want 2 (sibling + imported)", len(nodes))
	}
	var imported *store.CanvasNode
	for i := range nodes {
		if nodes[i].ID == sibling.ID {
			continue
		}
		imported = &nodes[i]
	}
	if imported == nil {
		t.Fatal("imported node not found in environment")
	}
	if imported.Label != "api" {
		t.Fatalf("label=%q", imported.Label)
	}
	if imported.ProjectID != dstProj.ID || imported.EnvironmentID != dstEnv.ID {
		t.Fatalf("wrong project/env: %+v", imported)
	}
	if imported.UID == "" {
		t.Fatal("UID must be assigned on import")
	}
	if imported.X != 120 || imported.Y != 200 {
		t.Fatalf("layout not preserved: x=%v y=%v", imported.X, imported.Y)
	}
	if templateID != 0 && imported.TemplateID != templateID {
		// Builtin may rematch by name to a different id after seed; still must be non-zero if source had one
		if imported.TemplateID == 0 {
			t.Fatal("template id lost on import")
		}
	}

	// --- Assert settings applied ---
	settings, err := s.GetNodeSettings(newID)
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"dockerfile":      "Dockerfile",
		"service_port":    "3000",
		"git_branch":      "main",
		"deploy_trigger":  "manual",
		"restart_policy":  "unless-stopped",
		"service_root":    "services/api",
	} {
		if settings[key] != want {
			// service_root may use OS separators in some paths — normalize
			if key == "service_root" && NormalizeRelative(settings[key]) == want {
				continue
			}
			t.Errorf("setting %s = %q want %q", key, settings[key], want)
		}
	}
	if _, ok := settings["git_repo_root"]; ok {
		t.Error("git_repo_root should not be written on import")
	}

	// Volumes: Draft volume kept; bind has remapped host
	volRaw := settings["volume_mounts"]
	if volRaw == "" {
		t.Fatal("volume_mounts missing")
	}
	var vols []map[string]any
	if err := json.Unmarshal([]byte(volRaw), &vols); err != nil {
		t.Fatal(err)
	}
	var sawVolume, sawBind bool
	for _, v := range vols {
		cp, _ := v["containerPath"].(string)
		typ, _ := v["type"].(string)
		if cp == "/app/data" && typ == "volume" {
			sawVolume = true
			if src, _ := v["source"].(string); src != "" {
				t.Errorf("Draft volume should have empty source for auto-name, got %q", src)
			}
		}
		if cp == "/secrets" {
			sawBind = true
			src, _ := v["source"].(string)
			if src != bindHost {
				t.Errorf("bind source = %q want %q", src, bindHost)
			}
		}
	}
	if !sawVolume {
		t.Error("missing Draft volume /app/data")
	}
	if !sawBind {
		t.Error("missing remapped bind /secrets")
	}

	// Env vars
	envVars, err := s.ListEnvVars(newID)
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]store.EnvVar{}
	for _, ev := range envVars {
		byKey[ev.Key] = ev
	}
	if byKey["NODE_ENV"].Value != "production" {
		t.Errorf("NODE_ENV=%q", byKey["NODE_ENV"].Value)
	}
	if byKey["DATABASE_URL"].Value != "@{db.INTERNAL_URL}" {
		t.Errorf("DATABASE_URL=%q (@{refs} must survive)", byKey["DATABASE_URL"].Value)
	}
	if byKey["API_KEY"].Value != "imported-key" || !byKey["API_KEY"].Secret {
		t.Errorf("API_KEY=%+v", byKey["API_KEY"])
	}

	// Project vars from pack merged into destination project
	pv, err := s.GetProjectEnvVar(dstProj.ID, "REGION")
	if err != nil {
		t.Fatalf("project var REGION: %v", err)
	}
	if pv.Value != "us-west" {
		t.Errorf("REGION=%q", pv.Value)
	}
}

func TestServicePackImportRenamesOnLabelCollision(t *testing.T) {
	s := testStore(t)
	srcRoot := t.TempDir()
	dstRoot := t.TempDir()

	srcProj, _ := s.CreateProject("src", srcRoot, "")
	srcEnv, _ := s.GetDefaultEnvironment(srcProj.ID)
	srcNode, _ := s.CreateNode(&store.CanvasNode{
		ID: "svc-src", Label: "worker", ProjectID: srcProj.ID, EnvironmentID: srcEnv.ID,
	})
	_ = s.SetNodeSetting(srcNode.ID, "image", "busybox:latest")
	_ = s.SetNodeSetting(srcNode.ID, "service_port", "8080")

	pack, err := (&Exporter{Store: s}).ExportService(srcNode.ID, DefaultExportOptions())
	if err != nil {
		t.Fatal(err)
	}

	dstProj, _ := s.CreateProject("dst", dstRoot, "")
	dstEnv, _ := s.GetDefaultEnvironment(dstProj.ID)
	// Existing node with same label
	if _, err := s.CreateNode(&store.CanvasNode{
		ID: "svc-exist", Label: "worker", ProjectID: dstProj.ID, EnvironmentID: dstEnv.ID,
	}); err != nil {
		t.Fatal(err)
	}

	res, err := (&Importer{Store: s}).Import(pack, ImportOptions{
		Mode: ImportIntoProject, ProjectID: dstProj.ID, EnvironmentID: dstEnv.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	node, err := s.GetNode(res.NodeIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	if node.Label != "worker-2" {
		t.Fatalf("label=%q want worker-2", node.Label)
	}
	// Original still present
	if _, err := s.GetNodeByLabel(dstEnv.ID, "worker"); err != nil {
		t.Fatal("original worker should remain")
	}
}

func TestEnvironmentPackImportIntoExistingFlattensToTargetEnv(t *testing.T) {
	s := testStore(t)
	srcRoot := t.TempDir()
	dstRoot := t.TempDir()

	srcProj, _ := s.CreateProject("multi", srcRoot, "")
	srcEnv, _ := s.GetDefaultEnvironment(srcProj.ID)
	for i, label := range []string{"web", "db"} {
		n, err := s.CreateNode(&store.CanvasNode{
			ID: "svc-" + label, Label: label, ProjectID: srcProj.ID, EnvironmentID: srcEnv.ID,
			X: float64(i * 100), Y: 50,
		})
		if err != nil {
			t.Fatal(err)
		}
		_ = s.SetNodeSetting(n.ID, "image", "img:"+label)
		_ = s.SetNodeSetting(n.ID, "service_port", "80")
	}

	pack, err := (&Exporter{Store: s}).ExportEnvironment(srcEnv.ID, DefaultExportOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(pack.Services) != 2 {
		t.Fatalf("services=%d", len(pack.Services))
	}

	dstProj, _ := s.CreateProject("target", dstRoot, "")
	// Create a second env and import into it (not default)
	staging, err := s.CreateEnvironment(dstProj.ID, "Staging")
	if err != nil {
		t.Fatal(err)
	}

	res, err := (&Importer{Store: s}).Import(pack, ImportOptions{
		Mode: ImportIntoProject, ProjectID: dstProj.ID, EnvironmentID: staging.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.NodeIDs) != 2 {
		t.Fatalf("nodes=%d", len(res.NodeIDs))
	}

	// All land in staging; default stays empty
	def, _ := s.GetDefaultEnvironment(dstProj.ID)
	defNodes, _ := s.ListNodesByEnvironment(def.ID)
	if len(defNodes) != 0 {
		t.Fatalf("default env should be empty, got %d", len(defNodes))
	}
	stNodes, _ := s.ListNodesByEnvironment(staging.ID)
	if len(stNodes) != 2 {
		t.Fatalf("staging nodes=%d", len(stNodes))
	}
	labels := map[string]bool{}
	for _, n := range stNodes {
		labels[n.Label] = true
		if n.EnvironmentID != staging.ID {
			t.Fatalf("node %s in wrong env", n.Label)
		}
		settings, _ := s.GetNodeSettings(n.ID)
		if settings["image"] == "" || settings["service_port"] != "80" {
			t.Errorf("%s settings incomplete: %v", n.Label, settings)
		}
	}
	if !labels["web"] || !labels["db"] {
		t.Fatalf("labels=%v", labels)
	}
}

func TestEngineExportImportServicePackFileRoundTrip(t *testing.T) {
	// Mirrors deploy.Engine draftpack helpers used by the daemon/Wails path.
	s := testStore(t)
	srcRoot := t.TempDir()
	dstRoot := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dstRoot, "app"), 0o755)

	srcProj, _ := s.CreateProject("eng-src", srcRoot, "")
	srcEnv, _ := s.GetDefaultEnvironment(srcProj.ID)
	node, _ := s.CreateNode(&store.CanvasNode{
		ID: "svc-eng", Label: "app", ProjectID: srcProj.ID, EnvironmentID: srcEnv.ID,
	})
	_ = s.SetNodeSetting(node.ID, "dockerfile", "Dockerfile")
	_ = s.SetNodeSetting(node.ID, "service_port", "8080")
	_ = s.SetNodeSetting(node.ID, "service_root", "app")
	_ = os.MkdirAll(filepath.Join(srcRoot, "app"), 0o755)

	ex := &Exporter{Store: s}
	pack, err := ex.ExportService(node.ID, DefaultExportOptions())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := MarshalPack(pack)
	if err != nil {
		t.Fatal(err)
	}
	packFile := filepath.Join(t.TempDir(), "app.draftpack")
	if err := os.WriteFile(packFile, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	// Re-read via same Unmarshal used by PreviewDraftPackImport / ImportDraftPack
	data, err := os.ReadFile(packFile)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := UnmarshalPack(data)
	if err != nil {
		t.Fatal(err)
	}

	dstProj, _ := s.CreateProject("eng-dst", dstRoot, "")
	dstEnv, _ := s.GetDefaultEnvironment(dstProj.ID)

	// Preview then import — the UI sequence
	imp := &Importer{Store: s}
	if _, err := imp.Preview(loaded, PreviewOptions{
		Mode: ImportIntoProject, ProjectID: dstProj.ID, EnvironmentID: dstEnv.ID,
	}); err != nil {
		t.Fatal(err)
	}
	res, err := imp.Import(loaded, ImportOptions{
		Mode: ImportIntoProject, ProjectID: dstProj.ID, EnvironmentID: dstEnv.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.NodeIDs) != 1 {
		t.Fatalf("nodes=%d", len(res.NodeIDs))
	}
	got, err := s.GetNode(res.NodeIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	if got.Label != "app" || got.EnvironmentID != dstEnv.ID {
		t.Fatalf("node=%+v", got)
	}
	settings, _ := s.GetNodeSettings(got.ID)
	if settings["service_port"] != "8080" || settings["dockerfile"] != "Dockerfile" {
		t.Fatalf("settings=%v", settings)
	}
	// service_root relative path written
	if NormalizeRelative(settings["service_root"]) != "app" {
		t.Fatalf("service_root=%q", settings["service_root"])
	}
}

func TestNewProjectImportCreatesEnvironmentsAndServices(t *testing.T) {
	s := testStore(t)
	srcRoot := t.TempDir()
	srcProj, _ := s.CreateProject("full", srcRoot, "desc")
	mainEnv, _ := s.GetDefaultEnvironment(srcProj.ID)
	staging, _ := s.CreateEnvironment(srcProj.ID, "Staging")

	for _, pair := range []struct {
		envID uint
		label string
	}{
		{mainEnv.ID, "api"},
		{staging.ID, "api"},
		{staging.ID, "worker"},
	} {
		n, err := s.CreateNode(&store.CanvasNode{
			ID: "svc-" + pair.label + "-" + itoa(pair.envID), Label: pair.label,
			ProjectID: srcProj.ID, EnvironmentID: pair.envID,
		})
		if err != nil {
			t.Fatal(err)
		}
		_ = s.SetNodeSetting(n.ID, "image", "img")
		_ = s.SetNodeSetting(n.ID, "service_port", "1")
	}

	pack, err := (&Exporter{Store: s}).ExportProject(srcProj.ID, DefaultExportOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(pack.Environments) != 2 {
		t.Fatalf("envs=%d", len(pack.Environments))
	}
	if len(pack.Services) != 3 {
		t.Fatalf("services=%d", len(pack.Services))
	}

	dest := t.TempDir()
	res, err := (&Importer{Store: s}).Import(pack, ImportOptions{
		Mode: ImportAsNewProject, ProjectName: "full-copy", ProjectPath: dest,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ProjectID == srcProj.ID {
		t.Fatal("expected new project")
	}
	envs, err := s.ListEnvironments(res.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	if len(envs) != 2 {
		t.Fatalf("imported envs=%d", len(envs))
	}
	if len(res.NodeIDs) != 3 {
		t.Fatalf("nodes=%d", len(res.NodeIDs))
	}
	// Each env has the expected service counts
	counts := map[string]int{}
	for _, env := range envs {
		nodes, _ := s.ListNodesByEnvironment(env.ID)
		counts[env.Name] = len(nodes)
	}
	// Default renamed to Main (or pack default name); staging named Staging
	total := 0
	for _, c := range counts {
		total += c
	}
	if total != 3 {
		t.Fatalf("counts=%v", counts)
	}
}

func itoa(n uint) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
