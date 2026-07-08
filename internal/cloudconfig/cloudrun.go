package cloudconfig

import (
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

func init() { Register(&cloudRunAdapter{}) }

// cloudRunAdapter maps Google Cloud Run. It handles both halves of a GCP
// deploy: the run spec (a Knative Service YAML) and the build spec (a
// cloudbuild.yaml). A standalone service.yaml imports as image-mode (Cloud Run
// runs a prebuilt image); a cloudbuild.yaml imports the build recipe. Exporting
// a build-mode service emits both files.
type cloudRunAdapter struct{}

func (cloudRunAdapter) Format() string { return "cloudrun" }

func (cloudRunAdapter) Detect(name string, data []byte) bool {
	s := string(data)
	if strings.Contains(s, "serving.knative.dev") || strings.Contains(s, "cloud-builders") {
		return true
	}
	lname := strings.ToLower(name)
	if strings.Contains(lname, "cloudbuild") {
		return true
	}
	return false
}

func (a cloudRunAdapter) Import(data []byte) ([]ServiceSpec, Report, error) {
	if isCloudBuild(data) {
		return a.importCloudBuild(data)
	}
	return a.importService(data)
}

func (cloudRunAdapter) importService(data []byte) ([]ServiceSpec, Report, error) {
	var rep Report
	var svc knativeService
	if err := yaml.Unmarshal(data, &svc); err != nil {
		return nil, rep, fmt.Errorf("parse Cloud Run service: %w", err)
	}
	if len(svc.Spec.Template.Spec.Containers) == 0 {
		return nil, rep, fmt.Errorf("Cloud Run service has no containers")
	}
	c := svc.Spec.Template.Spec.Containers[0]

	spec := ServiceSpec{Name: svc.Metadata.Name, Image: c.Image, Target: "cloudrun", Extra: map[string]any{}}
	if spec.Name == "" {
		spec.Name = "service"
	}
	// Knative: command overrides ENTRYPOINT, args overrides CMD.
	spec.Entrypoint = c.Command
	spec.Command = c.Args
	spec.WorkingDir = c.WorkingDir

	for _, p := range c.Ports {
		spec.Ports = append(spec.Ports, PortSpec{Container: p.ContainerPort, Protocol: protoOrTCP(p.Protocol)})
	}
	for _, e := range c.Env {
		ev := EnvVar{Key: e.Name, Value: e.Value, Scope: ScopeRuntime}
		if e.ValueFrom != nil && e.ValueFrom.SecretKeyRef != nil {
			ev.Secret = true
			ev.Value = ""
			rep.Add(KindManual, "secret_ref", e.Name,
				fmt.Sprintf("%s is bound to Secret Manager secret %q; set a local value before deploying.", e.Name, e.ValueFrom.SecretKeyRef.Name))
		}
		spec.Env = append(spec.Env, ev)
	}
	if cpu := c.Resources.Limits["cpu"]; cpu != "" {
		spec.Resources.MilliCPU = k8sCPUToMilli(cpu)
	}
	if mem := c.Resources.Limits["memory"]; mem != "" {
		spec.Resources.MemoryBytes = k8sMemToBytes(mem)
	}
	for _, vm := range c.VolumeMounts {
		spec.Volumes = append(spec.Volumes, VolumeMount{Type: VolumeVolume, Source: vm.Name, ContainerPath: vm.MountPath, ReadOnly: vm.ReadOnly})
	}
	if probe := firstProbe(c.LivenessProbe, c.StartupProbe); probe != nil {
		spec.Health = probeToHealth(probe)
	}

	// Control-plane bits Draft does not run, preserved as notes.
	if cc := svc.Spec.Template.Spec.ContainerConcurrency; cc > 0 {
		spec.Extra["containerConcurrency"] = cc
		rep.Add(KindIgnored, "concurrency", "", fmt.Sprintf("containerConcurrency %d is a Cloud Run scaling setting; Draft runs one instance.", cc))
	}
	for k, v := range svc.Spec.Template.Metadata.Annotations {
		if strings.HasPrefix(k, "autoscaling.") {
			rep.Add(KindIgnored, "autoscaling", "", fmt.Sprintf("Autoscaling annotation %s=%s ignored locally.", k, v))
		}
	}

	rep.Add(KindInfo, "image_mode", "", "Imported as an image-mode service (Cloud Run runs a prebuilt image). Pair a cloudbuild.yaml to import the build recipe.")
	return []ServiceSpec{spec}, rep, nil
}

func (cloudRunAdapter) importCloudBuild(data []byte) ([]ServiceSpec, Report, error) {
	var rep Report
	var cb cloudBuild
	if err := yaml.Unmarshal(data, &cb); err != nil {
		return nil, rep, fmt.Errorf("parse cloudbuild: %w", err)
	}
	build, imageTag, ok := extractDockerBuild(cb.Steps, &rep)
	if !ok {
		return nil, rep, fmt.Errorf("no docker build step found in cloudbuild.yaml")
	}
	name := serviceNameFromImage(imageTag)
	spec := ServiceSpec{Name: name, Build: build, Target: "cloudrun", Extra: map[string]any{}}
	rep.Add(KindInfo, "build_mode", "", "Imported the build recipe; add a Cloud Run service.yaml to import the run contract, or configure the port after import.")
	return []ServiceSpec{spec}, rep, nil
}

func (a cloudRunAdapter) Export(specs []ServiceSpec) (map[string][]byte, Report, error) {
	var rep Report
	out := map[string][]byte{}
	for _, spec := range specs {
		svc := knativeService{
			APIVersion: "serving.knative.dev/v1",
			Kind:       "Service",
			Metadata:   knMeta{Name: spec.Name},
		}
		c := knContainer{}

		image := spec.Image
		if spec.Build != nil {
			image = "gcr.io/PROJECT_ID/" + spec.Name
			rep.Add(KindManual, "image_placeholder", "", "Build-mode service: the service image is a placeholder ("+image+"). Build and push it with the emitted cloudbuild.yaml.")
			// Emit the paired build file.
			if cb, err := yaml.Marshal(buildToCloudBuild(spec, image)); err == nil {
				out["cloudbuild.yaml"] = cb
			}
		}
		c.Image = image
		c.Command = spec.Entrypoint
		c.Args = spec.Command
		c.WorkingDir = spec.WorkingDir

		if len(spec.Ports) > 0 {
			c.Ports = []knPort{{ContainerPort: spec.Ports[0].Container}}
		}
		for _, e := range spec.Env {
			if e.Secret {
				secretName := strings.ToLower(strings.ReplaceAll(e.Key, "_", "-"))
				c.Env = append(c.Env, knEnv{Name: e.Key, ValueFrom: &knValueFrom{SecretKeyRef: &knSecretKeyRef{Name: secretName, Key: "latest"}}})
				rep.Add(KindManual, "secret_ref", e.Key,
					fmt.Sprintf("%s is exported as a Secret Manager reference (%s); create that secret in Secret Manager and grant the service's runtime identity access before deploying.", e.Key, secretName))
				continue
			}
			c.Env = append(c.Env, knEnv{Name: e.Key, Value: e.Value})
		}
		if spec.Resources.MilliCPU > 0 || spec.Resources.MemoryBytes > 0 {
			c.Resources.Limits = map[string]string{}
			if spec.Resources.MilliCPU > 0 {
				c.Resources.Limits["cpu"] = milliToK8sCPU(spec.Resources.MilliCPU)
			}
			if spec.Resources.MemoryBytes > 0 {
				c.Resources.Limits["memory"] = bytesToK8sMem(spec.Resources.MemoryBytes)
			}
		}
		for _, v := range spec.Volumes {
			c.VolumeMounts = append(c.VolumeMounts, knVolumeMount{Name: volumeName(v), MountPath: v.ContainerPath, ReadOnly: v.ReadOnly})
		}
		if h := spec.Health; h != nil && !h.Disable {
			if p := healthToProbe(h, spec); p != nil {
				c.StartupProbe = p
			} else {
				rep.Add(KindIgnored, "exec_probe", "", "A shell/exec healthcheck has no Cloud Run equivalent (only HTTP/TCP probes); it was dropped.")
			}
		}

		svc.Spec.Template.Spec.Containers = []knContainer{c}
		rep.Add(KindIgnored, "deploy_target", "",
			"Project and region are not encoded in service.yaml; set them via `gcloud run services replace service.yaml --project=PROJECT_ID --region=REGION` (or the equivalent Terraform/gcloud target).")
		rep.Add(KindIgnored, "service_account", "",
			"No service account is set, so Cloud Run will use the default compute service account. Set spec.template.spec.serviceAccountName if the service needs specific IAM permissions.")
		data, err := yaml.Marshal(svc)
		if err != nil {
			return nil, rep, err
		}
		name := "service.yaml"
		if len(specs) > 1 {
			name = spec.Name + ".service.yaml"
		}
		out[name] = data
	}
	return out, rep, nil
}

// --- knative + cloudbuild shapes -------------------------------------------

type knativeService struct {
	APIVersion string        `yaml:"apiVersion"`
	Kind       string        `yaml:"kind"`
	Metadata   knMeta        `yaml:"metadata"`
	Spec       knServiceSpec `yaml:"spec"`
}
type knMeta struct {
	Name        string            `yaml:"name,omitempty"`
	Annotations map[string]string `yaml:"annotations,omitempty"`
}
type knServiceSpec struct {
	Template knTemplate `yaml:"template"`
}
type knTemplate struct {
	Metadata knMeta    `yaml:"metadata,omitempty"`
	Spec     knPodSpec `yaml:"spec"`
}
type knPodSpec struct {
	ContainerConcurrency int           `yaml:"containerConcurrency,omitempty"`
	Containers           []knContainer `yaml:"containers"`
	Volumes              []knVolume    `yaml:"volumes,omitempty"`
}
type knContainer struct {
	Image         string          `yaml:"image"`
	Command       []string        `yaml:"command,omitempty"`
	Args          []string        `yaml:"args,omitempty"`
	WorkingDir    string          `yaml:"workingDir,omitempty"`
	Ports         []knPort        `yaml:"ports,omitempty"`
	Env           []knEnv         `yaml:"env,omitempty"`
	Resources     knResources     `yaml:"resources,omitempty"`
	VolumeMounts  []knVolumeMount `yaml:"volumeMounts,omitempty"`
	StartupProbe  *knProbe        `yaml:"startupProbe,omitempty"`
	LivenessProbe *knProbe        `yaml:"livenessProbe,omitempty"`
}
type knPort struct {
	Name          string `yaml:"name,omitempty"`
	ContainerPort int    `yaml:"containerPort"`
	Protocol      string `yaml:"protocol,omitempty"`
}
type knEnv struct {
	Name      string       `yaml:"name"`
	Value     string       `yaml:"value,omitempty"`
	ValueFrom *knValueFrom `yaml:"valueFrom,omitempty"`
}
type knValueFrom struct {
	SecretKeyRef *knSecretKeyRef `yaml:"secretKeyRef,omitempty"`
}
type knSecretKeyRef struct {
	Name string `yaml:"name"`
	Key  string `yaml:"key,omitempty"`
}
type knResources struct {
	Limits   map[string]string `yaml:"limits,omitempty"`
	Requests map[string]string `yaml:"requests,omitempty"`
}
type knVolumeMount struct {
	Name      string `yaml:"name"`
	MountPath string `yaml:"mountPath"`
	ReadOnly  bool   `yaml:"readOnly,omitempty"`
}
type knVolume struct {
	Name string `yaml:"name"`
}
type knProbe struct {
	HTTPGet          *knHTTPGet   `yaml:"httpGet,omitempty"`
	TCPSocket        *knTCPSocket `yaml:"tcpSocket,omitempty"`
	PeriodSeconds    int          `yaml:"periodSeconds,omitempty"`
	TimeoutSeconds   int          `yaml:"timeoutSeconds,omitempty"`
	FailureThreshold int          `yaml:"failureThreshold,omitempty"`
}
type knHTTPGet struct {
	Path string `yaml:"path,omitempty"`
	Port int    `yaml:"port,omitempty"`
}
type knTCPSocket struct {
	Port int `yaml:"port,omitempty"`
}

type cloudBuild struct {
	Steps  []cloudBuildStep `yaml:"steps"`
	Images []string         `yaml:"images,omitempty"`
}
type cloudBuildStep struct {
	Name string   `yaml:"name"`
	Args []string `yaml:"args,omitempty"`
}

// --- helpers ---------------------------------------------------------------

func isCloudBuild(data []byte) bool {
	s := string(data)
	if strings.Contains(s, "cloud-builders") {
		return true
	}
	var probe struct {
		Steps []any `yaml:"steps"`
	}
	return yaml.Unmarshal(data, &probe) == nil && len(probe.Steps) > 0 && !strings.Contains(s, "kind:")
}

// extractDockerBuild finds the single `docker build` step and turns it into a
// BuildSpec. Returns the image tag it built (for naming) and ok=false when no
// build step is present.
func extractDockerBuild(steps []cloudBuildStep, rep *Report) (*BuildSpec, string, bool) {
	for _, st := range steps {
		if !strings.Contains(strings.ToLower(st.Name), "docker") {
			continue
		}
		if len(st.Args) == 0 || st.Args[0] != "build" {
			continue
		}
		return parseDockerBuildArgs(st.Args[1:], rep)
	}
	return nil, "", false
}

// parseDockerBuildArgs parses `docker build` flags into a BuildSpec.
func parseDockerBuildArgs(args []string, rep *Report) (*BuildSpec, string, bool) {
	b := &BuildSpec{}
	var imageTag string
	context := "."
	for i := 0; i < len(args); i++ {
		a := args[i]
		next := func() string {
			if i+1 < len(args) {
				i++
				return args[i]
			}
			return ""
		}
		switch {
		case a == "-t" || a == "--tag":
			imageTag = next()
		case a == "-f" || a == "--file":
			b.Dockerfile = next()
		case a == "--target":
			b.Target = next()
		case a == "--platform":
			b.Platform = next()
		case a == "--build-arg":
			kv := next()
			k, v, _ := strings.Cut(kv, "=")
			b.Args = append(b.Args, EnvVar{Key: k, Value: v, Scope: ScopeBuild})
		case strings.HasPrefix(a, "-"):
			// Unknown flag; skip its value heuristically if it looks paired.
		default:
			context = a // the context path is the bare positional arg
		}
	}
	b.Context = context
	return b, imageTag, true
}

func buildToCloudBuild(spec ServiceSpec, image string) cloudBuild {
	args := []string{"build", "-t", image}
	if spec.Build.Dockerfile != "" {
		args = append(args, "-f", spec.Build.Dockerfile)
	}
	if spec.Build.Target != "" {
		args = append(args, "--target", spec.Build.Target)
	}
	if spec.Build.Platform != "" {
		args = append(args, "--platform", spec.Build.Platform)
	}
	for _, a := range spec.Build.Args {
		args = append(args, "--build-arg", a.Key+"="+a.Value)
	}
	ctx := spec.Build.Context
	if ctx == "" {
		ctx = "."
	}
	args = append(args, ctx)
	return cloudBuild{
		Steps:  []cloudBuildStep{{Name: "gcr.io/cloud-builders/docker", Args: args}},
		Images: []string{image},
	}
}

func serviceNameFromImage(tag string) string {
	if tag == "" {
		return "service"
	}
	// gcr.io/proj/api:sha → api
	tag = strings.SplitN(tag, ":", 2)[0]
	parts := strings.Split(tag, "/")
	name := parts[len(parts)-1]
	if name == "" {
		return "service"
	}
	return name
}

func firstProbe(probes ...*knProbe) *knProbe {
	for _, p := range probes {
		if p != nil {
			return p
		}
	}
	return nil
}

func probeToHealth(p *knProbe) *HealthSpec {
	h := &HealthSpec{}
	switch {
	case p.HTTPGet != nil:
		h.Kind = HealthHTTP
		h.Path = p.HTTPGet.Path
		h.Port = p.HTTPGet.Port
	case p.TCPSocket != nil:
		h.Kind = HealthTCP
		h.Port = p.TCPSocket.Port
	default:
		return nil
	}
	if p.PeriodSeconds > 0 {
		h.Interval = strconv.Itoa(p.PeriodSeconds) + "s"
	}
	if p.TimeoutSeconds > 0 {
		h.Timeout = strconv.Itoa(p.TimeoutSeconds) + "s"
	}
	if p.FailureThreshold > 0 {
		h.Retries = p.FailureThreshold
	}
	return h
}

// healthToProbe renders an HTTP/TCP health as a Knative probe. A cmd-kind
// health (with no port) returns nil — Cloud Run has no exec probe.
func healthToProbe(h *HealthSpec, spec ServiceSpec) *knProbe {
	port := h.Port
	if port == 0 && len(spec.Ports) > 0 {
		port = spec.Ports[0].Container
	}
	switch h.Kind {
	case HealthHTTP:
		path := h.Path
		if path == "" {
			path = "/"
		}
		return &knProbe{HTTPGet: &knHTTPGet{Path: path, Port: port}}
	case HealthTCP:
		return &knProbe{TCPSocket: &knTCPSocket{Port: port}}
	default:
		return nil
	}
}

func protoOrTCP(p string) string {
	if p == "" {
		return "tcp"
	}
	return strings.ToLower(p)
}

func volumeName(v VolumeMount) string {
	if v.Source != "" {
		return v.Source
	}
	// Derive a name from the mount path.
	n := strings.Trim(strings.ReplaceAll(v.ContainerPath, "/", "-"), "-")
	if n == "" {
		n = "data"
	}
	return n
}
