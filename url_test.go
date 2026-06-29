package main

import (
	"testing"

	"Draft/internal/networking"
	"Draft/internal/store"
)

func TestBestDeploymentURL(t *testing.T) {
	dep := &store.Deployment{
		Hostname: "api.app.default.abcd.draft.local",
		HostPort: 49152,
	}

	tests := []struct {
		name string
		mode networking.LocalDomainStatus
		want string
	}{
		{
			name: "full clean domain",
			mode: networking.LocalDomainStatus{Mode: "full", ProxyPort: 80},
			want: "http://api.app.default.abcd.draft.local",
		},
		{
			name: "hostname with proxy port",
			mode: networking.LocalDomainStatus{Mode: "hostname-port", ProxyPort: 53888},
			want: "http://api.app.default.abcd.draft.local:53888",
		},
		{
			name: "localhost fallback",
			mode: networking.LocalDomainStatus{Mode: "localhost-port", ProxyPort: 53888},
			want: "http://127.0.0.1:49152",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BestDeploymentURL(dep, tt.mode); got != tt.want {
				t.Fatalf("BestDeploymentURL = %q, want %q", got, tt.want)
			}
		})
	}
}
