package deploy

import (
	"testing"

	"Draft/internal/store"
)

func TestRecordDeploymentInputsSupportsBothScopesForSameKey(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	dep, err := s.CreateDeployment(&store.Deployment{NodeID: "n1", ProjectID: 1, Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	value := "value"
	if err := e.recordDeploymentInputs(dep.ID, deploymentEnv{
		RuntimeEnv: []string{"SHARED=value"},
		BuildArgs:  map[string]*string{"SHARED": &value},
	}); err != nil {
		t.Fatalf("recordDeploymentInputs: %v", err)
	}
	inputs, err := s.ListDeploymentInputs(dep.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 2 || inputs[0].Scope != "build" || inputs[1].Scope != "runtime" {
		t.Fatalf("unexpected inputs: %+v", inputs)
	}
}
