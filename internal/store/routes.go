package store

import (
	"errors"
	"strings"
)

var ErrInvalidRoute = errors.New("route requires hostname, project ID, and node ID")

func (s *Store) CreateRoute(route *Route) (*Route, error) {
	route.Hostname = strings.TrimSpace(route.Hostname)
	route.NodeID = strings.TrimSpace(route.NodeID)
	if route.Hostname == "" || route.ProjectID == 0 || route.NodeID == "" {
		return nil, ErrInvalidRoute
	}
	if err := s.DB.Create(route).Error; err != nil {
		return nil, err
	}
	return route, nil
}

func (s *Store) GetRoute(hostname string) (*Route, error) {
	var r Route
	if err := s.DB.Where("hostname = ?", hostname).First(&r).Error; err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *Store) ListRoutesByProject(projectID uint) ([]Route, error) {
	var routes []Route
	if err := s.DB.Where("project_id = ?", projectID).Order("created_at asc").Find(&routes).Error; err != nil {
		return nil, err
	}
	return routes, nil
}

func (s *Store) ListRoutesByNode(nodeID string) ([]Route, error) {
	var routes []Route
	if err := s.DB.Where("node_id = ?", nodeID).Order("created_at asc").Find(&routes).Error; err != nil {
		return nil, err
	}
	return routes, nil
}

func (s *Store) DeleteRoute(hostname string) error {
	return s.DB.Delete(&Route{}, "hostname = ?", hostname).Error
}

func (s *Store) DeleteRoutesByNode(nodeID string) error {
	return s.DB.Delete(&Route{}, "node_id = ?", nodeID).Error
}

func (s *Store) ListAllRoutes() ([]Route, error) {
	var routes []Route
	if err := s.DB.Order("created_at asc").Find(&routes).Error; err != nil {
		return nil, err
	}
	return routes, nil
}
