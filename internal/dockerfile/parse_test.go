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
