package deploy

import (
	"sort"
	"strings"

	"Draft/internal/dockerfile"
	"Draft/internal/store"
)

// DockerfileBuildInfo reports the build-time facts the Variables UI needs to
// warn about env wiring. It intentionally carries only raw facts about the
// Dockerfile; the policy (which env var is a problem) is applied against the
// live, possibly-unsaved variable list on the frontend so warnings react
// instantly to the build-arg toggle.
type DockerfileBuildInfo struct {
	// BuildMode is true when the service builds from a Dockerfile (as opposed to
	// pulling a prebuilt image, where build args are meaningless).
	BuildMode bool `json:"buildMode"`
	// Parsed is true when the Dockerfile was found on disk and read. It can be
	// false for a build-mode service whose Dockerfile only exists on a pinned
	// git ref; scope warnings still apply, but ARG checks are skipped.
	Parsed bool `json:"parsed"`
	// HasBuildStep is true when some stage runs a recognized build command.
	HasBuildStep bool `json:"hasBuildStep"`
	// DeclaredArgs is every ARG name declared anywhere in the Dockerfile.
	DeclaredArgs []string `json:"declaredArgs"`
	// BuildStageArgs is the ARG names declared in a stage that runs a build
	// command — the args actually available to the build.
	BuildStageArgs []string `json:"buildStageArgs"`
}

// InspectDockerfileBuildInfo resolves a node's Dockerfile the same way the
// deploy engine does and returns its ARG/build-step structure. It never errors
// on a missing or unreadable Dockerfile: those degrade to Parsed=false so the
// UI can still surface scope-only warnings.
func InspectDockerfileBuildInfo(s *store.Store, nodeID string) (*DockerfileBuildInfo, error) {
	settings, err := s.EffectiveNodeSettings(nodeID)
	if err != nil {
		return nil, err
	}

	dockerfileSetting := strings.TrimSpace(settings["dockerfile"])
	if dockerfileSetting == "" {
		// Image-mode service: no build, so no build-arg wiring to check.
		return &DockerfileBuildInfo{BuildMode: false}, nil
	}

	out := &DockerfileBuildInfo{BuildMode: true}

	path, err := resolveDockerfileForAnalysis(s, nodeID, settings, dockerfileSetting)
	if err != nil || path == "" {
		return out, nil
	}
	info, err := dockerfile.ParseBuildInfo(path)
	if err != nil {
		return out, nil
	}

	out.Parsed = true
	out.HasBuildStep = info.HasBuildStep
	out.DeclaredArgs = sortedKeys(info.DeclaredArgs)
	out.BuildStageArgs = sortedKeys(info.BuildStageArgs)
	return out, nil
}

// resolveDockerfileForAnalysis mirrors the engine's working-tree path math so
// the analyzer inspects the same file a plain (non-git-pinned) deploy would.
func resolveDockerfileForAnalysis(s *store.Store, nodeID string, settings map[string]string, dockerfileSetting string) (string, error) {
	var node store.CanvasNode
	if err := s.DB.First(&node, "id = ?", nodeID).Error; err != nil {
		return "", err
	}
	project, err := s.GetProject(node.ProjectID)
	if err != nil {
		return "", err
	}
	plan, err := resolveBuildContextPlan(project.Path, settings["service_root"], dockerfileSetting)
	if err != nil {
		return "", err
	}
	return plan.DockerfilePath, nil
}

func sortedKeys(m map[string]bool) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
