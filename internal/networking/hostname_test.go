package networking

import "testing"

func TestSanitize(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"MyApp", "myapp"},
		{"my app", "my-app"},
		{"  hello  ", "hello"},
		{"foo--bar", "foo-bar"},
		{"My Cool App!", "my-cool-app"},
		{"---", "untitled"},
		{"", "untitled"},
		{"a.b.c", "a-b-c"},
	}
	for _, tt := range tests {
		if got := sanitize(tt.in); got != tt.want {
			t.Errorf("sanitize(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestHostname(t *testing.T) {
	h := Hostname("api", "myapp", "default", "a3f2")
	want := "api.myapp.default.a3f2.draft.local"
	if h != want {
		t.Errorf("Hostname = %q, want %q", h, want)
	}
}

func TestHostnameSpecialChars(t *testing.T) {
	h := Hostname("My API", "Cool App!", "staging", "b1c2")
	want := "my-api.cool-app.staging.b1c2.draft.local"
	if h != want {
		t.Errorf("Hostname = %q, want %q", h, want)
	}
}

func TestPublicHostname(t *testing.T) {
	h := PublicHostname("api.myapp.default.a3f2.draft.local")
	want := "api.myapp.default.a3f2.draft.resolv.sh"
	if h != want {
		t.Errorf("PublicHostname = %q, want %q", h, want)
	}
}

func TestInternalAndPublicURL(t *testing.T) {
	hostname := "api.myapp.default.a3f2.draft.local"
	if got := InternalURL(hostname, "3000"); got != "http://api.myapp.default.a3f2.draft.local:3000" {
		t.Errorf("InternalURL = %q", got)
	}
	if got := PublicURL(hostname, 53888); got != "http://api.myapp.default.a3f2.draft.resolv.sh:53888" {
		t.Errorf("PublicURL = %q", got)
	}
}

func TestPublicURLOmitsDefaultHTTPPort(t *testing.T) {
	hostname := "api.myapp.default.a3f2.draft.local"
	if got := PublicURL(hostname, 80); got != "http://api.myapp.default.a3f2.draft.resolv.sh" {
		t.Errorf("PublicURL = %q", got)
	}
}

func TestHostAliases(t *testing.T) {
	aliases := HostAliases("api.myapp.default.a3f2.draft.local")
	if len(aliases) != 3 {
		t.Fatalf("len(HostAliases) = %d, want 3: %+v", len(aliases), aliases)
	}
	if aliases[0] != "api.myapp.default.a3f2.draft.local" {
		t.Errorf("primary alias = %q", aliases[0])
	}
	if aliases[1] != "api.myapp.default.a3f2.draft" {
		t.Errorf("local alias = %q", aliases[1])
	}
	if aliases[2] != "api.myapp.default.a3f2.draft.resolv.sh" {
		t.Errorf("public alias = %q", aliases[2])
	}
}

func TestParseHostname(t *testing.T) {
	p := ParseHostname("api.myapp.default.a3f2.draft.local")
	if p == nil {
		t.Fatal("ParseHostname returned nil")
	}
	if p.Service != "api" || p.Project != "myapp" || p.Environment != "default" || p.UID != "a3f2" {
		t.Errorf("parsed = %+v", p)
	}
}

func TestParseHostnameInvalid(t *testing.T) {
	cases := []string{
		"not-a-draft-hostname.example.com",
		"draft.local",
		"a.draft.local",
		"a.b.draft.local",
		"",
	}
	for _, c := range cases {
		if p := ParseHostname(c); p != nil {
			t.Errorf("ParseHostname(%q) should be nil, got %+v", c, p)
		}
	}
}

func TestGenerateUID(t *testing.T) {
	uid := GenerateUID()
	if len(uid) != 4 {
		t.Errorf("GenerateUID() = %q, want 4-char hex", uid)
	}
	uid2 := GenerateUID()
	if uid == uid2 {
		t.Errorf("two UIDs are identical: %s", uid)
	}
}
