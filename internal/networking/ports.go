package networking

import (
	"fmt"
	"net"
)

const (
	PortRangeStart = 10000
	PortRangeEnd   = 60000
)

// IsPortFree checks whether a TCP port on localhost is available for binding.
func IsPortFree(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

// FindFreePort finds an available TCP port in Draft's managed range, skipping
// any ports in the excluded set (already leased by other services).
func FindFreePort(excluded map[int]bool) (int, error) {
	for port := PortRangeStart; port <= PortRangeEnd; port++ {
		if excluded[port] {
			continue
		}
		if IsPortFree(port) {
			return port, nil
		}
	}
	return 0, fmt.Errorf("no free port found in range %d–%d", PortRangeStart, PortRangeEnd)
}

// FindFreePortNear tries to allocate the preferred port first. If it's taken,
// falls back to FindFreePort.
func FindFreePortNear(preferred int, excluded map[int]bool) (int, error) {
	if preferred > 0 && !excluded[preferred] && IsPortFree(preferred) {
		return preferred, nil
	}
	return FindFreePort(excluded)
}
