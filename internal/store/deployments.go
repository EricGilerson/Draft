package store

import "gorm.io/gorm"

func (s *Store) CreateDeployment(d *Deployment) (*Deployment, error) {
	if d.Sequence == 0 {
		seq, err := s.nextDeploymentSequence(d.NodeID)
		if err != nil {
			return nil, err
		}
		d.Sequence = seq
	}
	if err := s.DB.Create(d).Error; err != nil {
		return nil, err
	}
	return d, nil
}

// nextDeploymentSequence returns the next 1-based sequence number for a node's
// deploy history (count of existing rows for the node + 1). Deployments are
// never deleted, so this is monotonic per service.
func (s *Store) nextDeploymentSequence(nodeID string) (int, error) {
	var count int64
	if err := s.DB.Model(&Deployment{}).Where("node_id = ?", nodeID).Count(&count).Error; err != nil {
		return 0, err
	}
	return int(count) + 1, nil
}

func (s *Store) GetDeployment(id uint) (*Deployment, error) {
	var d Deployment
	if err := s.DB.First(&d, id).Error; err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *Store) UpdateDeployment(d *Deployment) error {
	return s.DB.Save(d).Error
}

// ActiveDeployment returns the most recent live deployment for a node, or nil
// if none exists. Terminal statuses (stopped, failed, interrupted) are excluded
// so a cut-short build cannot block Stop/Restart/idle or look "active".
func (s *Store) ActiveDeployment(nodeID string) (*Deployment, error) {
	var d Deployment
	err := s.DB.
		Where("node_id = ? AND status NOT IN ?", nodeID, []string{"stopped", "failed", "interrupted"}).
		Order("created_at desc").
		First(&d).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// LatestDeployment returns the most recent deployment for a node regardless of
// status, or nil if none exists. Used by the git-trigger reconciler to read the
// last SourceSHA it built for the node.
func (s *Store) LatestDeployment(nodeID string) (*Deployment, error) {
	var d Deployment
	err := s.DB.Where("node_id = ?", nodeID).Order("created_at desc").First(&d).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// DeleteDeploymentsByNode removes every deployment row for a node.
func (s *Store) DeleteDeploymentsByNode(nodeID string) error {
	return s.DB.Delete(&Deployment{}, "node_id = ?", nodeID).Error
}

func (s *Store) ListDeployments(nodeID string) ([]Deployment, error) {
	return s.ListDeploymentsLimited(nodeID, 0)
}

// ListDeploymentsLimited returns recent deployments for a node. limit <= 0
// means unbounded (used by teardown/rollback paths that must see every row).
func (s *Store) ListDeploymentsLimited(nodeID string, limit int) ([]Deployment, error) {
	return s.ListDeploymentsPage(nodeID, limit, 0)
}

// ListDeploymentsPage returns a window of deployments ordered newest-first.
// limit <= 0 means unbounded (offset ignored). offset < 0 is treated as 0.
func (s *Store) ListDeploymentsPage(nodeID string, limit, offset int) ([]Deployment, error) {
	var deployments []Deployment
	q := s.DB.Where("node_id = ?", nodeID).Order("created_at desc, id desc")
	if limit > 0 {
		if offset < 0 {
			offset = 0
		}
		q = q.Limit(limit).Offset(offset)
	}
	if err := q.Find(&deployments).Error; err != nil {
		return nil, err
	}
	return deployments, nil
}

// CountDeployments returns how many deployment rows exist for a node.
func (s *Store) CountDeployments(nodeID string) (int64, error) {
	var count int64
	if err := s.DB.Model(&Deployment{}).Where("node_id = ?", nodeID).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// ListActiveDeploymentsByNodes returns the newest non-terminal deployment for
// each requested node ID (at most one row per node).
func (s *Store) ListActiveDeploymentsByNodes(nodeIDs []string) (map[string]*Deployment, error) {
	out := make(map[string]*Deployment, len(nodeIDs))
	if len(nodeIDs) == 0 {
		return out, nil
	}
	var deployments []Deployment
	if err := s.DB.
		Where("node_id IN ? AND status NOT IN ?", nodeIDs, []string{"stopped", "failed", "interrupted"}).
		Order("created_at desc").
		Find(&deployments).Error; err != nil {
		return nil, err
	}
	for i := range deployments {
		d := &deployments[i]
		if _, ok := out[d.NodeID]; ok {
			continue
		}
		out[d.NodeID] = d
	}
	return out, nil
}

func (s *Store) ListAllDeployments() ([]Deployment, error) {
	var deployments []Deployment
	if err := s.DB.Order("created_at desc").Find(&deployments).Error; err != nil {
		return nil, err
	}
	return deployments, nil
}

func (s *Store) ListActiveDeployments() ([]Deployment, error) {
	var deployments []Deployment
	if err := s.DB.
		Where("status NOT IN ?", []string{"stopped", "failed", "interrupted"}).
		Order("created_at desc").
		Find(&deployments).Error; err != nil {
		return nil, err
	}
	return deployments, nil
}
