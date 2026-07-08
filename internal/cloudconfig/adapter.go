package cloudconfig

import (
	"fmt"
	"sort"
)

// Adapter converts one external format to and from ServiceSpec. Each adapter is
// both build- and run-aware: Import parses the run document (and, when paired,
// its build document) into specs; Export serializes specs back to one or more
// files (a run file and, for build-mode specs, a build file).
//
// A compose adapter yields many specs from one file; single-service cloud
// formats yield exactly one.
type Adapter interface {
	// Format returns the stable format id: "compose"|"cloudrun"|"ecs"|"containerapps".
	Format() string
	// Detect reports whether data (with the given filename) is this format.
	Detect(name string, data []byte) bool
	// Import parses a document into one or more service specs plus a report.
	Import(data []byte) ([]ServiceSpec, Report, error)
	// Export serializes specs into a map of filename → file content, plus a
	// report of anything that could not be represented.
	Export(specs []ServiceSpec) (map[string][]byte, Report, error)
}

// Overlayer is an optional capability: given the original document a spec was
// imported from, re-emit it with Draft's current run/build-contract overlaid,
// preserving control-plane blocks Draft does not model. Adapters that can do a
// lossless round-trip implement this; deploy uses it when a node carries a
// stored source_config in the matching format.
type Overlayer interface {
	Overlay(base []byte, spec ServiceSpec) ([]byte, Report, error)
}

var registry []Adapter

// Register adds an adapter to the package registry. Adapters call this from
// their file's init().
func Register(a Adapter) { registry = append(registry, a) }

// Adapters returns all registered adapters, sorted by format id for stable
// ordering in UIs.
func Adapters() []Adapter {
	out := make([]Adapter, len(registry))
	copy(out, registry)
	sort.Slice(out, func(i, j int) bool { return out[i].Format() < out[j].Format() })
	return out
}

// ByFormat returns the adapter for a format id.
func ByFormat(format string) (Adapter, error) {
	for _, a := range registry {
		if a.Format() == format {
			return a, nil
		}
	}
	return nil, fmt.Errorf("unknown config format %q", format)
}

// Detect picks the adapter for a document by filename + content sniffing. The
// first registered adapter whose Detect returns true wins.
func Detect(name string, data []byte) (Adapter, error) {
	for _, a := range registry {
		if a.Detect(name, data) {
			return a, nil
		}
	}
	return nil, fmt.Errorf("could not recognize %q as a supported config format", name)
}
