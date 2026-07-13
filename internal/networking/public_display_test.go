package networking

import "testing"

func TestHostnameWithSuffix(t *testing.T) {
	in := "api.app.default.abcd.draft.local"
	if got := HostnameWithSuffix(in, LocalSuffix); got != "api.app.default.abcd.draft" {
		t.Fatalf("local suffix = %q", got)
	}
	if got := HostnameWithSuffix(in, PublicSuffix); got != "api.app.default.abcd.draft.resolv.sh" {
		t.Fatalf("public suffix = %q", got)
	}
	if got := HostnameWithSuffix(in, ""); got != "api.app.default.abcd.draft.resolv.sh" {
		t.Fatalf("empty suffix = %q", got)
	}
}

func TestBestServiceURLPrefersCachedSuffix(t *testing.T) {
	host := "api.app.default.abcd.draft.local"
	local := LocalDomainStatus{
		Mode:         "public-hostname-port",
		ProxyPort:    53888,
		PublicSuffix: LocalSuffix,
		DNSVerified:  true,
	}
	got := BestServiceURL(host, 49152, "http", local)
	want := "http://api.app.default.abcd.draft:53888"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}

	local.PublicSuffix = PublicSuffix
	got = BestServiceURL(host, 49152, "http", local)
	want = "http://api.app.default.abcd.draft.resolv.sh:53888"
	if got != want {
		t.Fatalf("resolv = %q want %q", got, want)
	}

	local.Mode = "localhost-port"
	got = BestServiceURL(host, 49152, "http", local)
	if got != "http://127.0.0.1:49152" {
		t.Fatalf("localhost = %q", got)
	}
}

func TestBestServiceURLTCP(t *testing.T) {
	host := "db.app.default.abcd.draft.local"
	local := LocalDomainStatus{
		Mode:         "public-hostname-port",
		ProxyPort:    53888,
		PublicSuffix: LocalSuffix,
	}
	got := BestServiceURL(host, 5432, "tcp", local)
	if got != "db.app.default.abcd.draft:5432" {
		t.Fatalf("tcp = %q", got)
	}
}
