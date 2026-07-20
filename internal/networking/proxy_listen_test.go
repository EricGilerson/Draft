package networking

import (
	"strings"
	"testing"

	"Draft/internal/store"
)

func TestProxyListenCandidates(t *testing.T) {
	tests := []struct {
		name string
		plan ProxyListenPlan
		want []string
	}{
		{
			name: "sticky runtime port wins before configured ports",
			plan: ProxyListenPlan{Mode: store.ProxyPortModePrefer80Fallback, FallbackPort: 38473, StickyPort: 50467},
			want: []string{"127.0.0.1:50467", "127.0.0.1:80", "127.0.0.1:38473", "127.0.0.1:0"},
		},
		{
			name: "prefer80",
			plan: ProxyListenPlan{Mode: store.ProxyPortModePrefer80},
			want: []string{"127.0.0.1:80", "127.0.0.1:0"},
		},
		{
			name: "prefer80 with fallback",
			plan: ProxyListenPlan{Mode: store.ProxyPortModePrefer80Fallback, FallbackPort: 38473},
			want: []string{"127.0.0.1:80", "127.0.0.1:38473", "127.0.0.1:0"},
		},
		{
			name: "prefer80 fallback ignores duplicate 80",
			plan: ProxyListenPlan{Mode: store.ProxyPortModePrefer80Fallback, FallbackPort: 80},
			want: []string{"127.0.0.1:80", "127.0.0.1:0"},
		},
		{
			name: "custom primary and fallback",
			plan: ProxyListenPlan{Mode: store.ProxyPortModeCustom, PrimaryPort: 9000, FallbackPort: 38473},
			want: []string{"127.0.0.1:9000", "127.0.0.1:38473", "127.0.0.1:0"},
		},
		{
			name: "custom primary only",
			plan: ProxyListenPlan{Mode: store.ProxyPortModeCustom, PrimaryPort: 9000},
			want: []string{"127.0.0.1:9000", "127.0.0.1:0"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ProxyListenCandidates(tt.plan)
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Fatalf("got %v want %v", got, tt.want)
			}
		})
	}
}

func TestProxyListenPlanFromSettings(t *testing.T) {
	s := openTestStore(t)
	_ = s.SetAppSetting(store.AppSettingProxyPortMode, store.ProxyPortModeCustom)
	_ = s.SetAppSetting(store.AppSettingProxyPort, "9001")
	_ = s.SetAppSetting(store.AppSettingProxyFallbackPort, "9002")
	_ = s.SetAppSetting(store.AppSettingProxyBoundPort, "9003")

	plan := ProxyListenPlanFromSettings(s)
	if plan.Mode != store.ProxyPortModeCustom || plan.PrimaryPort != 9001 || plan.FallbackPort != 9002 || plan.StickyPort != 9003 {
		t.Fatalf("plan = %+v", plan)
	}
}
