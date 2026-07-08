package cloudconfig

import (
	"strings"
	"testing"
)

const sampleCloudRun = `
apiVersion: serving.knative.dev/v1
kind: Service
metadata:
  name: api
spec:
  template:
    metadata:
      annotations:
        autoscaling.knative.dev/maxScale: "100"
    spec:
      containerConcurrency: 80
      containers:
      - image: gcr.io/proj/api:latest
        ports:
        - containerPort: 8080
        env:
        - name: LOG_LEVEL
          value: info
        - name: DB_URL
          valueFrom:
            secretKeyRef:
              name: db-url
              key: latest
        resources:
          limits:
            cpu: "1"
            memory: 512Mi
        startupProbe:
          httpGet:
            path: /healthz
            port: 8080
          periodSeconds: 5
`

func TestCloudRunImport(t *testing.T) {
	a := &cloudRunAdapter{}
	if !a.Detect("service.yaml", []byte(sampleCloudRun)) {
		t.Fatal("Detect failed")
	}
	specs, rep, err := a.Import([]byte(sampleCloudRun))
	if err != nil {
		t.Fatal(err)
	}
	s := specs[0]
	if s.Name != "api" || s.Image != "gcr.io/proj/api:latest" || !s.IsImageMode() {
		t.Errorf("bad spec: %+v", s)
	}
	if len(s.Ports) != 1 || s.Ports[0].Container != 8080 {
		t.Errorf("ports: %+v", s.Ports)
	}
	if s.Resources.MilliCPU != 1000 || s.Resources.MemoryBytes != 512*1024*1024 {
		t.Errorf("resources: %+v", s.Resources)
	}
	if s.Health == nil || s.Health.Kind != HealthHTTP || s.Health.Path != "/healthz" {
		t.Errorf("health: %+v", s.Health)
	}
	var dbSecret bool
	for _, e := range s.Env {
		if e.Key == "DB_URL" && e.Secret && e.Value == "" {
			dbSecret = true
		}
	}
	if !dbSecret {
		t.Errorf("DB_URL should be an empty secret: %+v", s.Env)
	}
	if !hasNote(rep, "concurrency") || !hasNote(rep, "autoscaling") {
		t.Errorf("expected control-plane notes: %+v", rep.Notes)
	}
}

func TestCloudRunExportBuildMode(t *testing.T) {
	spec := ServiceSpec{
		Name:  "api",
		Build: &BuildSpec{Context: "api", Dockerfile: "api/Dockerfile", Target: "runner", Args: []EnvVar{{Key: "NODE_ENV", Value: "production", Scope: ScopeBuild}}},
		Ports: []PortSpec{{Container: 8080, Protocol: "tcp"}},
		Env:   []EnvVar{{Key: "LOG_LEVEL", Value: "info", Scope: ScopeRuntime}},
	}
	a := &cloudRunAdapter{}
	files, _, err := a.Export([]ServiceSpec{spec})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := files["service.yaml"]; !ok {
		t.Fatal("missing service.yaml")
	}
	cb, ok := files["cloudbuild.yaml"]
	if !ok {
		t.Fatal("build-mode should emit cloudbuild.yaml")
	}
	s := string(cb)
	if !strings.Contains(s, "api/Dockerfile") || !strings.Contains(s, "--target") || !strings.Contains(s, "NODE_ENV=production") {
		t.Errorf("cloudbuild missing recipe:\n%s", s)
	}
}

const sampleECS = `{
  "family": "api",
  "containerDefinitions": [
    {
      "name": "api",
      "image": "123.dkr.ecr.us-east-1.amazonaws.com/api:latest",
      "essential": true,
      "portMappings": [{"containerPort": 8080, "protocol": "tcp"}],
      "cpu": 512,
      "memory": 1024,
      "environment": [{"name": "LOG_LEVEL", "value": "info"}],
      "secrets": [{"name": "DB_URL", "valueFrom": "arn:aws:ssm:...:parameter/db"}],
      "healthCheck": {"command": ["CMD-SHELL", "curl -f http://localhost:8080/health || exit 1"], "interval": 30, "retries": 3},
      "linuxParameters": {"initProcessEnabled": true, "capabilities": {"add": ["NET_ADMIN"]}}
    }
  ],
  "volumes": []
}`

func TestECSImport(t *testing.T) {
	a := &ecsAdapter{}
	if !a.Detect("taskdef.json", []byte(sampleECS)) {
		t.Fatal("Detect failed")
	}
	specs, _, err := a.Import([]byte(sampleECS))
	if err != nil {
		t.Fatal(err)
	}
	s := specs[0]
	if s.Name != "api" || s.Ports[0].Container != 8080 {
		t.Errorf("spec: %+v", s)
	}
	// 512 ECS units = 500 millicores.
	if s.Resources.MilliCPU != 500 || s.Resources.MemoryBytes != 1024*1024*1024 {
		t.Errorf("resources: %+v", s.Resources)
	}
	if s.Health == nil || s.Health.Kind != HealthCmd || s.Health.Retries != 3 {
		t.Errorf("health: %+v", s.Health)
	}
	if !s.Security.Init || len(s.Security.CapAdd) != 1 {
		t.Errorf("security: %+v", s.Security)
	}
	var secret bool
	for _, e := range s.Env {
		if e.Key == "DB_URL" && e.Secret {
			secret = true
		}
	}
	if !secret {
		t.Errorf("DB_URL secret: %+v", s.Env)
	}
}

func TestECSRoundTripResources(t *testing.T) {
	a := &ecsAdapter{}
	specs, _, _ := a.Import([]byte(sampleECS))
	files, _, err := a.Export(specs)
	if err != nil {
		t.Fatal(err)
	}
	out := string(files["taskdef.json"])
	// 500 millicores → 512 units; 1GiB → 1024 MiB.
	if !strings.Contains(out, `"cpu": 512`) || !strings.Contains(out, `"memory": 1024`) {
		t.Errorf("resource round-trip drifted:\n%s", out)
	}
}

const sampleACA = `
type: Microsoft.App/containerApps
name: api
properties:
  configuration:
    ingress:
      external: true
      targetPort: 8080
    secrets:
    - name: db-url
      value: ""
  template:
    containers:
    - name: api
      image: registry.azurecr.io/api:latest
      env:
      - name: LOG_LEVEL
        value: info
      - name: DB_URL
        secretRef: db-url
      resources:
        cpu: 0.5
        memory: 1Gi
      probes:
      - type: Liveness
        httpGet:
          path: /health
          port: 8080
    scale:
      minReplicas: 1
      maxReplicas: 10
`

func TestContainerAppsImport(t *testing.T) {
	a := &containerAppsAdapter{}
	if !a.Detect("containerapp.yaml", []byte(sampleACA)) {
		t.Fatal("Detect failed")
	}
	specs, rep, err := a.Import([]byte(sampleACA))
	if err != nil {
		t.Fatal(err)
	}
	s := specs[0]
	if s.Name != "api" || s.Ports[0].Container != 8080 {
		t.Errorf("spec: %+v", s)
	}
	if s.Resources.MilliCPU != 500 || s.Resources.MemoryBytes != 1024*1024*1024 {
		t.Errorf("resources: %+v", s.Resources)
	}
	if s.Health == nil || s.Health.Kind != HealthHTTP || s.Health.Port != 8080 {
		t.Errorf("health: %+v", s.Health)
	}
	if !hasNote(rep, "scale") {
		t.Errorf("expected scale note: %+v", rep.Notes)
	}
}

func TestDetectRouting(t *testing.T) {
	cases := []struct {
		name, data, want string
	}{
		{"docker-compose.yml", sampleCompose, "compose"},
		{"service.yaml", sampleCloudRun, "cloudrun"},
		{"taskdef.json", sampleECS, "ecs"},
		{"containerapp.yaml", sampleACA, "containerapps"},
	}
	for _, c := range cases {
		a, err := Detect(c.name, []byte(c.data))
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if a.Format() != c.want {
			t.Errorf("%s: detected %s, want %s", c.name, a.Format(), c.want)
		}
	}
}

func hasNote(rep Report, code string) bool {
	for _, n := range rep.Notes {
		if n.Code == code {
			return true
		}
	}
	return false
}
