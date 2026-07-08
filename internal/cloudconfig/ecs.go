package cloudconfig

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

func init() { Register(&ecsAdapter{}) }

// ecsAdapter maps AWS ECS. The run spec is an ECS task definition (JSON); the
// build spec is a CodeBuild buildspec.yml. ECS expresses CPU in units where
// 1024 = 1 vCPU and memory in MiB.
type ecsAdapter struct{}

func (ecsAdapter) Format() string { return "ecs" }

func (ecsAdapter) Detect(name string, data []byte) bool {
	lname := strings.ToLower(name)
	if strings.Contains(lname, "buildspec") {
		return true
	}
	s := string(data)
	if strings.Contains(s, "containerDefinitions") {
		return true
	}
	// buildspec.yml content sniff: a top-level phases mapping with a version.
	if strings.Contains(s, "phases:") && strings.Contains(s, "version:") && !strings.Contains(s, "containers:") {
		return true
	}
	return false
}

func (a ecsAdapter) Import(data []byte) ([]ServiceSpec, Report, error) {
	if isBuildspec(data) {
		return a.importBuildspec(data)
	}
	return a.importTaskDef(data)
}

func (ecsAdapter) importTaskDef(data []byte) ([]ServiceSpec, Report, error) {
	var rep Report
	var td ecsTaskDef
	if err := json.Unmarshal(data, &td); err != nil {
		return nil, rep, fmt.Errorf("parse ECS task definition: %w", err)
	}
	if len(td.ContainerDefinitions) == 0 {
		return nil, rep, fmt.Errorf("task definition has no containerDefinitions")
	}
	c := td.ContainerDefinitions[0]
	if len(td.ContainerDefinitions) > 1 {
		rep.Add(KindIgnored, "multi_container", "", "Only the first containerDefinition was imported; Draft models one container per service.")
	}

	spec := ServiceSpec{Name: orName(c.Name, td.Family), Image: c.Image, Target: "ecs", Extra: map[string]any{}}
	spec.Command = c.Command
	spec.Entrypoint = c.EntryPoint
	spec.User = c.User
	spec.WorkingDir = c.WorkingDirectory

	for _, e := range c.Environment {
		spec.Env = append(spec.Env, EnvVar{Key: e.Name, Value: e.Value, Scope: ScopeRuntime})
	}
	for _, s := range c.Secrets {
		spec.Env = append(spec.Env, EnvVar{Key: s.Name, Secret: true, Scope: ScopeRuntime})
		rep.Add(KindManual, "secret_ref", s.Name,
			fmt.Sprintf("%s is bound to an SSM/Secrets Manager value; set a local value before deploying.", s.Name))
	}
	for _, p := range c.PortMappings {
		spec.Ports = append(spec.Ports, PortSpec{Container: p.ContainerPort, Protocol: protoOrTCP(p.Protocol)})
	}

	// Resources: prefer container-level, fall back to task-level.
	if c.CPU > 0 {
		spec.Resources.MilliCPU = ecsUnitsToMilli(c.CPU)
	} else if td.CPU != "" {
		if n, err := strconv.Atoi(td.CPU); err == nil {
			spec.Resources.MilliCPU = ecsUnitsToMilli(n)
		}
	}
	if c.Memory > 0 {
		spec.Resources.MemoryBytes = int64(c.Memory) << 20
	} else if td.Memory != "" {
		if n, err := strconv.Atoi(td.Memory); err == nil {
			spec.Resources.MemoryBytes = int64(n) << 20
		}
	}
	if c.MemoryReservation > 0 {
		spec.Resources.MemoryReservBytes = int64(c.MemoryReservation) << 20
	}

	for _, m := range c.MountPoints {
		spec.Volumes = append(spec.Volumes, VolumeMount{Type: VolumeVolume, Source: m.SourceVolume, ContainerPath: m.ContainerPath, ReadOnly: m.ReadOnly})
	}
	if c.HealthCheck != nil && len(c.HealthCheck.Command) > 0 {
		spec.Health = ecsHealthToSpec(c.HealthCheck)
	}

	spec.Security = SecuritySpec{
		Privileged:     c.Privileged,
		ReadonlyRootfs: c.ReadonlyRootFilesystem,
		Labels:         c.DockerLabels,
	}
	if lp := c.LinuxParameters; lp != nil {
		spec.Security.Init = lp.InitProcessEnabled
		if lp.Capabilities != nil {
			spec.Security.CapAdd = lp.Capabilities.Add
			spec.Security.CapDrop = lp.Capabilities.Drop
		}
	}

	return []ServiceSpec{spec}, rep, nil
}

func (ecsAdapter) importBuildspec(data []byte) ([]ServiceSpec, Report, error) {
	var rep Report
	var bs buildspec
	if err := yaml.Unmarshal(data, &bs); err != nil {
		return nil, rep, fmt.Errorf("parse buildspec: %w", err)
	}
	cmds := append(append([]string{}, bs.Phases.PreBuild.Commands...), bs.Phases.Build.Commands...)
	cmds = append(cmds, bs.Phases.PostBuild.Commands...)
	build, _, ok := extractDockerBuildFromCommands(cmds, &rep)
	if !ok {
		return nil, rep, fmt.Errorf("no docker build command found in buildspec")
	}
	spec := ServiceSpec{Name: "service", Build: build, Target: "ecs", Extra: map[string]any{}}
	rep.Add(KindInfo, "build_mode", "", "Imported the build recipe from buildspec; import the task definition to add the run contract.")
	return []ServiceSpec{spec}, rep, nil
}

func (a ecsAdapter) Export(specs []ServiceSpec) (map[string][]byte, Report, error) {
	var rep Report
	out := map[string][]byte{}
	for _, spec := range specs {
		image := spec.Image
		if spec.Build != nil {
			image = "ACCOUNT.dkr.ecr.REGION.amazonaws.com/" + spec.Name + ":latest"
			rep.Add(KindManual, "image_placeholder", "", "Build-mode service: the image is a placeholder ("+image+"). Build and push it with the emitted buildspec.yml.")
			if b, err := yaml.Marshal(buildToBuildspec(spec, image)); err == nil {
				out["buildspec.yml"] = b
			}
		}
		c := ecsContainer{Name: spec.Name, Image: image, Essential: true, Command: spec.Command, EntryPoint: spec.Entrypoint, User: spec.User, WorkingDirectory: spec.WorkingDir}
		for _, e := range spec.Env {
			if e.Secret {
				c.Secrets = append(c.Secrets, ecsSecret{Name: e.Key, ValueFrom: "arn:aws:ssm:REGION:ACCOUNT:parameter/" + e.Key})
				continue
			}
			c.Environment = append(c.Environment, ecsKV{Name: e.Key, Value: e.Value})
		}
		for _, p := range spec.Ports {
			c.PortMappings = append(c.PortMappings, ecsPortMapping{ContainerPort: p.Container, Protocol: protoOrTCP(p.Protocol)})
		}
		if spec.Resources.MilliCPU > 0 {
			c.CPU = milliToECSUnits(spec.Resources.MilliCPU)
		}
		if spec.Resources.MemoryBytes > 0 {
			c.Memory = int(spec.Resources.MemoryBytes >> 20)
		}
		if spec.Resources.MemoryReservBytes > 0 {
			c.MemoryReservation = int(spec.Resources.MemoryReservBytes >> 20)
		}
		for _, v := range spec.Volumes {
			c.MountPoints = append(c.MountPoints, ecsMountPoint{SourceVolume: volumeName(v), ContainerPath: v.ContainerPath, ReadOnly: v.ReadOnly})
		}
		if h := spec.Health; h != nil && !h.Disable {
			c.HealthCheck = specHealthToECS(h)
		}
		c.Privileged = spec.Security.Privileged
		c.ReadonlyRootFilesystem = spec.Security.ReadonlyRootfs
		c.DockerLabels = spec.Security.Labels
		if spec.Security.Init || len(spec.Security.CapAdd) > 0 || len(spec.Security.CapDrop) > 0 {
			c.LinuxParameters = &ecsLinuxParams{InitProcessEnabled: spec.Security.Init}
			if len(spec.Security.CapAdd) > 0 || len(spec.Security.CapDrop) > 0 {
				c.LinuxParameters.Capabilities = &ecsCapabilities{Add: spec.Security.CapAdd, Drop: spec.Security.CapDrop}
			}
		}

		td := ecsTaskDef{Family: spec.Name, ContainerDefinitions: []ecsContainer{c}}
		var volSet []ecsVolume
		for _, v := range spec.Volumes {
			volSet = append(volSet, ecsVolume{Name: volumeName(v)})
		}
		td.Volumes = volSet

		data, err := json.MarshalIndent(td, "", "  ")
		if err != nil {
			return nil, rep, err
		}
		name := "taskdef.json"
		if len(specs) > 1 {
			name = spec.Name + ".taskdef.json"
		}
		out[name] = data
	}
	return out, rep, nil
}

// --- ECS shapes ------------------------------------------------------------

type ecsTaskDef struct {
	Family               string         `json:"family"`
	CPU                  string         `json:"cpu,omitempty"`
	Memory               string         `json:"memory,omitempty"`
	ContainerDefinitions []ecsContainer `json:"containerDefinitions"`
	Volumes              []ecsVolume    `json:"volumes,omitempty"`
}
type ecsContainer struct {
	Name                   string            `json:"name"`
	Image                  string            `json:"image"`
	Essential              bool              `json:"essential"`
	Command                []string          `json:"command,omitempty"`
	EntryPoint             []string          `json:"entryPoint,omitempty"`
	Environment            []ecsKV           `json:"environment,omitempty"`
	Secrets                []ecsSecret       `json:"secrets,omitempty"`
	PortMappings           []ecsPortMapping  `json:"portMappings,omitempty"`
	CPU                    int               `json:"cpu,omitempty"`
	Memory                 int               `json:"memory,omitempty"`
	MemoryReservation      int               `json:"memoryReservation,omitempty"`
	MountPoints            []ecsMountPoint   `json:"mountPoints,omitempty"`
	HealthCheck            *ecsHealthCheck   `json:"healthCheck,omitempty"`
	LinuxParameters        *ecsLinuxParams   `json:"linuxParameters,omitempty"`
	Privileged             bool              `json:"privileged,omitempty"`
	ReadonlyRootFilesystem bool              `json:"readonlyRootFilesystem,omitempty"`
	User                   string            `json:"user,omitempty"`
	WorkingDirectory       string            `json:"workingDirectory,omitempty"`
	DockerLabels           map[string]string `json:"dockerLabels,omitempty"`
}
type ecsKV struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}
type ecsSecret struct {
	Name      string `json:"name"`
	ValueFrom string `json:"valueFrom"`
}
type ecsPortMapping struct {
	ContainerPort int    `json:"containerPort"`
	Protocol      string `json:"protocol,omitempty"`
}
type ecsMountPoint struct {
	SourceVolume  string `json:"sourceVolume"`
	ContainerPath string `json:"containerPath"`
	ReadOnly      bool   `json:"readOnly,omitempty"`
}
type ecsHealthCheck struct {
	Command     []string `json:"command"`
	Interval    int      `json:"interval,omitempty"`
	Timeout     int      `json:"timeout,omitempty"`
	Retries     int      `json:"retries,omitempty"`
	StartPeriod int      `json:"startPeriod,omitempty"`
}
type ecsLinuxParams struct {
	InitProcessEnabled bool             `json:"initProcessEnabled,omitempty"`
	Capabilities       *ecsCapabilities `json:"capabilities,omitempty"`
}
type ecsCapabilities struct {
	Add  []string `json:"add,omitempty"`
	Drop []string `json:"drop,omitempty"`
}
type ecsVolume struct {
	Name string `json:"name"`
}

type buildspec struct {
	Version any             `yaml:"version"`
	Phases  buildspecPhases `yaml:"phases"`
}
type buildspecPhases struct {
	PreBuild  buildspecPhase `yaml:"pre_build"`
	Build     buildspecPhase `yaml:"build"`
	PostBuild buildspecPhase `yaml:"post_build"`
}
type buildspecPhase struct {
	Commands []string `yaml:"commands,omitempty"`
}

// --- helpers ---------------------------------------------------------------

func isBuildspec(data []byte) bool {
	s := string(data)
	if strings.Contains(s, "containerDefinitions") {
		return false
	}
	var probe struct {
		Phases map[string]any `yaml:"phases"`
	}
	return yaml.Unmarshal(data, &probe) == nil && len(probe.Phases) > 0
}

// extractDockerBuildFromCommands scans shell command lines for a `docker build`
// invocation and parses its flags.
func extractDockerBuildFromCommands(cmds []string, rep *Report) (*BuildSpec, string, bool) {
	for _, line := range cmds {
		fields := strings.Fields(line)
		for i := 0; i+1 < len(fields); i++ {
			if fields[i] == "docker" && fields[i+1] == "build" {
				return parseDockerBuildArgs(fields[i+2:], rep)
			}
		}
	}
	return nil, "", false
}

func buildToBuildspec(spec ServiceSpec, image string) buildspec {
	args := []string{"docker", "build", "-t", image}
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
	return buildspec{
		Version: 0.2,
		Phases: buildspecPhases{
			Build: buildspecPhase{Commands: []string{strings.Join(args, " ")}},
		},
	}
}

func ecsHealthToSpec(hc *ecsHealthCheck) *HealthSpec {
	cmd := hc.Command
	if len(cmd) > 0 && (cmd[0] == "CMD-SHELL" || cmd[0] == "CMD") {
		cmd = cmd[1:]
	}
	h := &HealthSpec{Kind: HealthCmd, Command: cmd}
	if hc.Interval > 0 {
		h.Interval = strconv.Itoa(hc.Interval) + "s"
	}
	if hc.Timeout > 0 {
		h.Timeout = strconv.Itoa(hc.Timeout) + "s"
	}
	if hc.StartPeriod > 0 {
		h.StartPeriod = strconv.Itoa(hc.StartPeriod) + "s"
	}
	h.Retries = hc.Retries
	return h
}

func specHealthToECS(h *HealthSpec) *ecsHealthCheck {
	cmd := h.Command
	if h.Kind == HealthHTTP {
		cmd = []string{"CMD-SHELL", fmt.Sprintf("curl -f http://127.0.0.1:%d%s || exit 1", h.Port, h.Path)}
	} else if len(cmd) > 0 {
		cmd = append([]string{"CMD-SHELL", strings.Join(cmd, " ")})
	}
	hc := &ecsHealthCheck{Command: cmd, Retries: h.Retries}
	hc.Interval = secondsFromDuration(h.Interval)
	hc.Timeout = secondsFromDuration(h.Timeout)
	hc.StartPeriod = secondsFromDuration(h.StartPeriod)
	return hc
}

func ecsUnitsToMilli(units int) int { return units * 1000 / 1024 }
func milliToECSUnits(milli int) int { return milli * 1024 / 1000 }

func orName(name, family string) string {
	if strings.TrimSpace(name) != "" {
		return name
	}
	if strings.TrimSpace(family) != "" {
		return family
	}
	return "service"
}

func secondsFromDuration(s string) int {
	if s == "" {
		return 0
	}
	n, err := parseDurationSeconds(s)
	if err != nil {
		return 0
	}
	return n
}
