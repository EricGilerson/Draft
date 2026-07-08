package cloudconfig

import (
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

func init() { Register(&containerAppsAdapter{}) }

// containerAppsAdapter maps Azure Container Apps. The run spec is a
// containerapp.yaml; the build spec is an ACR task (acb.yaml), which — like
// Cloud Build — is a step list wrapping a single docker build. Container Apps
// is the direct Cloud Run analog (managed, one primary container, HTTP ingress).
type containerAppsAdapter struct{}

func (containerAppsAdapter) Format() string { return "containerapps" }

func (containerAppsAdapter) Detect(name string, data []byte) bool {
	s := string(data)
	if strings.Contains(s, "Microsoft.App/containerApps") {
		return true
	}
	lname := strings.ToLower(name)
	if strings.Contains(lname, "containerapp") || strings.Contains(lname, "acb.yaml") {
		return true
	}
	// containerapp.yaml sniff: properties.template.containers.
	if strings.Contains(s, "template:") && strings.Contains(s, "containers:") && strings.Contains(s, "properties:") {
		return true
	}
	return false
}

func (a containerAppsAdapter) Import(data []byte) ([]ServiceSpec, Report, error) {
	if isACBTask(data) {
		return a.importACB(data)
	}
	return a.importContainerApp(data)
}

func (containerAppsAdapter) importContainerApp(data []byte) ([]ServiceSpec, Report, error) {
	var rep Report
	var app containerApp
	if err := yaml.Unmarshal(data, &app); err != nil {
		return nil, rep, fmt.Errorf("parse Container App: %w", err)
	}
	containers := app.Properties.Template.Containers
	if len(containers) == 0 {
		return nil, rep, fmt.Errorf("Container App has no containers")
	}
	c := containers[0]

	name := app.Name
	if name == "" {
		name = c.Name
	}
	spec := ServiceSpec{Name: orName(name, "service"), Image: c.Image, Target: "containerapps", Extra: map[string]any{}}
	spec.Entrypoint = c.Command
	spec.Command = c.Args

	// Secret name → value lookup, so a secretRef resolves to a local value when
	// the file inlines it (it usually does not).
	secretVals := map[string]string{}
	for _, s := range app.Properties.Configuration.Secrets {
		secretVals[s.Name] = s.Value
	}
	for _, e := range c.Env {
		ev := EnvVar{Key: e.Name, Value: e.Value, Scope: ScopeRuntime}
		if e.SecretRef != "" {
			ev.Secret = true
			ev.Value = secretVals[e.SecretRef] // usually "" (Key Vault ref)
			if ev.Value == "" {
				rep.Add(KindManual, "secret_ref", e.Name,
					fmt.Sprintf("%s is bound to secret %q; set a local value before deploying.", e.Name, e.SecretRef))
			}
		}
		spec.Env = append(spec.Env, ev)
	}

	if c.Resources.CPU > 0 {
		spec.Resources.MilliCPU = int(c.Resources.CPU * 1000)
	}
	if c.Resources.Memory != "" {
		spec.Resources.MemoryBytes = k8sMemToBytes(c.Resources.Memory)
	}
	for _, vm := range c.VolumeMounts {
		spec.Volumes = append(spec.Volumes, VolumeMount{Type: VolumeVolume, Source: vm.VolumeName, ContainerPath: vm.MountPath})
	}
	if probe := firstACAProbe(c.Probes); probe != nil {
		spec.Health = acaProbeToHealth(probe)
	}

	if ing := app.Properties.Configuration.Ingress; ing != nil && ing.TargetPort > 0 {
		spec.Ports = []PortSpec{{Container: ing.TargetPort, Protocol: "tcp"}}
	}
	if sc := app.Properties.Template.Scale; sc != nil {
		spec.Extra["scale"] = map[string]int{"minReplicas": sc.MinReplicas, "maxReplicas": sc.MaxReplicas}
		rep.Add(KindIgnored, "scale", "", fmt.Sprintf("Scale rules (min %d / max %d) are a Container Apps setting; Draft runs one instance.", sc.MinReplicas, sc.MaxReplicas))
	}

	return []ServiceSpec{spec}, rep, nil
}

func (containerAppsAdapter) importACB(data []byte) ([]ServiceSpec, Report, error) {
	var rep Report
	var task acbTask
	if err := yaml.Unmarshal(data, &task); err != nil {
		return nil, rep, fmt.Errorf("parse ACR task: %w", err)
	}
	for _, st := range task.Steps {
		if st.Build != "" {
			build, _, ok := parseDockerBuildArgs(strings.Fields(st.Build), &rep)
			if ok {
				spec := ServiceSpec{Name: "service", Build: build, Target: "containerapps", Extra: map[string]any{}}
				rep.Add(KindInfo, "build_mode", "", "Imported the build recipe from the ACR task; import a containerapp.yaml to add the run contract.")
				return []ServiceSpec{spec}, rep, nil
			}
		}
	}
	return nil, rep, fmt.Errorf("no build step found in ACR task")
}

func (a containerAppsAdapter) Export(specs []ServiceSpec) (map[string][]byte, Report, error) {
	var rep Report
	out := map[string][]byte{}
	for _, spec := range specs {
		image := spec.Image
		if spec.Build != nil {
			image = "REGISTRY.azurecr.io/" + spec.Name + ":latest"
			rep.Add(KindManual, "image_placeholder", "", "Build-mode service: the image is a placeholder ("+image+"). Build and push it with the emitted acb.yaml.")
			if b, err := yaml.Marshal(buildToACB(spec, image)); err == nil {
				out["acb.yaml"] = b
			}
		}

		app := containerApp{
			Type:     "Microsoft.App/containerApps",
			Name:     spec.Name,
			Location: "eastus",
		}
		cont := acaContainer{Name: spec.Name, Image: image, Command: spec.Entrypoint, Args: spec.Command}
		for _, e := range spec.Env {
			if e.Secret {
				secretName := strings.ToLower(strings.ReplaceAll(e.Key, "_", "-"))
				cont.Env = append(cont.Env, acaEnv{Name: e.Key, SecretRef: secretName})
				app.Properties.Configuration.Secrets = append(app.Properties.Configuration.Secrets, acaSecret{Name: secretName, Value: ""})
				continue
			}
			cont.Env = append(cont.Env, acaEnv{Name: e.Key, Value: e.Value})
		}
		if spec.Resources.MilliCPU > 0 {
			cont.Resources.CPU = float64(spec.Resources.MilliCPU) / 1000
		}
		if spec.Resources.MemoryBytes > 0 {
			cont.Resources.Memory = bytesToK8sMem(spec.Resources.MemoryBytes)
		}
		for _, v := range spec.Volumes {
			cont.VolumeMounts = append(cont.VolumeMounts, acaVolumeMount{VolumeName: volumeName(v), MountPath: v.ContainerPath})
		}
		if h := spec.Health; h != nil && !h.Disable {
			if p := healthToACAProbe(h, spec); p != nil {
				cont.Probes = append(cont.Probes, *p)
			} else {
				rep.Add(KindIgnored, "exec_probe", "", "A shell/exec healthcheck has no Container Apps equivalent (only HTTP/TCP probes); it was dropped.")
			}
		}
		app.Properties.Template.Containers = []acaContainer{cont}
		if len(spec.Ports) > 0 {
			app.Properties.Configuration.Ingress = &acaIngress{External: true, TargetPort: spec.Ports[0].Container}
		}

		data, err := yaml.Marshal(app)
		if err != nil {
			return nil, rep, err
		}
		name := "containerapp.yaml"
		if len(specs) > 1 {
			name = spec.Name + ".containerapp.yaml"
		}
		out[name] = data
	}
	return out, rep, nil
}

// --- Container Apps shapes --------------------------------------------------

type containerApp struct {
	Type       string        `yaml:"type,omitempty"`
	Name       string        `yaml:"name,omitempty"`
	Location   string        `yaml:"location,omitempty"`
	Properties acaProperties `yaml:"properties"`
}
type acaProperties struct {
	Configuration acaConfiguration `yaml:"configuration"`
	Template      acaTemplate      `yaml:"template"`
}
type acaConfiguration struct {
	Ingress *acaIngress `yaml:"ingress,omitempty"`
	Secrets []acaSecret `yaml:"secrets,omitempty"`
}
type acaIngress struct {
	External   bool `yaml:"external"`
	TargetPort int  `yaml:"targetPort"`
}
type acaSecret struct {
	Name  string `yaml:"name"`
	Value string `yaml:"value,omitempty"`
}
type acaTemplate struct {
	Containers []acaContainer `yaml:"containers"`
	Scale      *acaScale      `yaml:"scale,omitempty"`
}
type acaScale struct {
	MinReplicas int `yaml:"minReplicas,omitempty"`
	MaxReplicas int `yaml:"maxReplicas,omitempty"`
}
type acaContainer struct {
	Name         string           `yaml:"name"`
	Image        string           `yaml:"image"`
	Command      []string         `yaml:"command,omitempty"`
	Args         []string         `yaml:"args,omitempty"`
	Env          []acaEnv         `yaml:"env,omitempty"`
	Resources    acaResources     `yaml:"resources,omitempty"`
	VolumeMounts []acaVolumeMount `yaml:"volumeMounts,omitempty"`
	Probes       []acaProbe       `yaml:"probes,omitempty"`
}
type acaEnv struct {
	Name      string `yaml:"name"`
	Value     string `yaml:"value,omitempty"`
	SecretRef string `yaml:"secretRef,omitempty"`
}
type acaResources struct {
	CPU    float64 `yaml:"cpu,omitempty"`
	Memory string  `yaml:"memory,omitempty"`
}
type acaVolumeMount struct {
	VolumeName string `yaml:"volumeName"`
	MountPath  string `yaml:"mountPath"`
}
type acaProbe struct {
	Type             string        `yaml:"type,omitempty"` // Liveness|Readiness|Startup
	HTTPGet          *acaHTTPGet   `yaml:"httpGet,omitempty"`
	TCPSocket        *acaTCPSocket `yaml:"tcpSocket,omitempty"`
	PeriodSeconds    int           `yaml:"periodSeconds,omitempty"`
	FailureThreshold int           `yaml:"failureThreshold,omitempty"`
}
type acaHTTPGet struct {
	Path string `yaml:"path,omitempty"`
	Port int    `yaml:"port,omitempty"`
}
type acaTCPSocket struct {
	Port int `yaml:"port,omitempty"`
}

type acbTask struct {
	Steps []acbStep `yaml:"steps"`
}
type acbStep struct {
	Build string   `yaml:"build,omitempty"`
	Push  []string `yaml:"push,omitempty"`
}

// --- helpers ---------------------------------------------------------------

func isACBTask(data []byte) bool {
	s := string(data)
	if strings.Contains(s, "containers:") || strings.Contains(s, "Microsoft.App") {
		return false
	}
	var probe struct {
		Steps []struct {
			Build string `yaml:"build"`
		} `yaml:"steps"`
	}
	if yaml.Unmarshal(data, &probe) != nil {
		return false
	}
	for _, st := range probe.Steps {
		if st.Build != "" {
			return true
		}
	}
	return false
}

func firstACAProbe(probes []acaProbe) *acaProbe {
	// Prefer Liveness, then Readiness, then Startup, then first.
	for _, want := range []string{"Liveness", "Readiness", "Startup"} {
		for i := range probes {
			if probes[i].Type == want {
				return &probes[i]
			}
		}
	}
	if len(probes) > 0 {
		return &probes[0]
	}
	return nil
}

func acaProbeToHealth(p *acaProbe) *HealthSpec {
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
	if p.FailureThreshold > 0 {
		h.Retries = p.FailureThreshold
	}
	return h
}

func healthToACAProbe(h *HealthSpec, spec ServiceSpec) *acaProbe {
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
		return &acaProbe{Type: "Liveness", HTTPGet: &acaHTTPGet{Path: path, Port: port}}
	case HealthTCP:
		return &acaProbe{Type: "Liveness", TCPSocket: &acaTCPSocket{Port: port}}
	default:
		return nil
	}
}

func buildToACB(spec ServiceSpec, image string) acbTask {
	args := []string{"-t", image}
	if spec.Build.Dockerfile != "" {
		args = append(args, "-f", spec.Build.Dockerfile)
	}
	if spec.Build.Target != "" {
		args = append(args, "--target", spec.Build.Target)
	}
	for _, a := range spec.Build.Args {
		args = append(args, "--build-arg", a.Key+"="+a.Value)
	}
	ctx := spec.Build.Context
	if ctx == "" {
		ctx = "."
	}
	args = append(args, ctx)
	return acbTask{Steps: []acbStep{{Build: strings.Join(args, " ")}}}
}
