package deploy

import (
	"os"
	"path/filepath"
	"testing"

	"Draft/internal/store"
)

func TestInspectDockerfileBuildInfoParsesWorkingTree(t *testing.T) {
	s, err := store.Open(store.MemoryDSN())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	dir := t.TempDir()
	df := `FROM node:20 AS builder
ARG NEXT_PUBLIC_API_URL
RUN npm run build

FROM node:20 AS runner
CMD ["npm", "start"]
`
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(df), 0644); err != nil {
		t.Fatal(err)
	}

	project, err := s.CreateProject("web", dir, "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := s.CreateNode(&store.CanvasNode{ID: "web-1", ProjectID: project.ID, Label: "web"}); err != nil {
		t.Fatalf("create node: %v", err)
	}
	if err := s.SetNodeSetting("web-1", "dockerfile", "Dockerfile"); err != nil {
		t.Fatalf("set dockerfile: %v", err)
	}

	info, err := InspectDockerfileBuildInfo(s, "web-1")
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if !info.BuildMode || !info.Parsed {
		t.Fatalf("expected build-mode parsed info, got %+v", info)
	}
	if !info.HasBuildStep {
		t.Fatal("expected a build step")
	}
	if !contains(info.DeclaredArgs, "NEXT_PUBLIC_API_URL") {
		t.Fatalf("expected declared arg, got %v", info.DeclaredArgs)
	}
	if !contains(info.BuildStageArgs, "NEXT_PUBLIC_API_URL") {
		t.Fatalf("expected build-stage arg, got %v", info.BuildStageArgs)
	}
}

func TestInspectDockerfileBuildInfoImageMode(t *testing.T) {
	s, err := store.Open(store.MemoryDSN())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	project, err := s.CreateProject("db", t.TempDir(), "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := s.CreateNode(&store.CanvasNode{ID: "db-1", ProjectID: project.ID, Label: "db"}); err != nil {
		t.Fatalf("create node: %v", err)
	}
	if err := s.SetNodeSetting("db-1", "image", "postgres:16"); err != nil {
		t.Fatalf("set image: %v", err)
	}

	info, err := InspectDockerfileBuildInfo(s, "db-1")
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if info.BuildMode {
		t.Fatalf("image-mode service should not be build-mode, got %+v", info)
	}
}

func TestInspectDockerfileBuildInfoMissingFile(t *testing.T) {
	s, err := store.Open(store.MemoryDSN())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	project, err := s.CreateProject("web", t.TempDir(), "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := s.CreateNode(&store.CanvasNode{ID: "web-1", ProjectID: project.ID, Label: "web"}); err != nil {
		t.Fatalf("create node: %v", err)
	}
	if err := s.SetNodeSetting("web-1", "dockerfile", "Dockerfile"); err != nil {
		t.Fatalf("set dockerfile: %v", err)
	}

	// Build-mode is reported even though the file can't be read, so scope
	// warnings still apply; ARG facts are simply unavailable.
	info, err := InspectDockerfileBuildInfo(s, "web-1")
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if !info.BuildMode {
		t.Fatal("expected build-mode for a set Dockerfile")
	}
	if info.Parsed {
		t.Fatal("expected Parsed=false for a missing Dockerfile")
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
