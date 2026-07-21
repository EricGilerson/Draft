package daemon

import "testing"

func TestShouldPublishDockerActivity(t *testing.T) {
	tests := []struct {
		name   string
		typ    string
		action string
		attrs  map[string]string
		want   bool
	}{
		{name: "draft labeled container", typ: "container", action: "create", attrs: map[string]string{"draft.deployment": "1"}, want: true},
		{name: "draft managed volume", typ: "volume", action: "create", attrs: map[string]string{"draft.managed": "true"}, want: true},
		{name: "draft name prefix", typ: "container", action: "create", attrs: map[string]string{"name": "draft-demo-api-1"}, want: true},
		{name: "draft image prefix", typ: "image", action: "tag", attrs: map[string]string{"image": "draft-demo-api:1"}, want: true},
		{name: "container start always", typ: "container", action: "start", attrs: map[string]string{"name": "redis"}, want: true},
		{name: "container die always", typ: "container", action: "die", attrs: map[string]string{}, want: true},
		{name: "container create without draft", typ: "container", action: "create", attrs: map[string]string{"name": "redis"}, want: false},
		{name: "image pull spam", typ: "image", action: "pull", attrs: map[string]string{"name": "alpine"}, want: false},
		{name: "unrelated type", typ: "plugin", action: "install", attrs: nil, want: false},
		{name: "nil attrs container", typ: "container", action: "create", attrs: nil, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldPublishDockerActivity(tt.typ, tt.action, tt.attrs)
			if got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCopyStringMapIndependent(t *testing.T) {
	in := map[string]string{"a": "1"}
	out := copyStringMap(in)
	out["a"] = "2"
	out["b"] = "3"
	if in["a"] != "1" {
		t.Fatal("copy mutated original")
	}
	if _, ok := in["b"]; ok {
		t.Fatal("copy leaked key into original")
	}
	if copyStringMap(nil) != nil {
		t.Fatal("nil input should yield nil")
	}
}
