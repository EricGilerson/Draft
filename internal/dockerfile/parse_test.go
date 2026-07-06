package dockerfile

import (
	"os"
	"path/filepath"
	"testing"
)

func writeDockerfile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "Dockerfile")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseSingleExpose(t *testing.T) {
	path := writeDockerfile(t, "FROM alpine\nEXPOSE 8080\nCMD [\"app\"]\n")
	ports, err := ParseExposePorts(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(ports) != 1 {
		t.Fatalf("expected 1 port, got %d", len(ports))
	}
	if ports[0].Port != 8080 || ports[0].Protocol != "tcp" {
		t.Errorf("got %+v", ports[0])
	}
}

func TestParseMultiplePorts(t *testing.T) {
	path := writeDockerfile(t, "FROM node\nEXPOSE 3000 5432/tcp 6379\n")
	ports, err := ParseExposePorts(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(ports) != 3 {
		t.Fatalf("expected 3 ports, got %d", len(ports))
	}
	want := []ExposePort{
		{3000, "tcp"},
		{5432, "tcp"},
		{6379, "tcp"},
	}
	for i, w := range want {
		if ports[i] != w {
			t.Errorf("port %d: got %+v, want %+v", i, ports[i], w)
		}
	}
}

func TestParseUDPProtocol(t *testing.T) {
	path := writeDockerfile(t, "FROM alpine\nEXPOSE 53/udp\n")
	ports, err := ParseExposePorts(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(ports) != 1 || ports[0].Protocol != "udp" {
		t.Errorf("expected 53/udp, got %+v", ports)
	}
}

func TestParseMultipleExposeLines(t *testing.T) {
	path := writeDockerfile(t, "FROM alpine\nEXPOSE 80\nRUN echo hi\nEXPOSE 443\n")
	ports, err := ParseExposePorts(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(ports) != 2 {
		t.Fatalf("expected 2 ports, got %d", len(ports))
	}
}

func TestParseDeduplicate(t *testing.T) {
	path := writeDockerfile(t, "FROM alpine\nEXPOSE 8080\nEXPOSE 8080\n")
	ports, err := ParseExposePorts(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(ports) != 1 {
		t.Fatalf("expected 1 port (deduped), got %d", len(ports))
	}
}

func TestParseSkipsVariables(t *testing.T) {
	path := writeDockerfile(t, "FROM alpine\nEXPOSE $PORT\nEXPOSE ${APP_PORT}\nEXPOSE 3000\n")
	ports, err := ParseExposePorts(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(ports) != 1 || ports[0].Port != 3000 {
		t.Errorf("expected only port 3000, got %+v", ports)
	}
}

func TestParseNoExpose(t *testing.T) {
	path := writeDockerfile(t, "FROM alpine\nCMD [\"app\"]\n")
	ports, err := ParseExposePorts(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(ports) != 0 {
		t.Errorf("expected 0 ports, got %d", len(ports))
	}
}

func TestParseCommentedExpose(t *testing.T) {
	path := writeDockerfile(t, "FROM alpine\n# EXPOSE 9999\nEXPOSE 3000\n")
	ports, err := ParseExposePorts(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(ports) != 1 || ports[0].Port != 3000 {
		t.Errorf("expected only 3000, got %+v", ports)
	}
}

func TestParseCaseInsensitive(t *testing.T) {
	path := writeDockerfile(t, "from alpine\nexpose 4000\n")
	ports, err := ParseExposePorts(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(ports) != 1 || ports[0].Port != 4000 {
		t.Errorf("expected 4000, got %+v", ports)
	}
}

func TestParseFileNotFound(t *testing.T) {
	_, err := ParseExposePorts("/nonexistent/Dockerfile")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestParseBuildInfoArgInBuildStage(t *testing.T) {
	df := `FROM node:20 AS builder
ARG NEXT_PUBLIC_API_URL
COPY . .
RUN npm run build

FROM node:20 AS runner
COPY --from=builder /app/.next ./.next
CMD ["npm", "start"]
`
	info, err := ParseBuildInfo(writeDockerfile(t, df))
	if err != nil {
		t.Fatal(err)
	}
	if !info.HasBuildStep {
		t.Fatal("expected a build step to be detected")
	}
	if !info.DeclaredArgs["NEXT_PUBLIC_API_URL"] {
		t.Fatal("expected NEXT_PUBLIC_API_URL to be declared")
	}
	if !info.BuildStageArgs["NEXT_PUBLIC_API_URL"] {
		t.Fatal("expected NEXT_PUBLIC_API_URL to be available in the build stage")
	}
}

func TestParseBuildInfoArgInWrongStage(t *testing.T) {
	df := `FROM node:20 AS builder
RUN npm run build

FROM node:20 AS runner
ARG NEXT_PUBLIC_API_URL
CMD ["npm", "start"]
`
	info, err := ParseBuildInfo(writeDockerfile(t, df))
	if err != nil {
		t.Fatal(err)
	}
	if !info.DeclaredArgs["NEXT_PUBLIC_API_URL"] {
		t.Fatal("expected the arg to be declared somewhere")
	}
	if info.BuildStageArgs["NEXT_PUBLIC_API_URL"] {
		t.Fatal("arg declared in runner stage must not count as build-stage available")
	}
}

func TestParseBuildInfoNoArg(t *testing.T) {
	df := `FROM node:20
COPY . .
RUN npm run build
CMD ["npm", "start"]
`
	info, err := ParseBuildInfo(writeDockerfile(t, df))
	if err != nil {
		t.Fatal(err)
	}
	if !info.HasBuildStep {
		t.Fatal("expected build step")
	}
	if len(info.DeclaredArgs) != 0 {
		t.Fatalf("expected no declared args, got %v", info.DeclaredArgs)
	}
}

func TestParseBuildInfoArgDefaultAndContinuation(t *testing.T) {
	df := `FROM node:20 AS builder
ARG BUILD_TOKEN=fallback
RUN npm ci && \
    npm run build
`
	info, err := ParseBuildInfo(writeDockerfile(t, df))
	if err != nil {
		t.Fatal(err)
	}
	if !info.DeclaredArgs["BUILD_TOKEN"] {
		t.Fatalf("expected BUILD_TOKEN parsed without default, got %v", info.DeclaredArgs)
	}
	if !info.BuildStageArgs["BUILD_TOKEN"] {
		t.Fatal("expected BUILD_TOKEN available to continued RUN build step")
	}
}

func TestParseBuildInfoGlobalArgNotInBuildStage(t *testing.T) {
	// An ARG before the first FROM is global and must be re-declared inside a
	// stage to be usable there.
	df := `ARG NEXT_PUBLIC_API_URL
FROM node:20 AS builder
RUN npm run build
`
	info, err := ParseBuildInfo(writeDockerfile(t, df))
	if err != nil {
		t.Fatal(err)
	}
	if !info.DeclaredArgs["NEXT_PUBLIC_API_URL"] {
		t.Fatal("global arg should be counted as declared")
	}
	if info.BuildStageArgs["NEXT_PUBLIC_API_URL"] {
		t.Fatal("global arg must not count as build-stage available")
	}
}

func TestParseBuildInfoNoBuildStep(t *testing.T) {
	df := `FROM python:3.12
ARG PIP_TOKEN
RUN pip install -r requirements.txt
CMD ["python", "app.py"]
`
	info, err := ParseBuildInfo(writeDockerfile(t, df))
	if err != nil {
		t.Fatal(err)
	}
	if info.HasBuildStep {
		t.Fatal("pip install should not be detected as a framework build step")
	}
}
