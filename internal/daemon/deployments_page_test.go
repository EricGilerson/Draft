package daemon

import (
	"context"
	"testing"

	"Draft/internal/store"
)

func TestClientGetDeploymentsPage(t *testing.T) {
	srv, s, _ := newTestServer(t)
	ts := newIPv4Server(t, srv.routes())
	c := clientForHTTPServer(t, ts, srv.state.Token)

	for i := 0; i < 5; i++ {
		if _, err := s.CreateDeployment(&store.Deployment{
			NodeID:    "svc1",
			ProjectID: 1,
			Status:    "stopped",
		}); err != nil {
			t.Fatal(err)
		}
	}

	page, err := c.GetDeploymentsPage(context.Background(), "svc1", 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if page == nil || len(page.Deployments) != 2 || page.Total != 5 {
		t.Fatalf("page = %+v", page)
	}
	if page.Limit != 2 || page.Offset != 0 || !page.HasMore {
		t.Fatalf("meta = limit=%d offset=%d hasMore=%v", page.Limit, page.Offset, page.HasMore)
	}

	page2, err := c.GetDeploymentsPage(context.Background(), "svc1", 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page2.Deployments) != 2 {
		t.Fatalf("page2 len = %d", len(page2.Deployments))
	}
	if page.Deployments[0].ID == page2.Deployments[0].ID {
		t.Fatal("expected distinct pages")
	}

	// Legacy list remains first-page convenience (array, not page object).
	deps, err := c.GetDeployments(context.Background(), "svc1")
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) == 0 || len(deps) > 50 {
		t.Fatalf("legacy GetDeployments len = %d", len(deps))
	}
}
