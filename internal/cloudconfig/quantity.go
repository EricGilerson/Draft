package cloudconfig

import (
	"strconv"
	"strings"
)

// Kubernetes-style quantity helpers shared by the Cloud Run and Container Apps
// adapters, which both express CPU as cores (or millicores) and memory with
// binary/decimal suffixes.

// k8sCPUToMilli parses "1", "0.5", or "500m" into millicores.
func k8sCPUToMilli(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	if strings.HasSuffix(s, "m") {
		if n, err := strconv.Atoi(strings.TrimSuffix(s, "m")); err == nil {
			return n
		}
		return 0
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return int(f * 1000)
	}
	return 0
}

// milliToK8sCPU renders millicores as a cores string ("1", "0.5"); it never
// uses the "m" form so the output reads naturally in a service.yaml.
func milliToK8sCPU(milli int) string {
	return strconv.FormatFloat(float64(milli)/1000, 'g', -1, 64)
}

// k8sMemToBytes parses "512Mi", "1Gi", "512M", "1G", or a bare byte count.
func k8sMemToBytes(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	type unit struct {
		suffix string
		mult   int64
	}
	// Order matters: check the two-char binary suffixes before the one-char ones.
	units := []unit{
		{"Ki", 1 << 10}, {"Mi", 1 << 20}, {"Gi", 1 << 30}, {"Ti", 1 << 40},
		{"K", 1000}, {"M", 1000 * 1000}, {"G", 1000 * 1000 * 1000}, {"T", 1000 * 1000 * 1000 * 1000},
	}
	for _, u := range units {
		if strings.HasSuffix(s, u.suffix) {
			if n, err := strconv.ParseFloat(strings.TrimSuffix(s, u.suffix), 64); err == nil {
				return int64(n * float64(u.mult))
			}
		}
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n
	}
	return 0
}

// bytesToK8sMem renders bytes using binary suffixes (Mi/Gi), the form Cloud Run
// and Container Apps prefer.
func bytesToK8sMem(b int64) string {
	const (
		ki = 1 << 10
		mi = 1 << 20
		gi = 1 << 30
	)
	switch {
	case b%gi == 0:
		return strconv.FormatInt(b/gi, 10) + "Gi"
	case b%mi == 0:
		return strconv.FormatInt(b/mi, 10) + "Mi"
	case b%ki == 0:
		return strconv.FormatInt(b/ki, 10) + "Ki"
	default:
		return strconv.FormatInt(b, 10)
	}
}
