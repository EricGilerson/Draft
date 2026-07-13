package networking

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	"Draft/internal/store"
)

// ProxyListenPlan describes which loopback ports to try, in order, before the
// ephemeral 127.0.0.1:0 last resort.
type ProxyListenPlan struct {
	Mode         string
	PrimaryPort  int // custom mode only
	FallbackPort int // prefer80_fallback + custom
}

// ProxyListenPlanFromSettings reads the proxy port preference from app settings.
func ProxyListenPlanFromSettings(s *store.Store) ProxyListenPlan {
	plan := ProxyListenPlan{Mode: store.ProxyPortModePrefer80Fallback}
	if s == nil {
		return plan
	}
	mode, _ := s.GetAppSetting(store.AppSettingProxyPortMode)
	plan.Mode = NormalizeProxyPortMode(mode)
	plan.PrimaryPort = parseListenPort(mustSetting(s, store.AppSettingProxyPort))
	plan.FallbackPort = parseListenPort(mustSetting(s, store.AppSettingProxyFallbackPort))
	return plan
}

func mustSetting(s *store.Store, key string) string {
	v, _ := s.GetAppSetting(key)
	return v
}

// NormalizeProxyPortMode returns a known mode, defaulting to prefer80_fallback
// so public URLs use a stable port when 80 is unavailable.
func NormalizeProxyPortMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case store.ProxyPortModePrefer80:
		return store.ProxyPortModePrefer80
	case store.ProxyPortModeCustom:
		return store.ProxyPortModeCustom
	default:
		return store.ProxyPortModePrefer80Fallback
	}
}

func parseListenPort(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > 65535 {
		return 0
	}
	return n
}

// ProxyListenCandidates returns bind addresses to try in order, always ending
// with 127.0.0.1:0 (OS-assigned ephemeral) as the last resort.
func ProxyListenCandidates(plan ProxyListenPlan) []string {
	mode := NormalizeProxyPortMode(plan.Mode)
	ports := make([]int, 0, 3)
	add := func(p int) {
		if p < 1 || p > 65535 {
			return
		}
		for _, existing := range ports {
			if existing == p {
				return
			}
		}
		ports = append(ports, p)
	}

	switch mode {
	case store.ProxyPortModeCustom:
		add(plan.PrimaryPort)
		add(plan.FallbackPort)
	case store.ProxyPortModePrefer80Fallback:
		add(80)
		add(plan.FallbackPort)
	default:
		add(80)
	}

	out := make([]string, 0, len(ports)+1)
	for _, p := range ports {
		out = append(out, fmt.Sprintf("127.0.0.1:%d", p))
	}
	out = append(out, "127.0.0.1:0")
	return out
}

// StartRouterWithPlan creates and starts a Router, trying each listen candidate
// until one succeeds.
func StartRouterWithPlan(s *store.Store, plan ProxyListenPlan) (*Router, error) {
	var lastErr error
	for _, addr := range ProxyListenCandidates(plan) {
		r := NewRouter(s, addr)
		if err := r.Start(); err != nil {
			lastErr = err
			log.Printf("[draft-router] proxy listen %s unavailable: %v", addr, err)
			continue
		}
		if addr == "127.0.0.1:0" {
			log.Printf("[draft-router] proxy listening on ephemeral %s", r.proxy.Addr())
		} else {
			log.Printf("[draft-router] proxy listening on %s", r.proxy.Addr())
		}
		return r, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no proxy listen candidates")
	}
	return nil, lastErr
}

// StartRouterFromSettings starts the local reverse proxy using persisted
// proxy-port preferences.
func StartRouterFromSettings(s *store.Store) (*Router, error) {
	return StartRouterWithPlan(s, ProxyListenPlanFromSettings(s))
}
