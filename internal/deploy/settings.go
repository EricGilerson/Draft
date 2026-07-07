package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/go-units"
)

type containerOverrides struct {
	Cmd        []string
	Entrypoint []string
	WorkingDir string
	User       string
	StopSignal string
	Labels     map[string]string

	RestartPolicy container.RestartPolicy
	Healthcheck   *container.HealthConfig
	Resources     container.Resources
	Mounts        []mount.Mount
	Privileged    bool
	Init          *bool
	ReadonlyRootfs bool
	CapAdd        []string
	CapDrop       []string
	StopTimeout   *int
}

type buildOverrides struct {
	Target   string
	Platform string
	NoCache  bool
}

type lifecycleHooks struct {
	PreBuild  string
	PostBuild string
	PreDeploy string
	PostDeploy string
}

func parseContainerOverrides(settings map[string]string) containerOverrides {
	var o containerOverrides

	if v := strings.TrimSpace(settings["cmd_override"]); v != "" {
		o.Cmd = shellSplit(v)
	}
	if v := strings.TrimSpace(settings["entrypoint_override"]); v != "" {
		o.Entrypoint = shellSplit(v)
	}
	o.WorkingDir = strings.TrimSpace(settings["working_dir"])
	o.User = strings.TrimSpace(settings["run_user"])
	o.StopSignal = strings.TrimSpace(settings["stop_signal"])

	if v := strings.TrimSpace(settings["custom_labels"]); v != "" {
		labels := map[string]string{}
		if json.Unmarshal([]byte(v), &labels) == nil {
			o.Labels = labels
		}
	}

	o.RestartPolicy = parseRestartPolicy(settings)
	o.Healthcheck = parseHealthcheck(settings)
	o.Resources = parseResources(settings)
	o.Mounts = parseVolumeMounts(settings)

	o.Privileged = settings["privileged"] == "true"
	o.ReadonlyRootfs = settings["readonly_rootfs"] == "true"

	if settings["init_process"] == "true" {
		t := true
		o.Init = &t
	}

	if v := strings.TrimSpace(settings["cap_add"]); v != "" {
		o.CapAdd = splitCSV(v)
	}
	if v := strings.TrimSpace(settings["cap_drop"]); v != "" {
		o.CapDrop = splitCSV(v)
	}

	if v := strings.TrimSpace(settings["stop_grace_period"]); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			o.StopTimeout = &secs
		}
	}

	return o
}

func parseBuildOverrides(settings map[string]string) buildOverrides {
	return buildOverrides{
		Target:   strings.TrimSpace(settings["build_target"]),
		Platform: strings.TrimSpace(settings["build_platform"]),
		NoCache:  settings["build_no_cache"] == "true",
	}
}

func parseLifecycleHooks(settings map[string]string) lifecycleHooks {
	return lifecycleHooks{
		PreBuild:   strings.TrimSpace(settings["pre_build_cmd"]),
		PostBuild:  strings.TrimSpace(settings["post_build_cmd"]),
		PreDeploy:  strings.TrimSpace(settings["pre_deploy_cmd"]),
		PostDeploy: strings.TrimSpace(settings["post_deploy_cmd"]),
	}
}

func parseRestartPolicy(settings map[string]string) container.RestartPolicy {
	policy := strings.TrimSpace(settings["restart_policy"])
	switch policy {
	case "always":
		return container.RestartPolicy{Name: container.RestartPolicyAlways}
	case "on-failure":
		rp := container.RestartPolicy{Name: container.RestartPolicyOnFailure}
		if v := strings.TrimSpace(settings["restart_max_retries"]); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				rp.MaximumRetryCount = n
			}
		}
		return rp
	case "unless-stopped":
		return container.RestartPolicy{Name: container.RestartPolicyUnlessStopped}
	default:
		return container.RestartPolicy{}
	}
}

func parseHealthcheck(settings map[string]string) *container.HealthConfig {
	if settings["healthcheck_disable"] == "true" {
		return &container.HealthConfig{Test: []string{"NONE"}}
	}

	cmd := strings.TrimSpace(settings["healthcheck_cmd"])
	if cmd == "" {
		return nil
	}

	hc := &container.HealthConfig{
		Test: []string{"CMD-SHELL", cmd},
	}

	if v := strings.TrimSpace(settings["healthcheck_interval"]); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			hc.Interval = d
		}
	}
	if v := strings.TrimSpace(settings["healthcheck_timeout"]); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			hc.Timeout = d
		}
	}
	if v := strings.TrimSpace(settings["healthcheck_start_period"]); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			hc.StartPeriod = d
		}
	}
	if v := strings.TrimSpace(settings["healthcheck_retries"]); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			hc.Retries = n
		}
	}

	return hc
}

func parseResources(settings map[string]string) container.Resources {
	var r container.Resources

	if v := strings.TrimSpace(settings["cpu_limit"]); v != "" {
		if cpus, err := strconv.ParseFloat(v, 64); err == nil && cpus > 0 {
			r.NanoCPUs = int64(cpus * 1e9)
		}
	}

	if v := strings.TrimSpace(settings["memory_limit"]); v != "" {
		if bytes, err := units.RAMInBytes(v); err == nil && bytes > 0 {
			r.Memory = bytes
		}
	}

	if v := strings.TrimSpace(settings["memory_reservation"]); v != "" {
		if bytes, err := units.RAMInBytes(v); err == nil && bytes > 0 {
			r.MemoryReservation = bytes
		}
	}

	if v := strings.TrimSpace(settings["pids_limit"]); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			r.PidsLimit = &n
		}
	}

	return r
}

// parseVolumeMounts reads node_settings.volume_mounts and returns Docker
// mounts. Auto-named volumes (type=="volume" with empty source) get an empty
// Source here; ensureNamedVolumes fills it in at deploy time once the node
// identity is known. Legacy rows without a "type" are treated as bind mounts,
// and bind mounts fall back to the pre-volume "hostPath" field for back-compat.
func parseVolumeMounts(settings map[string]string) []mount.Mount {
	return SpecsToMounts(ParseVolumeSpecs(settings["volume_mounts"]))
}

func runLifecycleHook(ctx context.Context, label, cmd, workDir string, logFn func(string)) error {
	if cmd == "" {
		return nil
	}
	logFn(fmt.Sprintf("==> Running %s hook...", label))
	logFn(fmt.Sprintf("    %s", cmd))

	var c *exec.Cmd
	if runtime.GOOS == "windows" {
		c = exec.CommandContext(ctx, "cmd", "/C", cmd)
	} else {
		c = exec.CommandContext(ctx, "sh", "-c", cmd)
	}
	c.Dir = workDir

	output, err := c.CombinedOutput()
	if len(output) > 0 {
		for _, line := range strings.Split(strings.TrimRight(string(output), "\n"), "\n") {
			logFn("    " + line)
		}
	}
	if err != nil {
		logFn(fmt.Sprintf("    Hook failed: %v", err))
		return fmt.Errorf("%s hook failed: %w", label, err)
	}
	logFn(fmt.Sprintf("    %s hook completed", label))
	return nil
}

func shellSplit(s string) []string {
	var parts []string
	var current strings.Builder
	inSingle := false
	inDouble := false
	escaped := false

	for _, r := range s {
		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' && !inSingle {
			escaped = true
			continue
		}
		if r == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if r == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
		if r == ' ' && !inSingle && !inDouble {
			if current.Len() > 0 {
				parts = append(parts, current.String())
				current.Reset()
			}
			continue
		}
		current.WriteRune(r)
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	return parts
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			result = append(result, t)
		}
	}
	return result
}

// Image retention policy for rollback support. Stored on node_settings under
// "keep_images". Local disk is finite, so unlike hosted PaaSes we can't keep
// every historical build image — see the rollback design in the plan.
const (
	keepImagesLast = "last" // default: keep only N-1 (the immediately previous image)
	keepImagesNone = "none" // current behavior: remove every prior image on cutover
	keepImagesAll  = "all"  // keep every image (power users with disk to spare)
)

// keepImagesPolicy coerces the setting to one of the recognized values,
// defaulting to keepImagesLast (the 95% rollback case: "the deploy I just did
// is broken, go back to what was running 5 minutes ago").
func keepImagesPolicy(settings map[string]string) string {
	switch strings.TrimSpace(strings.ToLower(settings["keep_images"])) {
	case keepImagesNone:
		return keepImagesNone
	case keepImagesAll:
		return keepImagesAll
	default:
		return keepImagesLast
	}
}

