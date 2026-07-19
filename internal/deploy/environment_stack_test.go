package deploy

import (
	"sort"
	"testing"

	"Draft/internal/store"
)

func TestStackDependencyWavesOrdersProvidersFirst(t *testing.T) {
	nodes := []store.CanvasNode{
		{ID: "api", Label: "api"},
		{ID: "db", Label: "db"},
		{ID: "cache", Label: "cache"},
		{ID: "worker", Label: "worker"},
	}
	// api → db, api → cache, worker → api
	deps := map[string]map[string]struct{}{
		"api":    {"db": {}, "cache": {}},
		"worker": {"api": {}},
		"db":     {},
		"cache":  {},
	}
	waves := stackDependencyWaves(nodes, deps)
	if len(waves) < 3 {
		t.Fatalf("expected at least 3 waves, got %v", waves)
	}
	pos := map[string]int{}
	for i, wave := range waves {
		for _, id := range wave {
			pos[id] = i
		}
	}
	if pos["db"] >= pos["api"] || pos["cache"] >= pos["api"] {
		t.Fatalf("providers should precede api: %v", waves)
	}
	if pos["api"] >= pos["worker"] {
		t.Fatalf("api should precede worker: %v", waves)
	}
}

func TestStackDependencyWavesBreaksCycles(t *testing.T) {
	nodes := []store.CanvasNode{
		{ID: "a", Label: "a"},
		{ID: "b", Label: "b"},
	}
	deps := map[string]map[string]struct{}{
		"a": {"b": {}},
		"b": {"a": {}},
	}
	waves := stackDependencyWaves(nodes, deps)
	seen := map[string]bool{}
	for _, wave := range waves {
		for _, id := range wave {
			seen[id] = true
		}
	}
	if !seen["a"] || !seen["b"] {
		t.Fatalf("cycle wave missing nodes: %v", waves)
	}
}

func TestReverseWaves(t *testing.T) {
	waves := [][]string{{"db", "cache"}, {"api"}, {"worker"}}
	got := reverseWaves(waves)
	if len(got) != 3 || got[0][0] != "worker" || got[2][0] != "db" {
		t.Fatalf("reverseWaves = %v", got)
	}
	// Stable sort for assertion on first wave contents
	sort.Strings(got[2])
	if got[2][0] != "cache" && got[2][0] != "db" {
		t.Fatalf("unexpected last wave %v", got[2])
	}
}
