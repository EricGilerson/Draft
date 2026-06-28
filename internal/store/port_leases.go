package store

import (
	"errors"
	"strings"
)

var ErrInvalidPortLease = errors.New("port lease requires port, project ID, and node ID")

func (s *Store) CreatePortLease(lease *PortLease) (*PortLease, error) {
	lease.NodeID = strings.TrimSpace(lease.NodeID)
	if lease.Port == 0 || lease.ProjectID == 0 || lease.NodeID == "" {
		return nil, ErrInvalidPortLease
	}
	if err := s.DB.Create(lease).Error; err != nil {
		return nil, err
	}
	return lease, nil
}

func (s *Store) GetPortLease(port int) (*PortLease, error) {
	var l PortLease
	if err := s.DB.Where("port = ?", port).First(&l).Error; err != nil {
		return nil, err
	}
	return &l, nil
}

func (s *Store) ListPortLeasesByProject(projectID uint) ([]PortLease, error) {
	var leases []PortLease
	if err := s.DB.Where("project_id = ?", projectID).Order("port asc").Find(&leases).Error; err != nil {
		return nil, err
	}
	return leases, nil
}

func (s *Store) DeletePortLease(port int) error {
	return s.DB.Delete(&PortLease{}, "port = ?", port).Error
}

func (s *Store) DeletePortLeasesByNode(nodeID string) error {
	return s.DB.Delete(&PortLease{}, "node_id = ?", nodeID).Error
}

func (s *Store) ListAllPortLeases() ([]PortLease, error) {
	var leases []PortLease
	if err := s.DB.Order("port asc").Find(&leases).Error; err != nil {
		return nil, err
	}
	return leases, nil
}
