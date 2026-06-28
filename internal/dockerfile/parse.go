package dockerfile

import (
	"bufio"
	"fmt"
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
