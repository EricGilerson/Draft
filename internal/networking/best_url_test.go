package networking

import (
	"strings"
	"testing"

	"Draft/internal/store"
)

func TestBestDeploymentURL(t *testing.T) {
	dep := &store.Deployment{
		Hostname: "api.app.default.abcd.draft.local",
		HostPort: 49152,
	}

	tests := []struct {
		name     string
		mode     LocalDomainStatus
		protocol string
		want     string
	}{
		{
			name:     "http public hostname with proxy port",
			mode:     LocalDomainStatus{Mode: "public-hostname-port", ProxyPort: 53888},
			protocol: "http",
			want:     "http://api.app.default.abcd.draft.resolv.sh:53888",
		},
		{
			name:     "http localhost port fallback without proxy",
			mode:     LocalDomainStatus{Mode: "localhost-port"},
			protocol: "http",
			want:     "http://127.0.0.1:49152",
		},
		{
			name:     "tcp uses public hostname and host port not proxy",
			mode:     LocalDomainStatus{Mode: "public-hostname-port", ProxyPort: 53888},
			protocol: "tcp",
			want:     "api.app.default.abcd.draft.resolv.sh:49152",
		},
		{
			name:     "tcp without hostname falls back to loopback host port",
			mode:     LocalDomainStatus{Mode: "public-hostname-port", ProxyPort: 53888},
			protocol: "tcp",
			want:     "api.app.default.abcd.draft.resolv.sh:49152",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BestDeploymentURL(dep, tt.mode, tt.protocol); got != tt.want {
				t.Fatalf("BestDeploymentURL = %q, want %q", got, tt.want)
			}
		})
	}

	t.Run("tcp host port only", func(t *testing.T) {
		d := &store.Deployment{HostPort: 5432}
		got := BestDeploymentURL(d, LocalDomainStatus{ProxyPort: 80}, "tcp")
		if got != "127.0.0.1:5432" {
			t.Fatalf("got %q", got)
		}
	})
}

func TestServicePublicAndInternalURL(t *testing.T) {
	host := "db.app.default.abcd.draft.local"
	if got := ServicePublicURL(host, 53888, 5432, "tcp"); got != "db.app.default.abcd.draft.resolv.sh:5432" {
		t.Fatalf("tcp public = %q", got)
	}
	if got := ServicePublicURL(host, 53888, 5432, "http"); got != "http://db.app.default.abcd.draft.resolv.sh:53888" {
		t.Fatalf("http public = %q", got)
	}
	if got := ServiceInternalURL(host, "5432", "tcp"); got != "db.app.default.abcd.draft.local:5432" {
		t.Fatalf("tcp internal = %q", got)
	}
	if strings.HasPrefix(ServiceInternalURL(host, "5432", "tcp"), "http") {
		t.Fatal("tcp internal must not use http scheme")
	}
	if got := ServiceInternalURL(host, "3000", "http"); got != "http://db.app.default.abcd.draft.local:3000" {
		t.Fatalf("http internal = %q", got)
	}
}
