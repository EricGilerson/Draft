package store

import (
	"sort"

	"gorm.io/gorm"
)

// ReplaceDeploymentInputs atomically replaces a deployment's resolved input
// manifest. Callers provide digests, never plaintext values.
func (s *Store) ReplaceDeploymentInputs(deploymentID uint, inputs []DeploymentInput) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&DeploymentInput{}, "deployment_id = ?", deploymentID).Error; err != nil {
			return err
		}
		for i := range inputs {
			inputs[i].DeploymentID = deploymentID
		}
		if len(inputs) == 0 {
			return nil
		}
		return tx.Create(&inputs).Error
	})
}

func (s *Store) ListDeploymentInputs(deploymentID uint) ([]DeploymentInput, error) {
	var inputs []DeploymentInput
	err := s.DB.Where("deployment_id = ?", deploymentID).Find(&inputs).Error
	sort.Slice(inputs, func(i, j int) bool {
		if inputs[i].Scope != inputs[j].Scope {
			return inputs[i].Scope < inputs[j].Scope
		}
		return inputs[i].Key < inputs[j].Key
	})
	return inputs, err
}
