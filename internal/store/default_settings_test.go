package store

import "testing"

func TestParseDefaultSettings(t *testing.T) {
	m, err := ParseDefaultSettings(`{"route_protocol":"tcp","host_port":"5432"}`)
	if err != nil {
		t.Fatal(err)
	}
	if m["route_protocol"] != "tcp" || m["host_port"] != "5432" {
		t.Fatalf("got %+v", m)
	}
	empty, err := ParseDefaultSettings("")
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty: %v %+v", err, empty)
	}
	if _, err := ParseDefaultSettings("{bad"); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestBuiltinTCPWireDefaults(t *testing.T) {
	s, err := Open(MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	want := map[string]string{
		"PostgreSQL": "5432",
		"Redis":      "6379",
		"MySQL":      "3306",
		"MongoDB":    "27017",
		"Memcached":  "11211",
	}
	list, err := s.ListTemplates()
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]ServiceTemplate{}
	for _, tpl := range list {
		byName[tpl.Name] = tpl
	}
	for name, port := range want {
		tpl, ok := byName[name]
		if !ok {
			t.Fatalf("missing builtin %s", name)
		}
		m, err := ParseDefaultSettings(tpl.DefaultSettings)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if m["route_protocol"] != "tcp" {
			t.Errorf("%s route_protocol = %q", name, m["route_protocol"])
		}
		if m["host_port"] != port {
			t.Errorf("%s host_port = %q, want %s", name, m["host_port"], port)
		}
	}
	// Hybrids must not ship TCP defaults.
	for _, name := range []string{"Meilisearch", "MinIO", "RabbitMQ", "ClickHouse"} {
		tpl := byName[name]
		m, _ := ParseDefaultSettings(tpl.DefaultSettings)
		if m["route_protocol"] == "tcp" {
			t.Errorf("%s should not default to tcp", name)
		}
	}
}
