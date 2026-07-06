package dockerfile

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

type ExposePort struct {
	Port     int    `json:"port"`
	Protocol string `json:"protocol"` // "tcp" or "udp"
}

func ParseExposePorts(path string) ([]ExposePort, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open dockerfile: %w", err)
	}
	defer f.Close()

	var ports []ExposePort
	seen := make(map[string]bool)

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if strings.HasPrefix(line, "#") {
			continue
		}

		upper := strings.ToUpper(line)
		if !strings.HasPrefix(upper, "EXPOSE ") {
			continue
		}

		args := strings.TrimSpace(line[7:])
		for _, token := range strings.Fields(args) {
			token = stripARGRefs(token)
			if token == "" {
				continue
			}

			port, proto := parsePortToken(token)
			if port <= 0 || port > 65535 {
				continue
			}

			key := fmt.Sprintf("%d/%s", port, proto)
			if seen[key] {
				continue
			}
			seen[key] = true
			ports = append(ports, ExposePort{Port: port, Protocol: proto})
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read dockerfile: %w", err)
	}

	return ports, nil
}

// BuildInfo summarizes the ARG declarations and build steps in a Dockerfile,
// with awareness of Docker's per-stage ARG scoping. A `--build-arg` only reaches
// a build command if a matching `ARG` is declared in the *same* stage that runs
// that command, so callers need to know not just which args exist but where.
type BuildInfo struct {
	// DeclaredArgs is every ARG name declared anywhere in the Dockerfile
	// (the global scope before the first FROM, or inside any stage).
	DeclaredArgs map[string]bool
	// BuildStageArgs is the ARG names declared inside a stage that also runs a
	// recognized build command. These are the args actually available to the
	// build; an arg present only in another stage is silently unavailable.
	BuildStageArgs map[string]bool
	// HasBuildStep reports whether any stage runs a recognized build command
	// (e.g. `npm run build`, `next build`, `vite build`). When false, callers
	// cannot reason about which stage "the build" happens in.
	HasBuildStep bool
}

// buildCommandPatterns are uppercased substrings that indicate a framework build
// step — the moment where env-driven values (e.g. NEXT_PUBLIC_*, VITE_*) get
// baked into the output. Kept deliberately broad; a false positive only widens
// which stage we consider "the build stage".
var buildCommandPatterns = []string{
	"NPM RUN BUILD", "YARN BUILD", "YARN RUN BUILD",
	"PNPM BUILD", "PNPM RUN BUILD", "BUN RUN BUILD",
	"NEXT BUILD", "VITE BUILD", "NG BUILD",
	"GATSBY BUILD", "NUXT BUILD", "NUXT GENERATE",
	"NPM RUN GENERATE", "REACT-SCRIPTS BUILD", "CRACO BUILD",
	"NPM RUN EXPORT", "EXPO EXPORT",
}

// ParseBuildInfo reads a Dockerfile and reports its ARG/build-step structure.
func ParseBuildInfo(path string) (*BuildInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open dockerfile: %w", err)
	}
	defer f.Close()
	return parseBuildInfo(f), nil
}

// parseBuildInfo does the parsing against any reader so it can be unit-tested
// without touching the filesystem.
func parseBuildInfo(r io.Reader) *BuildInfo {
	info := &BuildInfo{
		DeclaredArgs:   map[string]bool{},
		BuildStageArgs: map[string]bool{},
	}

	// A stage is a FROM block. The synthetic first stage collects any ARGs that
	// appear before the first FROM (Docker's "global" args), which are never in
	// scope inside a stage unless re-declared there.
	type stage struct {
		args         []string
		hasBuildStep bool
	}
	global := &stage{}
	stages := []*stage{global}
	current := global

	process := func(logical string) {
		l := strings.TrimSpace(logical)
		if l == "" || strings.HasPrefix(l, "#") {
			return
		}
		upper := strings.ToUpper(l)
		switch {
		case strings.HasPrefix(upper, "FROM "):
			current = &stage{}
			stages = append(stages, current)
		case strings.HasPrefix(upper, "ARG "):
			if name := parseArgName(l[len("ARG "):]); name != "" {
				current.args = append(current.args, name)
			}
		case strings.HasPrefix(upper, "RUN ") || strings.HasPrefix(upper, "CMD ") || strings.HasPrefix(upper, "ENTRYPOINT "):
			if containsBuildCommand(upper) {
				current.hasBuildStep = true
			}
		}
	}

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var logical strings.Builder
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		// Full-line comments never continue and never contribute to an
		// instruction, even mid-continuation.
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		// A trailing backslash continues the instruction onto the next line.
		if strings.HasSuffix(trimmed, "\\") {
			logical.WriteString(strings.TrimSuffix(trimmed, "\\"))
			logical.WriteByte(' ')
			continue
		}
		logical.WriteString(trimmed)
		process(logical.String())
		logical.Reset()
	}
	if logical.Len() > 0 {
		process(logical.String())
	}

	for _, st := range stages {
		for _, a := range st.args {
			info.DeclaredArgs[a] = true
		}
		if st.hasBuildStep {
			info.HasBuildStep = true
			for _, a := range st.args {
				info.BuildStageArgs[a] = true
			}
		}
	}
	return info
}

// parseArgName extracts the variable name from an ARG instruction's arguments,
// dropping any `=default` and ignoring anything past the first token.
func parseArgName(s string) string {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return ""
	}
	name := fields[0]
	if i := strings.Index(name, "="); i >= 0 {
		name = name[:i]
	}
	return name
}

func containsBuildCommand(upper string) bool {
	for _, pat := range buildCommandPatterns {
		if strings.Contains(upper, pat) {
			return true
		}
	}
	return false
}

func parsePortToken(token string) (int, string) {
	proto := "tcp"
	if i := strings.Index(token, "/"); i >= 0 {
		proto = strings.ToLower(token[i+1:])
		token = token[:i]
	}
	if proto != "tcp" && proto != "udp" {
		proto = "tcp"
	}
	port, err := strconv.Atoi(token)
	if err != nil {
		return 0, proto
	}
	return port, proto
}

// stripARGRefs removes common Dockerfile ARG/variable syntax like ${PORT} or $PORT
// that can't be resolved statically. Returns empty string if the token is entirely a variable.
func stripARGRefs(token string) string {
	if strings.Contains(token, "$") {
		return ""
	}
	return token
}
