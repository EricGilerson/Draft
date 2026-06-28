package store

import "testing"

func TestCreateAndGetRoute(t *testing.T) {
	s := openTemp(t)

	route := &Route{
		Hostname:    "api.myapp.default.a3f2.draft.local",
		ProjectID:   1,
		NodeID:      "svc-0001",
		Environment: "default",
		Protocol:    "http",
		TargetHost:  "127.0.0.1",
		TargetPort:  3000,
	}
	created, err := s.CreateRoute(route)
	if err != nil {
		t.Fatalf("CreateRoute: %v", err)
	}
	if created.Hostname != route.Hostname {
		t.Errorf("Hostname = %q, want %q", created.Hostname, route.Hostname)
	}

	got, err := s.GetRoute(route.Hostname)
	if err != nil {
		t.Fatalf("GetRoute: %v", err)
	}
	if got.NodeID != "svc-0001" {
		t.Errorf("NodeID = %q, want svc-0001", got.NodeID)
	}
	if got.TargetPort != 3000 {
		t.Errorf("TargetPort = %d, want 3000", got.TargetPort)
	}
}

func TestCreateRouteValidation(t *testing.T) {
	s := openTemp(t)

	cases := []struct {
		name  string
		route *Route
	}{
		{"empty hostname", &Route{ProjectID: 1, NodeID: "n1"}},
		{"zero project", &Route{Hostname: "x.draft.local", NodeID: "n1"}},
		{"empty node", &Route{Hostname: "x.draft.local", ProjectID: 1}},
	}
	for _, tc := range cases {
		if _, err := s.CreateRoute(tc.route); err != ErrInvalidRoute {
			t.Errorf("%s: got %v, want ErrInvalidRoute", tc.name, err)
		}
	}
}

func TestCreateRouteDuplicateHostname(t *testing.T) {
	s := openTemp(t)

	r := &Route{Hostname: "api.x.default.aaaa.draft.local", ProjectID: 1, NodeID: "n1", Environment: "default", Protocol: "http", TargetHost: "127.0.0.1", TargetPort: 3000}
	if _, err := s.CreateRoute(r); err != nil {
		t.Fatal(err)
	}
	r2 := &Route{Hostname: "api.x.default.aaaa.draft.local", ProjectID: 1, NodeID: "n2", Environment: "default", Protocol: "http", TargetHost: "127.0.0.1", TargetPort: 4000}
	if _, err := s.CreateRoute(r2); err == nil {
		t.Error("duplicate hostname should fail")
	}
}

func TestListRoutesByProject(t *testing.T) {
	s := openTemp(t)

	s.CreateRoute(&Route{Hostname: "a.p1.default.aaaa.draft.local", ProjectID: 1, NodeID: "n1", Environment: "default", Protocol: "http", TargetHost: "127.0.0.1", TargetPort: 3000})
	s.CreateRoute(&Route{Hostname: "b.p1.default.aaaa.draft.local", ProjectID: 1, NodeID: "n2", Environment: "default", Protocol: "http", TargetHost: "127.0.0.1", TargetPort: 3001})
	s.CreateRoute(&Route{Hostname: "c.p2.default.bbbb.draft.local", ProjectID: 2, NodeID: "n3", Environment: "default", Protocol: "http", TargetHost: "127.0.0.1", TargetPort: 3002})

	routes, err := s.ListRoutesByProject(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 2 {
		t.Errorf("got %d routes for project 1, want 2", len(routes))
	}
}

func TestListRoutesByNode(t *testing.T) {
	s := openTemp(t)

	s.CreateRoute(&Route{Hostname: "a.p1.default.aaaa.draft.local", ProjectID: 1, NodeID: "n1", Environment: "default", Protocol: "http", TargetHost: "127.0.0.1", TargetPort: 3000})
	s.CreateRoute(&Route{Hostname: "a.p1.staging.aaaa.draft.local", ProjectID: 1, NodeID: "n1", Environment: "staging", Protocol: "http", TargetHost: "127.0.0.1", TargetPort: 3001})
	s.CreateRoute(&Route{Hostname: "b.p1.default.aaaa.draft.local", ProjectID: 1, NodeID: "n2", Environment: "default", Protocol: "http", TargetHost: "127.0.0.1", TargetPort: 3002})

	routes, err := s.ListRoutesByNode("n1")
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 2 {
		t.Errorf("got %d routes for node n1, want 2", len(routes))
	}
}

func TestDeleteRoute(t *testing.T) {
	s := openTemp(t)

	s.CreateRoute(&Route{Hostname: "a.p1.default.aaaa.draft.local", ProjectID: 1, NodeID: "n1", Environment: "default", Protocol: "http", TargetHost: "127.0.0.1", TargetPort: 3000})
	if err := s.DeleteRoute("a.p1.default.aaaa.draft.local"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetRoute("a.p1.default.aaaa.draft.local"); err == nil {
		t.Error("route should be deleted")
	}
}

func TestDeleteRoutesByNode(t *testing.T) {
	s := openTemp(t)

	s.CreateRoute(&Route{Hostname: "a.p1.default.aaaa.draft.local", ProjectID: 1, NodeID: "n1", Environment: "default", Protocol: "http", TargetHost: "127.0.0.1", TargetPort: 3000})
	s.CreateRoute(&Route{Hostname: "b.p1.default.aaaa.draft.local", ProjectID: 1, NodeID: "n1", Environment: "default", Protocol: "http", TargetHost: "127.0.0.1", TargetPort: 3001})
	s.CreateRoute(&Route{Hostname: "c.p1.default.aaaa.draft.local", ProjectID: 1, NodeID: "n2", Environment: "default", Protocol: "http", TargetHost: "127.0.0.1", TargetPort: 3002})

	if err := s.DeleteRoutesByNode("n1"); err != nil {
		t.Fatal(err)
	}

	all, _ := s.ListAllRoutes()
	if len(all) != 1 {
		t.Errorf("got %d routes, want 1 (only n2's route)", len(all))
	}
	if all[0].NodeID != "n2" {
		t.Errorf("remaining route node = %q, want n2", all[0].NodeID)
	}
}
