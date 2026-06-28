package store

import "gorm.io/gorm"

func (s *Store) CreateDeployment(d *Deployment) (*Deployment, error) {
	if err := s.DB.Create(d).Error; err != nil {
		return nil, err
	}
	return d, nil
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

// ActiveDeployment returns the most recent non-stopped deployment for a node,
// or nil if none exists.
func (s *Store) ActiveDeployment(nodeID string) (*Deployment, error) {
	var d Deployment
	err := s.DB.
		Where("node_id = ? AND status NOT IN ?", nodeID, []string{"stopped", "failed"}).
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

func (s *Store) ListDeployments(nodeID string) ([]Deployment, error) {
	var deployments []Deployment
	if err := s.DB.Where("node_id = ?", nodeID).Order("created_at desc").Find(&deployments).Error; err != nil {
		return nil, err
	}
	return deployments, nil
}
