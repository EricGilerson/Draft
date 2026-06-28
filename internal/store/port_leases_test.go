package store

import "testing"

func TestCreateAndGetPortLease(t *testing.T) {
	s := openTemp(t)

	lease := &PortLease{Port: 15432, ProjectID: 1, NodeID: "n1"}
	created, err := s.CreatePortLease(lease)
	if err != nil {
		t.Fatalf("CreatePortLease: %v", err)
	}
	if created.Port != 15432 {
		t.Errorf("Port = %d, want 15432", created.Port)
	}

	got, err := s.GetPortLease(15432)
	if err != nil {
		t.Fatalf("GetPortLease: %v", err)
	}
	if got.NodeID != "n1" {
		t.Errorf("NodeID = %q, want n1", got.NodeID)
	}
}

func TestCreatePortLeaseValidation(t *testing.T) {
	s := openTemp(t)

	cases := []struct {
		name  string
		lease *PortLease
	}{
		{"zero port", &PortLease{ProjectID: 1, NodeID: "n1"}},
		{"zero project", &PortLease{Port: 10000, NodeID: "n1"}},
		{"empty node", &PortLease{Port: 10000, ProjectID: 1}},
	}
	for _, tc := range cases {
		if _, err := s.CreatePortLease(tc.lease); err != ErrInvalidPortLease {
			t.Errorf("%s: got %v, want ErrInvalidPortLease", tc.name, err)
		}
	}
}

func TestCreatePortLeaseDuplicatePort(t *testing.T) {
	s := openTemp(t)

	s.CreatePortLease(&PortLease{Port: 15432, ProjectID: 1, NodeID: "n1"})
	if _, err := s.CreatePortLease(&PortLease{Port: 15432, ProjectID: 2, NodeID: "n2"}); err == nil {
		t.Error("duplicate port should fail")
	}
}

func TestListPortLeasesByProject(t *testing.T) {
	s := openTemp(t)

	s.CreatePortLease(&PortLease{Port: 10001, ProjectID: 1, NodeID: "n1"})
	s.CreatePortLease(&PortLease{Port: 10002, ProjectID: 1, NodeID: "n2"})
	s.CreatePortLease(&PortLease{Port: 10003, ProjectID: 2, NodeID: "n3"})

	leases, err := s.ListPortLeasesByProject(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(leases) != 2 {
		t.Errorf("got %d leases for project 1, want 2", len(leases))
	}
}

func TestDeletePortLease(t *testing.T) {
	s := openTemp(t)

	s.CreatePortLease(&PortLease{Port: 15432, ProjectID: 1, NodeID: "n1"})
	if err := s.DeletePortLease(15432); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetPortLease(15432); err == nil {
		t.Error("lease should be deleted")
	}
}

func TestDeletePortLeasesByNode(t *testing.T) {
	s := openTemp(t)

	s.CreatePortLease(&PortLease{Port: 10001, ProjectID: 1, NodeID: "n1"})
	s.CreatePortLease(&PortLease{Port: 10002, ProjectID: 1, NodeID: "n1"})
	s.CreatePortLease(&PortLease{Port: 10003, ProjectID: 1, NodeID: "n2"})

	if err := s.DeletePortLeasesByNode("n1"); err != nil {
		t.Fatal(err)
	}

	all, _ := s.ListAllPortLeases()
	if len(all) != 1 {
		t.Errorf("got %d leases, want 1", len(all))
	}
	if all[0].Port != 10003 {
		t.Errorf("remaining port = %d, want 10003", all[0].Port)
	}
}
