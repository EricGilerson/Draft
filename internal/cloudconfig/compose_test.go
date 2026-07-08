package cloudconfig

import (
	"strings"
	"testing"
)

const sampleCompose = `
services:
  web:
    build:
      context: ./web
      dockerfile: Dockerfile
      target: prod
      args:
        NODE_ENV: production
    command: ["node", "server.js"]
    ports:
      - "8080:3000"
    environment:
      LOG_LEVEL: info
      DATABASE_URL: postgres://db:5432/app
    deploy:
      resources:
        limits:
          cpus: "1"
          memory: 512M
        reservations:
          memory: 256M
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:3000/health"]
      interval: 10s
      retries: 3
    restart: unless-stopped
    volumes:
      - ./data:/app/data
      - cache:/var/cache:ro
  db:
    image: postgres:16
    ports:
      - "5432"
    environment:
      POSTGRES_PASSWORD: secret
    volumes:
      - pgdata:/var/lib/postgresql/data
volumes:
  cache: {}
  pgdata: {}
`

func TestComposeImport(t *testing.T) {
	a := &composeAdapter{}
	if !a.Detect("docker-compose.yml", []byte(sampleCompose)) {
		t.Fatal("Detect should recognize compose file")
	}
	specs, rep, err := a.Import([]byte(sampleCompose))
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 2 {
		t.Fatalf("want 2 services, got %d", len(specs))
	}
	byName := map[string]ServiceSpec{}
	for _, s := range specs {
		byName[s.Name] = s
	}

	web := byName["web"]
	if web.Build == nil || web.Build.Context != "./web" || web.Build.Target != "prod" {
		t.Errorf("web build not parsed: %+v", web.Build)
	}
	if len(web.Build.Args) != 1 || web.Build.Args[0].Key != "NODE_ENV" {
		t.Errorf("build args: %+v", web.Build.Args)
	}
	if len(web.Ports) != 1 || web.Ports[0].Container != 3000 || web.Ports[0].Published != 8080 {
		t.Errorf("web ports: %+v", web.Ports)
	}
	if web.Resources.MilliCPU != 1000 || web.Resources.MemoryBytes != 512*1024*1024 {
		t.Errorf("web resources: %+v", web.Resources)
	}
	if web.Resources.MemoryReservBytes != 256*1024*1024 {
		t.Errorf("web reservation: %d", web.Resources.MemoryReservBytes)
	}
	if web.Health == nil || web.Health.Kind != HealthCmd || web.Health.Retries != 3 {
		t.Errorf("web health: %+v", web.Health)
	}
	if web.Restart.Policy != "unless-stopped" {
		t.Errorf("web restart: %q", web.Restart.Policy)
	}
	if len(web.Volumes) != 2 {
		t.Fatalf("web volumes: %+v", web.Volumes)
	}
	if web.Volumes[0].Type != VolumeBind || web.Volumes[0].ContainerPath != "/app/data" {
		t.Errorf("web bind vol: %+v", web.Volumes[0])
	}
	if web.Volumes[1].Type != VolumeVolume || !web.Volumes[1].ReadOnly {
		t.Errorf("web named vol: %+v", web.Volumes[1])
	}

	// Cross-service rewrite: web's DATABASE_URL references db.
	var dbURL string
	for _, e := range web.Env {
		if e.Key == "DATABASE_URL" {
			dbURL = e.Value
		}
	}
	if !strings.Contains(dbURL, "@{db.DRAFT_INTERNAL_HOSTNAME}") {
		t.Errorf("DATABASE_URL not rewired: %q", dbURL)
	}
	foundRewire := false
	for _, n := range rep.Notes {
		if n.Code == "cross_service_ref" {
			foundRewire = true
		}
	}
	if !foundRewire {
		t.Error("expected a cross_service_ref note")
	}

	db := byName["db"]
	if db.Image != "postgres:16" || len(db.Ports) != 1 || db.Ports[0].Container != 5432 {
		t.Errorf("db: %+v", db)
	}
}

func TestBridgeRoundTrip(t *testing.T) {
	a := &composeAdapter{}
	specs, _, err := a.Import([]byte(sampleCompose))
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range specs {
		bundle, _ := BundleFromSpec(spec)
		// service_port present
		if bundle.Settings["service_port"] == "" {
			t.Errorf("%s: missing service_port", spec.Name)
		}
		back, _ := SpecFromBundle(bundle)
		if spec.IsImageMode() != back.IsImageMode() {
			t.Errorf("%s: mode changed on round-trip", spec.Name)
		}
		if len(spec.Ports) > 0 && (len(back.Ports) == 0 || back.Ports[0].Container != spec.Ports[0].Container) {
			t.Errorf("%s: port lost on round-trip", spec.Name)
		}
	}

	// web build-arg NODE_ENV must round-trip through settings as a build-scoped
	// env var, then back into Build.Args.
	web := specs[0]
	if web.Name != "web" {
		for _, s := range specs {
			if s.Name == "web" {
				web = s
			}
		}
	}
	bundle, _ := BundleFromSpec(web)
	var found bool
	for _, e := range bundle.Env {
		if e.Key == "NODE_ENV" && (e.Scope == ScopeBuild || e.Scope == ScopeBoth) {
			found = true
		}
	}
	if !found {
		t.Errorf("NODE_ENV should be build-scoped in bundle: %+v", bundle.Env)
	}
}

func TestComposeExportSecretPlaceholder(t *testing.T) {
	spec := ServiceSpec{
		Name:  "api",
		Image: "api:latest",
		Ports: []PortSpec{{Container: 8080, Protocol: "tcp"}},
		Env:   []EnvVar{{Key: "TOKEN", Secret: true, Scope: ScopeRuntime}},
	}
	a := &composeAdapter{}
	files, _, err := a.Export([]ServiceSpec{spec})
	if err != nil {
		t.Fatal(err)
	}
	out := string(files["docker-compose.yml"])
	if !strings.Contains(out, "${TOKEN}") {
		t.Errorf("secret should export as placeholder, got:\n%s", out)
	}
}
