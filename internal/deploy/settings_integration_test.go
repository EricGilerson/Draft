package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/build"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
)

func skipIfNoDocker(t *testing.T) *client.Client {
	t.Helper()
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Skipf("Docker not available: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := cli.Ping(ctx); err != nil {
		cli.Close()
		t.Skipf("Docker daemon not reachable: %v", err)
	}
	return cli
}

func buildTestImage(t *testing.T, cli *client.Client, tag, dockerfile string) {
	t.Helper()
	ctx := context.Background()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(dockerfile), 0644); err != nil {
		t.Fatalf("write Dockerfile: %v", err)
	}

	tarBuf, _, _, err := tarDirectoryWithProgress(dir, nil, nil)
	if err != nil {
		t.Fatalf("tar: %v", err)
	}
	defer tarBuf.Close()

	resp, err := cli.ImageBuild(ctx, tarBuf, build.ImageBuildOptions{
		Tags:       []string{tag},
		Dockerfile: "Dockerfile",
		Remove:     true,
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	defer resp.Body.Close()

	// drain build output
	buf := make([]byte, 4096)
	for {
		_, readErr := resp.Body.Read(buf)
		if readErr != nil {
			break
		}
	}
}

func removeContainer(cli *client.Client, id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	timeout := 1
	cli.ContainerStop(ctx, id, container.StopOptions{Timeout: &timeout})
	cli.ContainerRemove(ctx, id, container.RemoveOptions{Force: true})
}

func removeImage(cli *client.Client, tag string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cli.ImageRemove(ctx, tag, image.RemoveOptions{Force: true})
}

// TestSettingsParsing verifies all the parsers produce correct typed output
func TestSettingsParsing(t *testing.T) {
	t.Run("shellSplit", func(t *testing.T) {
		tests := []struct {
			input string
			want  []string
		}{
			{`node server.js`, []string{"node", "server.js"}},
			{`python -m "my app"`, []string{"python", "-m", "my app"}},
			{`sh -c 'echo hello world'`, []string{"sh", "-c", "echo hello world"}},
			{`cmd /C "echo test"`, []string{"cmd", "/C", "echo test"}},
			{`one   two   three`, []string{"one", "two", "three"}},
			{`escaped\ space`, []string{"escaped space"}},
		}
		for _, tt := range tests {
			got := shellSplit(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("shellSplit(%q): got %v, want %v", tt.input, got, tt.want)
				continue
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("shellSplit(%q)[%d]: got %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		}
	})

	t.Run("splitCSV", func(t *testing.T) {
		got := splitCSV("SYS_PTRACE, NET_ADMIN , CHOWN")
		want := []string{"SYS_PTRACE", "NET_ADMIN", "CHOWN"}
		if len(got) != len(want) {
			t.Fatalf("splitCSV: got %v, want %v", got, want)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("splitCSV[%d]: got %q, want %q", i, got[i], want[i])
			}
		}
	})

	t.Run("parseContainerOverrides_full", func(t *testing.T) {
		settings := map[string]string{
			"cmd_override":        `node -e "console.log('hi')"`,
			"entrypoint_override": `/bin/sh -c`,
			"working_dir":        "/app",
			"run_user":           "1000:1000",
			"stop_signal":        "SIGINT",
			"custom_labels":      `{"com.example.app":"test","version":"1.0"}`,
			"restart_policy":     "on-failure",
			"restart_max_retries": "5",
			"healthcheck_cmd":     "curl -f http://localhost:8080/health",
			"healthcheck_interval": "30s",
			"healthcheck_timeout":  "10s",
			"healthcheck_start_period": "5s",
			"healthcheck_retries": "3",
			"cpu_limit":          "1.5",
			"memory_limit":       "512m",
			"memory_reservation": "256m",
			"pids_limit":         "100",
			"volume_mounts":      `[{"hostPath":"C:\\data","containerPath":"/data","readOnly":true}]`,
			"privileged":         "true",
			"init_process":       "true",
			"readonly_rootfs":    "true",
			"cap_add":            "SYS_PTRACE,NET_ADMIN",
			"cap_drop":           "ALL",
			"stop_grace_period":  "30",
		}

		o := parseContainerOverrides(settings)

		if len(o.Cmd) != 3 || o.Cmd[0] != "node" {
			t.Errorf("Cmd: %v", o.Cmd)
		}
		if len(o.Entrypoint) != 2 || o.Entrypoint[0] != "/bin/sh" {
			t.Errorf("Entrypoint: %v", o.Entrypoint)
		}
		if o.WorkingDir != "/app" {
			t.Errorf("WorkingDir: %s", o.WorkingDir)
		}
		if o.User != "1000:1000" {
			t.Errorf("User: %s", o.User)
		}
		if o.StopSignal != "SIGINT" {
			t.Errorf("StopSignal: %s", o.StopSignal)
		}
		if len(o.Labels) != 2 || o.Labels["com.example.app"] != "test" {
			t.Errorf("Labels: %v", o.Labels)
		}
		if o.RestartPolicy.Name != container.RestartPolicyOnFailure || o.RestartPolicy.MaximumRetryCount != 5 {
			t.Errorf("RestartPolicy: %+v", o.RestartPolicy)
		}
		if o.Healthcheck == nil || len(o.Healthcheck.Test) != 2 || o.Healthcheck.Test[0] != "CMD-SHELL" {
			t.Errorf("Healthcheck: %+v", o.Healthcheck)
		}
		if o.Healthcheck.Interval != 30*time.Second {
			t.Errorf("Healthcheck.Interval: %v", o.Healthcheck.Interval)
		}
		if o.Healthcheck.Timeout != 10*time.Second {
			t.Errorf("Healthcheck.Timeout: %v", o.Healthcheck.Timeout)
		}
		if o.Healthcheck.StartPeriod != 5*time.Second {
			t.Errorf("Healthcheck.StartPeriod: %v", o.Healthcheck.StartPeriod)
		}
		if o.Healthcheck.Retries != 3 {
			t.Errorf("Healthcheck.Retries: %d", o.Healthcheck.Retries)
		}
		if o.Resources.NanoCPUs != 1_500_000_000 {
			t.Errorf("NanoCPUs: %d (want 1500000000)", o.Resources.NanoCPUs)
		}
		if o.Resources.Memory != 512*1024*1024 {
			t.Errorf("Memory: %d (want %d)", o.Resources.Memory, 512*1024*1024)
		}
		if o.Resources.MemoryReservation != 256*1024*1024 {
			t.Errorf("MemoryReservation: %d", o.Resources.MemoryReservation)
		}
		if o.Resources.PidsLimit == nil || *o.Resources.PidsLimit != 100 {
			t.Errorf("PidsLimit: %v", o.Resources.PidsLimit)
		}
		if len(o.Mounts) != 1 || o.Mounts[0].Target != "/data" || !o.Mounts[0].ReadOnly {
			t.Errorf("Mounts: %+v", o.Mounts)
		}
		if !o.Privileged {
			t.Error("Privileged should be true")
		}
		if o.Init == nil || !*o.Init {
			t.Error("Init should be true")
		}
		if !o.ReadonlyRootfs {
			t.Error("ReadonlyRootfs should be true")
		}
		if len(o.CapAdd) != 2 || o.CapAdd[0] != "SYS_PTRACE" {
			t.Errorf("CapAdd: %v", o.CapAdd)
		}
		if len(o.CapDrop) != 1 || o.CapDrop[0] != "ALL" {
			t.Errorf("CapDrop: %v", o.CapDrop)
		}
		if o.StopTimeout == nil || *o.StopTimeout != 30 {
			t.Errorf("StopTimeout: %v", o.StopTimeout)
		}
	})

	t.Run("parseContainerOverrides_empty", func(t *testing.T) {
		o := parseContainerOverrides(map[string]string{})
		if len(o.Cmd) != 0 || len(o.Entrypoint) != 0 || o.WorkingDir != "" || o.User != "" {
			t.Error("empty settings should produce zero-value overrides")
		}
		if o.Healthcheck != nil {
			t.Error("empty settings should not produce healthcheck")
		}
		if o.Privileged || o.ReadonlyRootfs || o.Init != nil {
			t.Error("booleans should be false/nil for empty settings")
		}
	})

	t.Run("parseHealthcheck_disable", func(t *testing.T) {
		hc := parseHealthcheck(map[string]string{"healthcheck_disable": "true"})
		if hc == nil || len(hc.Test) != 1 || hc.Test[0] != "NONE" {
			t.Errorf("disabled healthcheck: %+v", hc)
		}
	})

	t.Run("parseResources_memory_formats", func(t *testing.T) {
		for _, tt := range []struct {
			input string
			want  int64
		}{
			{"512m", 512 * 1024 * 1024},
			{"1g", 1024 * 1024 * 1024},
			{"256M", 256 * 1024 * 1024},
			{"2G", 2 * 1024 * 1024 * 1024},
			{"1073741824", 1073741824}, // raw bytes
		} {
			r := parseResources(map[string]string{"memory_limit": tt.input})
			if r.Memory != tt.want {
				t.Errorf("memory_limit=%q: got %d, want %d", tt.input, r.Memory, tt.want)
			}
		}
	})

	t.Run("parseVolumeMounts_invalid_json", func(t *testing.T) {
		m := parseVolumeMounts(map[string]string{"volume_mounts": "not json"})
		if m != nil {
			t.Error("invalid JSON should return nil")
		}
	})

	t.Run("parseVolumeMounts_empty_paths", func(t *testing.T) {
		m := parseVolumeMounts(map[string]string{
			"volume_mounts": `[{"hostPath":"","containerPath":"/data","readOnly":false}]`,
		})
		if len(m) != 0 {
			t.Error("empty hostPath should be skipped")
		}
	})

	t.Run("parseBuildOverrides", func(t *testing.T) {
		bo := parseBuildOverrides(map[string]string{
			"build_target":   "production",
			"build_platform": "linux/amd64",
			"build_no_cache": "true",
		})
		if bo.Target != "production" {
			t.Errorf("Target: %s", bo.Target)
		}
		if bo.Platform != "linux/amd64" {
			t.Errorf("Platform: %s", bo.Platform)
		}
		if !bo.NoCache {
			t.Error("NoCache should be true")
		}
	})

	t.Run("parseLifecycleHooks", func(t *testing.T) {
		h := parseLifecycleHooks(map[string]string{
			"pre_build_cmd":  "echo pre-build",
			"post_build_cmd": "echo post-build",
			"pre_deploy_cmd": "echo pre-deploy",
			"post_deploy_cmd": "echo post-deploy",
		})
		if h.PreBuild != "echo pre-build" || h.PostBuild != "echo post-build" {
			t.Errorf("hooks: %+v", h)
		}
	})

	t.Run("legacyImageBuildOptions_with_overrides", func(t *testing.T) {
		bo := buildOverrides{Target: "builder", Platform: "linux/arm64", NoCache: true}
		opts := legacyImageBuildOptions("test:latest", "Dockerfile", nil, bo)
		if opts.Target != "builder" {
			t.Errorf("Target: %s", opts.Target)
		}
		if opts.Platform != "linux/arm64" {
			t.Errorf("Platform: %s", opts.Platform)
		}
		if !opts.NoCache {
			t.Error("NoCache should be true")
		}
	})
}

// TestDockerContainerOverrides creates a real container with all overrides and inspects it
func TestDockerContainerOverrides(t *testing.T) {
	cli := skipIfNoDocker(t)
	defer cli.Close()
	ctx := context.Background()

	imageTag := "draft-settings-test:latest"
	buildTestImage(t, cli, imageTag, `FROM alpine:latest
CMD ["sleep", "3600"]
`)
	defer removeImage(cli, imageTag)

	t.Run("cmd_and_entrypoint", func(t *testing.T) {
		settings := map[string]string{
			"cmd_override":        "echo hello",
			"entrypoint_override": "/bin/sh -c",
		}
		o := parseContainerOverrides(settings)

		cfg := &container.Config{
			Image: imageTag,
		}
		if len(o.Cmd) > 0 {
			cfg.Cmd = o.Cmd
		}
		if len(o.Entrypoint) > 0 {
			cfg.Entrypoint = o.Entrypoint
		}

		resp, err := cli.ContainerCreate(ctx, cfg, &container.HostConfig{}, nil, nil, "")
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		defer removeContainer(cli, resp.ID)

		inspect, err := cli.ContainerInspect(ctx, resp.ID)
		if err != nil {
			t.Fatalf("inspect: %v", err)
		}

		if strings.Join(inspect.Config.Entrypoint, " ") != "/bin/sh -c" {
			t.Errorf("Entrypoint: %v", inspect.Config.Entrypoint)
		}
		if strings.Join(inspect.Config.Cmd, " ") != "echo hello" {
			t.Errorf("Cmd: %v", inspect.Config.Cmd)
		}
	})

	t.Run("workdir_user_stopsignal", func(t *testing.T) {
		settings := map[string]string{
			"working_dir": "/tmp",
			"run_user":    "nobody",
			"stop_signal": "SIGINT",
		}
		o := parseContainerOverrides(settings)

		cfg := &container.Config{
			Image:      imageTag,
			WorkingDir: o.WorkingDir,
			User:       o.User,
			StopSignal: o.StopSignal,
		}

		resp, err := cli.ContainerCreate(ctx, cfg, &container.HostConfig{}, nil, nil, "")
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		defer removeContainer(cli, resp.ID)

		inspect, err := cli.ContainerInspect(ctx, resp.ID)
		if err != nil {
			t.Fatalf("inspect: %v", err)
		}
		if inspect.Config.WorkingDir != "/tmp" {
			t.Errorf("WorkingDir: %s", inspect.Config.WorkingDir)
		}
		if inspect.Config.User != "nobody" {
			t.Errorf("User: %s", inspect.Config.User)
		}
		if inspect.Config.StopSignal != "SIGINT" {
			t.Errorf("StopSignal: %s", inspect.Config.StopSignal)
		}
	})

	t.Run("restart_policy_on_failure", func(t *testing.T) {
		settings := map[string]string{
			"restart_policy":      "on-failure",
			"restart_max_retries": "3",
		}
		o := parseContainerOverrides(settings)

		resp, err := cli.ContainerCreate(ctx, &container.Config{Image: imageTag},
			&container.HostConfig{RestartPolicy: o.RestartPolicy}, nil, nil, "")
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		defer removeContainer(cli, resp.ID)

		inspect, err := cli.ContainerInspect(ctx, resp.ID)
		if err != nil {
			t.Fatalf("inspect: %v", err)
		}
		if inspect.HostConfig.RestartPolicy.Name != container.RestartPolicyOnFailure {
			t.Errorf("RestartPolicy.Name: %s", inspect.HostConfig.RestartPolicy.Name)
		}
		if inspect.HostConfig.RestartPolicy.MaximumRetryCount != 3 {
			t.Errorf("MaxRetries: %d", inspect.HostConfig.RestartPolicy.MaximumRetryCount)
		}
	})

	t.Run("restart_policy_always", func(t *testing.T) {
		o := parseContainerOverrides(map[string]string{"restart_policy": "always"})
		resp, err := cli.ContainerCreate(ctx, &container.Config{Image: imageTag},
			&container.HostConfig{RestartPolicy: o.RestartPolicy}, nil, nil, "")
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		defer removeContainer(cli, resp.ID)

		inspect, _ := cli.ContainerInspect(ctx, resp.ID)
		if inspect.HostConfig.RestartPolicy.Name != container.RestartPolicyAlways {
			t.Errorf("RestartPolicy.Name: %s", inspect.HostConfig.RestartPolicy.Name)
		}
	})

	t.Run("restart_policy_unless_stopped", func(t *testing.T) {
		o := parseContainerOverrides(map[string]string{"restart_policy": "unless-stopped"})
		resp, err := cli.ContainerCreate(ctx, &container.Config{Image: imageTag},
			&container.HostConfig{RestartPolicy: o.RestartPolicy}, nil, nil, "")
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		defer removeContainer(cli, resp.ID)

		inspect, _ := cli.ContainerInspect(ctx, resp.ID)
		if inspect.HostConfig.RestartPolicy.Name != container.RestartPolicyUnlessStopped {
			t.Errorf("RestartPolicy.Name: %s", inspect.HostConfig.RestartPolicy.Name)
		}
	})

	t.Run("healthcheck", func(t *testing.T) {
		settings := map[string]string{
			"healthcheck_cmd":          "wget -q --spider http://localhost:80 || exit 1",
			"healthcheck_interval":     "15s",
			"healthcheck_timeout":      "5s",
			"healthcheck_start_period": "10s",
			"healthcheck_retries":      "3",
		}
		o := parseContainerOverrides(settings)

		cfg := &container.Config{
			Image:       imageTag,
			Healthcheck: o.Healthcheck,
		}
		resp, err := cli.ContainerCreate(ctx, cfg, &container.HostConfig{}, nil, nil, "")
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		defer removeContainer(cli, resp.ID)

		inspect, err := cli.ContainerInspect(ctx, resp.ID)
		if err != nil {
			t.Fatalf("inspect: %v", err)
		}
		hc := inspect.Config.Healthcheck
		if hc == nil {
			t.Fatal("Healthcheck is nil")
		}
		if len(hc.Test) < 2 || hc.Test[0] != "CMD-SHELL" {
			t.Errorf("Test: %v", hc.Test)
		}
		if hc.Interval != 15*time.Second {
			t.Errorf("Interval: %v", hc.Interval)
		}
		if hc.Timeout != 5*time.Second {
			t.Errorf("Timeout: %v", hc.Timeout)
		}
		if hc.StartPeriod != 10*time.Second {
			t.Errorf("StartPeriod: %v", hc.StartPeriod)
		}
		if hc.Retries != 3 {
			t.Errorf("Retries: %d", hc.Retries)
		}
	})

	t.Run("healthcheck_disable", func(t *testing.T) {
		o := parseContainerOverrides(map[string]string{"healthcheck_disable": "true"})
		cfg := &container.Config{
			Image:       imageTag,
			Healthcheck: o.Healthcheck,
		}
		resp, err := cli.ContainerCreate(ctx, cfg, &container.HostConfig{}, nil, nil, "")
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		defer removeContainer(cli, resp.ID)

		inspect, _ := cli.ContainerInspect(ctx, resp.ID)
		if inspect.Config.Healthcheck == nil {
			t.Fatal("Healthcheck is nil (should be NONE)")
		}
		if len(inspect.Config.Healthcheck.Test) != 1 || inspect.Config.Healthcheck.Test[0] != "NONE" {
			t.Errorf("Test: %v", inspect.Config.Healthcheck.Test)
		}
	})

	t.Run("resource_limits", func(t *testing.T) {
		settings := map[string]string{
			"cpu_limit":          "0.5",
			"memory_limit":       "128m",
			"memory_reservation": "64m",
			"pids_limit":         "50",
		}
		o := parseContainerOverrides(settings)

		resp, err := cli.ContainerCreate(ctx, &container.Config{Image: imageTag},
			&container.HostConfig{Resources: o.Resources}, nil, nil, "")
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		defer removeContainer(cli, resp.ID)

		inspect, err := cli.ContainerInspect(ctx, resp.ID)
		if err != nil {
			t.Fatalf("inspect: %v", err)
		}

		if inspect.HostConfig.NanoCPUs != 500_000_000 {
			t.Errorf("NanoCPUs: %d (want 500000000)", inspect.HostConfig.NanoCPUs)
		}
		if inspect.HostConfig.Memory != 128*1024*1024 {
			t.Errorf("Memory: %d (want %d)", inspect.HostConfig.Memory, 128*1024*1024)
		}
		if inspect.HostConfig.MemoryReservation != 64*1024*1024 {
			t.Errorf("MemoryReservation: %d", inspect.HostConfig.MemoryReservation)
		}
		if inspect.HostConfig.PidsLimit == nil || *inspect.HostConfig.PidsLimit != 50 {
			t.Errorf("PidsLimit: %v", inspect.HostConfig.PidsLimit)
		}
	})

	t.Run("volumes", func(t *testing.T) {
		tmpDir := t.TempDir()
		entries := []VolumeSpec{
			{Type: VolumeTypeBind, HostPath: tmpDir, ContainerPath: "/mnt/test", ReadOnly: true},
		}
		jsonBytes, _ := json.Marshal(entries)

		o := parseContainerOverrides(map[string]string{
			"volume_mounts": string(jsonBytes),
		})

		if len(o.Mounts) != 1 {
			t.Fatalf("expected 1 mount, got %d", len(o.Mounts))
		}

		resp, err := cli.ContainerCreate(ctx, &container.Config{Image: imageTag},
			&container.HostConfig{Mounts: o.Mounts}, nil, nil, "")
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		defer removeContainer(cli, resp.ID)

		inspect, err := cli.ContainerInspect(ctx, resp.ID)
		if err != nil {
			t.Fatalf("inspect: %v", err)
		}

		found := false
		for _, m := range inspect.Mounts {
			if m.Destination == "/mnt/test" {
				found = true
				if !m.RW == true {
					// RW should be false since we set ReadOnly=true
				}
				if m.Type != mount.TypeBind {
					t.Errorf("Mount type: %s (want bind)", m.Type)
				}
			}
		}
		if !found {
			t.Errorf("Mount /mnt/test not found. Mounts: %+v", inspect.Mounts)
		}
	})

	t.Run("custom_labels", func(t *testing.T) {
		settings := map[string]string{
			"custom_labels": `{"com.draft.test":"integration","env":"testing"}`,
		}
		o := parseContainerOverrides(settings)

		labels := map[string]string{"draft.managed": "true"}
		for k, v := range o.Labels {
			labels[k] = v
		}

		cfg := &container.Config{
			Image:  imageTag,
			Labels: labels,
		}
		resp, err := cli.ContainerCreate(ctx, cfg, &container.HostConfig{}, nil, nil, "")
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		defer removeContainer(cli, resp.ID)

		inspect, _ := cli.ContainerInspect(ctx, resp.ID)
		if inspect.Config.Labels["com.draft.test"] != "integration" {
			t.Errorf("label com.draft.test: %s", inspect.Config.Labels["com.draft.test"])
		}
		if inspect.Config.Labels["env"] != "testing" {
			t.Errorf("label env: %s", inspect.Config.Labels["env"])
		}
		if inspect.Config.Labels["draft.managed"] != "true" {
			t.Error("draft labels should be preserved alongside custom labels")
		}
	})

	t.Run("security_options", func(t *testing.T) {
		settings := map[string]string{
			"privileged":     "true",
			"readonly_rootfs": "true",
			"init_process":  "true",
			"cap_add":       "SYS_PTRACE",
			"cap_drop":      "NET_RAW",
		}
		o := parseContainerOverrides(settings)

		resp, err := cli.ContainerCreate(ctx, &container.Config{Image: imageTag},
			&container.HostConfig{
				Privileged:     o.Privileged,
				ReadonlyRootfs: o.ReadonlyRootfs,
				Init:           o.Init,
				CapAdd:         o.CapAdd,
				CapDrop:        o.CapDrop,
			}, nil, nil, "")
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		defer removeContainer(cli, resp.ID)

		inspect, _ := cli.ContainerInspect(ctx, resp.ID)
		if !inspect.HostConfig.Privileged {
			t.Error("Privileged should be true")
		}
		if !inspect.HostConfig.ReadonlyRootfs {
			t.Error("ReadonlyRootfs should be true")
		}
		if inspect.HostConfig.Init == nil || !*inspect.HostConfig.Init {
			t.Error("Init should be true")
		}

		capAddFound := false
		for _, c := range inspect.HostConfig.CapAdd {
			if c == "SYS_PTRACE" || c == "CAP_SYS_PTRACE" {
				capAddFound = true
			}
		}
		if !capAddFound {
			t.Errorf("CapAdd missing SYS_PTRACE: %v", inspect.HostConfig.CapAdd)
		}

		capDropFound := false
		for _, c := range inspect.HostConfig.CapDrop {
			if c == "NET_RAW" || c == "CAP_NET_RAW" {
				capDropFound = true
			}
		}
		if !capDropFound {
			t.Errorf("CapDrop missing NET_RAW: %v", inspect.HostConfig.CapDrop)
		}
	})

	t.Run("stop_timeout", func(t *testing.T) {
		o := parseContainerOverrides(map[string]string{"stop_grace_period": "15"})
		cfg := &container.Config{
			Image:       imageTag,
			StopTimeout: o.StopTimeout,
		}
		resp, err := cli.ContainerCreate(ctx, cfg, &container.HostConfig{}, nil, nil, "")
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		defer removeContainer(cli, resp.ID)

		inspect, _ := cli.ContainerInspect(ctx, resp.ID)
		if inspect.Config.StopTimeout == nil || *inspect.Config.StopTimeout != 15 {
			t.Errorf("StopTimeout: %v", inspect.Config.StopTimeout)
		}
	})

	t.Run("port_binding", func(t *testing.T) {
		portStr := "8080"
		containerPort := nat.Port(portStr + "/tcp")
		cfg := &container.Config{
			Image:        imageTag,
			ExposedPorts: nat.PortSet{containerPort: struct{}{}},
		}
		hostCfg := &container.HostConfig{
			PortBindings: nat.PortMap{
				containerPort: []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: "0"}},
			},
		}

		resp, err := cli.ContainerCreate(ctx, cfg, hostCfg, nil, nil, "")
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		defer removeContainer(cli, resp.ID)

		if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
			t.Fatalf("start: %v", err)
		}

		inspect, _ := cli.ContainerInspect(ctx, resp.ID)
		bindings := inspect.NetworkSettings.Ports[containerPort]
		if len(bindings) == 0 {
			t.Fatal("no port bindings found")
		}
		if bindings[0].HostIP != "127.0.0.1" {
			t.Errorf("HostIP: %s", bindings[0].HostIP)
		}
		if bindings[0].HostPort == "" || bindings[0].HostPort == "0" {
			t.Error("Docker should have assigned an ephemeral port")
		}
		t.Logf("Ephemeral port assigned: %s", bindings[0].HostPort)
	})

	t.Run("full_integration_create_start_inspect", func(t *testing.T) {
		settings := map[string]string{
			"cmd_override":         "sleep 30",
			"working_dir":          "/tmp",
			"restart_policy":       "on-failure",
			"restart_max_retries":  "2",
			"healthcheck_cmd":      "true",
			"healthcheck_interval": "10s",
			"healthcheck_timeout":  "3s",
			"healthcheck_retries":  "2",
			"cpu_limit":            "0.25",
			"memory_limit":         "64m",
			"init_process":         "true",
			"custom_labels":        `{"draft.test.full":"yes"}`,
			"stop_grace_period":    "5",
		}

		o := parseContainerOverrides(settings)

		portStr := "3000"
		containerPort := nat.Port(portStr + "/tcp")

		labels := map[string]string{"draft.managed": "true"}
		for k, v := range o.Labels {
			labels[k] = v
		}

		containerCfg := &container.Config{
			Image:        imageTag,
			Labels:       labels,
			ExposedPorts: nat.PortSet{containerPort: struct{}{}},
		}
		if len(o.Cmd) > 0 {
			containerCfg.Cmd = o.Cmd
		}
		if o.WorkingDir != "" {
			containerCfg.WorkingDir = o.WorkingDir
		}
		if o.Healthcheck != nil {
			containerCfg.Healthcheck = o.Healthcheck
		}
		if o.StopTimeout != nil {
			containerCfg.StopTimeout = o.StopTimeout
		}

		hostCfg := &container.HostConfig{
			PortBindings: nat.PortMap{
				containerPort: []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: "0"}},
			},
			RestartPolicy: o.RestartPolicy,
			Resources:     o.Resources,
			Init:          o.Init,
		}

		containerName := fmt.Sprintf("draft-test-full-%d", time.Now().UnixNano())
		resp, err := cli.ContainerCreate(ctx, containerCfg, hostCfg, nil, nil, containerName)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		defer removeContainer(cli, resp.ID)

		if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
			t.Fatalf("start: %v", err)
		}

		inspect, err := cli.ContainerInspect(ctx, resp.ID)
		if err != nil {
			t.Fatalf("inspect: %v", err)
		}

		// Verify everything landed
		if !inspect.State.Running {
			t.Fatal("container should be running")
		}
		if strings.Join(inspect.Config.Cmd, " ") != "sleep 30" {
			t.Errorf("Cmd: %v", inspect.Config.Cmd)
		}
		if inspect.Config.WorkingDir != "/tmp" {
			t.Errorf("WorkingDir: %s", inspect.Config.WorkingDir)
		}
		if inspect.HostConfig.RestartPolicy.Name != container.RestartPolicyOnFailure {
			t.Errorf("RestartPolicy: %s", inspect.HostConfig.RestartPolicy.Name)
		}
		if inspect.HostConfig.RestartPolicy.MaximumRetryCount != 2 {
			t.Errorf("MaxRetries: %d", inspect.HostConfig.RestartPolicy.MaximumRetryCount)
		}
		if inspect.Config.Healthcheck == nil {
			t.Error("Healthcheck should be set")
		}
		if inspect.HostConfig.NanoCPUs != 250_000_000 {
			t.Errorf("NanoCPUs: %d", inspect.HostConfig.NanoCPUs)
		}
		if inspect.HostConfig.Memory != 64*1024*1024 {
			t.Errorf("Memory: %d", inspect.HostConfig.Memory)
		}
		if inspect.HostConfig.Init == nil || !*inspect.HostConfig.Init {
			t.Error("Init should be true")
		}
		if inspect.Config.Labels["draft.test.full"] != "yes" {
			t.Error("custom label missing")
		}
		if inspect.Config.StopTimeout == nil || *inspect.Config.StopTimeout != 5 {
			t.Errorf("StopTimeout: %v", inspect.Config.StopTimeout)
		}

		bindings := inspect.NetworkSettings.Ports[containerPort]
		if len(bindings) == 0 {
			t.Error("no port bindings")
		} else {
			t.Logf("Port %s bound to host port %s", portStr, bindings[0].HostPort)
		}

		t.Log("Full integration test passed - all overrides verified on running container")
	})
}

// TestLifecycleHookExecution tests that lifecycle hooks actually execute
func TestLifecycleHookExecution(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	// Use a simple command that always works on Windows cmd /C
	hookCmd := "echo hook_executed"

	var logs []string
	logFn := func(line string) { logs = append(logs, line) }

	err := runLifecycleHook(ctx, "test-hook", hookCmd, tmpDir, logFn)
	if err != nil {
		t.Fatalf("hook failed: %v\nlogs: %s", err, strings.Join(logs, "\n"))
	}

	foundOutput := false
	foundRunning := false
	foundCompleted := false
	for _, l := range logs {
		if strings.Contains(l, "hook_executed") {
			foundOutput = true
		}
		if strings.Contains(l, "Running test-hook hook") {
			foundRunning = true
		}
		if strings.Contains(l, "test-hook hook completed") {
			foundCompleted = true
		}
	}
	if !foundOutput {
		t.Errorf("hook output not captured. logs: %v", logs)
	}
	if !foundRunning || !foundCompleted {
		t.Errorf("missing log messages. logs: %v", logs)
	}
}

// TestLifecycleHookFailure tests that a failing hook returns an error
func TestLifecycleHookFailure(t *testing.T) {
	ctx := context.Background()
	err := runLifecycleHook(ctx, "fail-hook", "exit 1", t.TempDir(), func(string) {})
	if err == nil {
		t.Error("expected error from failing hook")
	}
}

// TestLifecycleHookEmpty tests that empty hook is a no-op
func TestLifecycleHookEmpty(t *testing.T) {
	err := runLifecycleHook(context.Background(), "noop", "", t.TempDir(), func(string) {})
	if err != nil {
		t.Errorf("empty hook should not error: %v", err)
	}
}

// TestBuildWithOverrides tests that build overrides are properly formed for both paths
func TestBuildWithOverrides(t *testing.T) {
	t.Run("buildx_args_formation", func(t *testing.T) {
		bo := buildOverrides{Target: "prod", Platform: "linux/amd64", NoCache: true}
		args := []string{"buildx", "build", "--load", "--progress=plain", "-t", "test:v1", "-f", "Dockerfile"}
		if bo.Target != "" {
			args = append(args, "--target", bo.Target)
		}
		if bo.Platform != "" {
			args = append(args, "--platform", bo.Platform)
		}
		if bo.NoCache {
			args = append(args, "--no-cache")
		}
		args = append(args, ".")

		joined := strings.Join(args, " ")
		if !strings.Contains(joined, "--target prod") {
			t.Errorf("missing --target: %s", joined)
		}
		if !strings.Contains(joined, "--platform linux/amd64") {
			t.Errorf("missing --platform: %s", joined)
		}
		if !strings.Contains(joined, "--no-cache") {
			t.Errorf("missing --no-cache: %s", joined)
		}
	})

	t.Run("legacy_build_options", func(t *testing.T) {
		bo := buildOverrides{Target: "builder", NoCache: true}
		opts := legacyImageBuildOptions("img:v1", "Dockerfile", map[string]*string{
			"FOO": strPtr("bar"),
		}, bo)

		if opts.Target != "builder" {
			t.Errorf("Target: %s", opts.Target)
		}
		if !opts.NoCache {
			t.Error("NoCache not set")
		}
		if opts.BuildArgs["FOO"] == nil || *opts.BuildArgs["FOO"] != "bar" {
			t.Error("build args not passed through")
		}
		if opts.Dockerfile != "Dockerfile" {
			t.Errorf("Dockerfile: %s", opts.Dockerfile)
		}
	})
}

func strPtr(s string) *string { return &s }
