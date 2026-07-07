package daemon

import (
	"net/http"
	"strconv"

	"Draft/internal/store"
)

// RouteRow is a Route enriched with the owning project name and service label so
// the Routes tab can render a single table without a second round-trip per row.
type RouteRow struct {
	store.Route
	ProjectName string `json:"projectName"`
	ServiceName string `json:"serviceName"`
}

// handleListRoutes returns every Route, optionally filtered by ?projectId=.
// Read-only in v1: the routes themselves are created implicitly at deploy time
// and removed when a service is stopped/deleted. A future v2 will add custom
// ingress rules on top of this same view.
func (s *Server) handleListRoutes(w http.ResponseWriter, r *http.Request) {
	var routes []store.Route
	if v := r.URL.Query().Get("projectId"); v != "" {
		pid, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			http.Error(w, "bad projectId", http.StatusBadRequest)
			return
		}
		routes, err = s.store.ListRoutesByProject(uint(pid))
		if err != nil {
			writeError(w, err)
			return
		}
	} else {
		var err error
		routes, err = s.store.ListAllRoutes()
		if err != nil {
			writeError(w, err)
			return
		}
	}

	projects, _ := s.store.ListProjects()
	projectByID := make(map[uint]store.Project, len(projects))
	for _, p := range projects {
		projectByID[p.ID] = p
	}

	rows := make([]RouteRow, 0, len(routes))
	for _, rt := range routes {
		row := RouteRow{Route: rt}
		if p, ok := projectByID[rt.ProjectID]; ok {
			row.ProjectName = p.Name
		}
		if node, err := s.store.GetNode(rt.NodeID); err == nil {
			row.ServiceName = node.Label
		}
		rows = append(rows, row)
	}
	writeJSON(w, rows)
}
