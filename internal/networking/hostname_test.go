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
