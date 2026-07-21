package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"Draft/internal/store"
)

// SandboxTestRunMode selects whether a test run rebuilds the stack or only
// re-executes steps on an existing live testing sandbox.
type SandboxTestRunMode string

const (
	SandboxTestRunFresh SandboxTestRunMode = "fresh"
	SandboxTestRunSteps SandboxTestRunMode = "steps"
)

// SandboxTestRunRequest starts a testing-sandbox suite.
//
// Fresh mode creates a new sandbox from SourceEnvironmentID + ProfileID/Plan
// (same path as CreateSandbox). Steps mode reuses SandboxID's live stack.
type SandboxTestRunRequest struct {
	Name                string              `json:"name"`
	SourceEnvironmentID uint                `json:"sourceEnvironmentId"`
	ProfileID           uint                `json:"profileId,omitempty"`
	Plan                SandboxPlan         `json:"plan"`
	Links               []store.SandboxLink `json:"links,omitempty"`
	// SandboxID is required for mode=steps; optional for mode=fresh (when set
	// with fresh, the previous sandbox is deleted after a successful create).
	SandboxID uint               `json:"sandboxId,omitempty"`
	Mode      SandboxTestRunMode `json:"mode,omitempty"`
}

// SandboxTestStepResult is the outcome of one plan step.
type SandboxTestStepResult struct {
	Name         string `json:"name"`
	ServiceLabel string `json:"serviceLabel"`
	NodeID       string `json:"nodeId,omitempty"`
	ExitCode     int    `json:"exitCode"`
	Output       string `json:"output,omitempty"`
	Error        string `json:"error,omitempty"`
	// DurationMs is wall time spent waiting for the command (and readiness for
	// the first attempt), not including prior deploy time.
	DurationMs int64 `json:"durationMs"`
	// Status is queued while the sandbox is being prepared, running while the
	// command is attached, and passed/failed once its result is durable.
	Status string `json:"status,omitempty"`
}

// SandboxTestRunResult is the API response for a completed (or failed) suite.
type SandboxTestRunResult struct {
	Run     store.SandboxTestRun    `json:"run"`
	Sandbox *store.Sandbox          `json:"sandbox,omitempty"`
	Steps   []SandboxTestStepResult `json:"steps"`
	Stack   *EnvironmentStackResult `json:"stack,omitempty"`
}

// ListSandboxTestRuns returns recent runs for a project (history after cleanup).
func (e *Engine) ListSandboxTestRuns(projectID uint, limit int) ([]store.SandboxTestRun, error) {
	return e.store.ListSandboxTestRuns(projectID, limit)
}

// GetSandboxTestRun returns one run with decoded step results when present.
func (e *Engine) GetSandboxTestRun(runID uint) (*SandboxTestRunResult, error) {
	run, err := e.store.GetSandboxTestRun(runID)
	if err != nil {
		return nil, err
	}
	out := &SandboxTestRunResult{Run: *run}
	if run.SandboxID != 0 {
		if sb, err := e.store.GetSandbox(run.SandboxID); err == nil {
			out.Sandbox = sb
		}
	}
	if err := json.Unmarshal([]byte(run.StepsJSON), &out.Steps); err != nil {
		out.Steps = nil
	}
	return out, nil
}

// RunTestingSandbox materializes (or reuses) a testing sandbox and executes
// plan steps. The durable recipe is the plan/profile; this call produces a
// SandboxTestRun history row plus an optional live sandbox instance.
func (e *Engine) RunTestingSandbox(ctx context.Context, req SandboxTestRunRequest) (*SandboxTestRunResult, error) {
	mode := req.Mode
	if mode == "" {
		mode = SandboxTestRunFresh
	}
	switch mode {
	case SandboxTestRunFresh, SandboxTestRunSteps:
	default:
		return nil, fmt.Errorf("invalid test run mode %q", mode)
	}

	if mode == SandboxTestRunSteps {
		return e.runTestingSandboxSteps(ctx, req)
	}
	return e.runTestingSandboxFresh(ctx, req)
}

// StartTestingSandbox creates the durable run and its sandbox, then continues
// the expensive deploy/readiness/step work in the daemon. This lets callers
// move to the sandbox canvas immediately and poll the same durable run.
func (e *Engine) StartTestingSandbox(ctx context.Context, req SandboxTestRunRequest) (*SandboxTestRunResult, error) {
	mode := req.Mode
	if mode == "" {
		mode = SandboxTestRunFresh
	}
	var prepared *SandboxTestRunResult
	var plan SandboxPlan
	var err error
	startStack := false
	switch mode {
	case SandboxTestRunFresh:
		prepared, plan, err = e.prepareTestingSandboxFresh(ctx, req)
		startStack = true
	case SandboxTestRunSteps:
		prepared, plan, err = e.prepareTestingSandboxSteps(ctx, req)
	default:
		return nil, fmt.Errorf("invalid test run mode %q", mode)
	}
	if err != nil {
		return prepared, err
	}
	go func() {
		// The HTTP/Wails request ends as soon as the canvas can be opened; do
		// not let that cancellation terminate the daemon-owned test run.
		_, _ = e.executeTestingSandbox(context.WithoutCancel(ctx), &prepared.Run, prepared.Sandbox, plan, startStack)
	}()
	return prepared, nil
}

func (e *Engine) runTestingSandboxFresh(ctx context.Context, req SandboxTestRunRequest) (*SandboxTestRunResult, error) {
	prepared, plan, err := e.prepareTestingSandboxFresh(ctx, req)
	if err != nil {
		return prepared, err
	}
	return e.executeTestingSandbox(ctx, &prepared.Run, prepared.Sandbox, plan, true)
}

// prepareTestingSandboxFresh persists a run before doing asynchronous work and
// returns as soon as the sandbox environment exists. The initial queued steps
// make a newly-created run inspectable even if Draft quits during deployment.
func (e *Engine) prepareTestingSandboxFresh(ctx context.Context, req SandboxTestRunRequest) (*SandboxTestRunResult, SandboxPlan, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "test-run"
	}
	// Force purpose=test so lifecycle defaults and step validation apply even
	// when the caller only set steps.
	plan := req.Plan
	plan.Purpose = SandboxPurposeTest
	createReq := SandboxCreateRequest{
		Name:                name,
		SourceEnvironmentID: req.SourceEnvironmentID,
		ProfileID:           req.ProfileID,
		Plan:                plan,
		Links:               req.Links,
	}
	// Preview first so we can persist a run row with the resolved plan even if
	// create fails later.
	preview, err := e.PreviewSandbox(ctx, createReq)
	if err != nil {
		return nil, SandboxPlan{}, err
	}
	if preview.Plan.Purpose != SandboxPurposeTest {
		return nil, SandboxPlan{}, fmt.Errorf("testing sandbox requires purpose=test")
	}

	planJSON, _ := json.Marshal(preview.Plan)
	run := &store.SandboxTestRun{
		ProjectID:           preview.ProjectID,
		ProfileID:           preview.ProfileID,
		SourceEnvironmentID: preview.SourceEnvironmentID,
		Name:                name,
		Mode:                string(SandboxTestRunFresh),
		Status:              "running",
		PlanJSON:            string(planJSON),
		StepsJSON:           testStepResultsJSON(preview.Plan.Steps),
		StartedAt:           time.Now().UTC(),
	}
	if _, err := e.store.CreateSandboxTestRun(run); err != nil {
		return nil, SandboxPlan{}, err
	}

	// Replace previous live instance when rerunning a known sandbox id.
	previousID := req.SandboxID
	// Testing path starts the stack inside executeTestingSandbox; do not double-start.
	createReq.StartOnCreate = false
	created, err := e.CreateSandbox(ctx, createReq)
	if err != nil {
		e.finishTestRun(run, "failed", queuedTestStepResults(preview.Plan.Steps), err.Error())
		return &SandboxTestRunResult{Run: *run}, SandboxPlan{}, err
	}
	sandbox := created.Sandbox
	run.SandboxID = sandbox.ID
	_ = e.store.UpdateSandboxTestRun(run)

	if previousID != 0 && previousID != sandbox.ID {
		_ = e.DeleteSandbox(ctx, previousID)
	}

	return &SandboxTestRunResult{Run: *run, Sandbox: sandbox, Steps: queuedTestStepResults(preview.Plan.Steps)}, preview.Plan, nil
}

func (e *Engine) runTestingSandboxSteps(ctx context.Context, req SandboxTestRunRequest) (*SandboxTestRunResult, error) {
	prepared, plan, err := e.prepareTestingSandboxSteps(ctx, req)
	if err != nil {
		return prepared, err
	}
	return e.executeTestingSandbox(ctx, &prepared.Run, prepared.Sandbox, plan, false)
}

func (e *Engine) prepareTestingSandboxSteps(ctx context.Context, req SandboxTestRunRequest) (*SandboxTestRunResult, SandboxPlan, error) {
	if req.SandboxID == 0 {
		return nil, SandboxPlan{}, fmt.Errorf("sandboxId is required for steps mode")
	}
	sandbox, err := e.store.GetSandbox(req.SandboxID)
	if err != nil {
		return nil, SandboxPlan{}, err
	}
	if sandbox.Purpose != string(SandboxPurposeTest) && !planPurposeIsTest(sandbox.PlanJSON) {
		return nil, SandboxPlan{}, fmt.Errorf("sandbox %q is not a testing sandbox", sandbox.Name)
	}
	if sandbox.Status == "expired" || sandbox.Status == "cleanup_failed" {
		return nil, SandboxPlan{}, fmt.Errorf("sandbox %q is not live (status %s); use fresh mode", sandbox.Name, sandbox.Status)
	}
	var plan SandboxPlan
	if err := json.Unmarshal([]byte(sandbox.PlanJSON), &plan); err != nil {
		return nil, SandboxPlan{}, fmt.Errorf("read sandbox plan: %w", err)
	}
	// Allow request plan to override steps only (recipe tweaks without rebuild).
	if req.Plan.Steps != nil {
		plan.Steps = req.Plan.Steps
	}
	if req.Plan.OnComplete != "" {
		plan.OnComplete = req.Plan.OnComplete
	}
	plan.Purpose = SandboxPurposeTest
	// Re-validate steps through resolve defaults path pieces.
	if err := validateTestingPlanSteps(&plan); err != nil {
		return nil, SandboxPlan{}, err
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = sandbox.Name
	}
	planJSON, _ := json.Marshal(plan)
	run := &store.SandboxTestRun{
		ProjectID:           sandbox.ProjectID,
		ProfileID:           sandbox.ProfileID,
		SandboxID:           sandbox.ID,
		SourceEnvironmentID: sandbox.SourceEnvironmentID,
		Name:                name,
		Mode:                string(SandboxTestRunSteps),
		Status:              "running",
		PlanJSON:            string(planJSON),
		StepsJSON:           testStepResultsJSON(plan.Steps),
		StartedAt:           time.Now().UTC(),
	}
	if _, err := e.store.CreateSandboxTestRun(run); err != nil {
		return nil, SandboxPlan{}, err
	}

	// Resume if suspended so commands have containers.
	if sandbox.Status == "suspended" {
		if _, err := e.ResumeSandbox(ctx, sandbox.ID); err != nil {
			e.finishTestRun(run, "failed", nil, err.Error())
			return &SandboxTestRunResult{Run: *run, Sandbox: sandbox}, SandboxPlan{}, err
		}
		sandbox, _ = e.store.GetSandbox(sandbox.ID)
	}
	return &SandboxTestRunResult{Run: *run, Sandbox: sandbox, Steps: queuedTestStepResults(plan.Steps)}, plan, nil
}

func (e *Engine) executeTestingSandbox(ctx context.Context, run *store.SandboxTestRun, sandbox *store.Sandbox, plan SandboxPlan, startStack bool) (*SandboxTestRunResult, error) {
	out := &SandboxTestRunResult{Run: *run, Sandbox: sandbox, Steps: queuedTestStepResults(plan.Steps)}
	e.saveTestRunProgress(run, out.Steps)

	if startStack {
		// Fresh runs deploy the newly materialized environment. Step-only reruns
		// deliberately skip this: calling StackStart here redeployed containers
		// and could cancel the command sequence it was meant to repeat.
		stack, err := e.RunEnvironmentStack(ctx, sandbox.EnvironmentID, StackStart)
		out.Stack = stack
		if err != nil {
			e.finishTestRun(run, "failed", out.Steps, err.Error())
			out.Run = *run
			return out, err
		}
	}

	nodes, err := e.store.ListNodesByEnvironment(sandbox.EnvironmentID)
	if err != nil {
		e.finishTestRun(run, "failed", out.Steps, err.Error())
		out.Run = *run
		return out, err
	}
	byLabel := map[string]store.CanvasNode{}
	for _, n := range nodes {
		byLabel[n.Label] = n
	}

	// Wait for non-linked services that will run steps (and best-effort for all
	// copied services so integration tests see healthy deps).
	if err := e.waitSandboxServicesReady(ctx, nodes, 3*time.Minute); err != nil {
		e.finishTestRun(run, "failed", out.Steps, err.Error())
		out.Run = *run
		return out, err
	}

	allPassed := true
	for i, step := range plan.Steps {
		stepRes := SandboxTestStepResult{
			Name:         step.Name,
			ServiceLabel: step.ServiceLabel,
			Status:       "running",
		}
		// Replace the queued entry before attaching to Docker so a reader can
		// see exactly which command is currently running.
		out.Steps[i] = stepRes
		e.saveTestRunProgress(run, out.Steps)
		node, ok := byLabel[step.ServiceLabel]
		if !ok {
			stepRes.Error = fmt.Sprintf("service %q not found in sandbox", step.ServiceLabel)
			stepRes.ExitCode = -1
			allPassed = false
			stepRes.Status = "failed"
			out.Steps[i] = stepRes
			break
		}
		stepRes.NodeID = node.ID
		// Linked aliases have no local container — resolve to root for exec.
		execNodeID := node.ID
		if settings, err := e.store.GetNodeSettings(node.ID); err == nil {
			if link := ParseServiceLink(settings[SettingServiceLink]); link != nil {
				execNodeID = link.RootNodeID
			}
		}
		start := time.Now()
		cmdRes, cmdErr := e.RunCommandStream(ctx, execNodeID, step.Cmd, step.WorkDir, func(chunk string) {
			stepRes.Output += chunk
			out.Steps[i] = stepRes
			e.saveTestRunProgress(run, out.Steps)
		})
		stepRes.DurationMs = time.Since(start).Milliseconds()
		stepRes.Output = cmdRes.Output
		stepRes.ExitCode = cmdRes.ExitCode
		if cmdErr != nil {
			stepRes.ExitCode = -1
			stepRes.Error = cmdErr.Error()
			allPassed = false
			stepRes.Status = "failed"
			out.Steps[i] = stepRes
			break
		}
		if cmdRes.Error != "" {
			stepRes.Error = cmdRes.Error
			allPassed = false
			stepRes.Status = "failed"
			out.Steps[i] = stepRes
			break
		}
		if !stepPassed(step, stepRes) {
			allPassed = false
			if stepRes.Error == "" {
				stepRes.Error = stepFailureReason(step, stepRes)
			}
			stepRes.Status = "failed"
			out.Steps[i] = stepRes
			break
		}
		stepRes.Status = "passed"
		out.Steps[i] = stepRes
		e.saveTestRunProgress(run, out.Steps)
	}

	status := "passed"
	errMsg := ""
	if !allPassed {
		status = "failed"
		if len(out.Steps) > 0 {
			last := out.Steps[len(out.Steps)-1]
			if last.Error != "" {
				errMsg = last.Error
			} else {
				errMsg = fmt.Sprintf("step %q exited %d", last.Name, last.ExitCode)
			}
		} else {
			errMsg = "no steps executed"
		}
	}
	e.finishTestRun(run, status, out.Steps, errMsg)
	out.Run = *run

	// Post-run lifecycle. Failures here do not rewrite pass/fail of the suite.
	switch plan.OnComplete {
	case SandboxOnCompleteDelete:
		if delErr := e.DeleteSandbox(ctx, sandbox.ID); delErr == nil {
			out.Sandbox = nil
			run.SandboxID = 0
			_ = e.store.UpdateSandboxTestRun(run)
			out.Run = *run
		}
	case SandboxOnCompleteSuspend:
		if sb, susErr := e.SuspendSandbox(ctx, sandbox.ID); susErr == nil {
			out.Sandbox = sb
		}
	}

	// Suite assertion failures are reported via run.Status / run.Error, not as
	// a transport error — callers always get the full step transcript.
	return out, nil
}

func (e *Engine) finishTestRun(run *store.SandboxTestRun, status string, steps []SandboxTestStepResult, errMsg string) {
	if run == nil {
		return
	}
	now := time.Now().UTC()
	run.Status = status
	run.Error = errMsg
	run.FinishedAt = &now
	if steps == nil {
		steps = []SandboxTestStepResult{}
	}
	if raw, err := json.Marshal(steps); err == nil {
		run.StepsJSON = string(raw)
	}
	_ = e.store.UpdateSandboxTestRun(run)
}

func queuedTestStepResults(steps []SandboxStep) []SandboxTestStepResult {
	out := make([]SandboxTestStepResult, len(steps))
	for i, step := range steps {
		out[i] = SandboxTestStepResult{Name: step.Name, ServiceLabel: step.ServiceLabel, Status: "queued"}
	}
	return out
}

func testStepResultsJSON(steps []SandboxStep) string {
	raw, err := json.Marshal(queuedTestStepResults(steps))
	if err != nil {
		return "[]"
	}
	return string(raw)
}

// saveTestRunProgress deliberately updates the same history row after every
// state change. Polling readers therefore see current work and a daemon crash
// still leaves an honest partial record instead of an empty completed-looking run.
func (e *Engine) saveTestRunProgress(run *store.SandboxTestRun, steps []SandboxTestStepResult) {
	if run == nil {
		return
	}
	if raw, err := json.Marshal(steps); err == nil {
		run.StepsJSON = string(raw)
	}
	_ = e.store.UpdateSandboxTestRun(run)
	if e.emit != nil {
		e.emit("sandbox:test-progress", map[string]any{"runId": run.ID, "run": run, "steps": steps})
	}
}

func stepPassed(step SandboxStep, result SandboxTestStepResult) bool {
	exitCodes := step.ExpectedExitCodes
	if len(exitCodes) == 0 && strings.TrimSpace(step.OutputContains) == "" {
		exitCodes = []int{0}
	}
	for _, code := range exitCodes {
		if result.ExitCode == code {
			return true
		}
	}
	return outputContainsLoose(result.Output, step.OutputContains)
}

func stepFailureReason(step SandboxStep, result SandboxTestStepResult) string {
	parts := make([]string, 0, 2)
	if len(step.ExpectedExitCodes) > 0 {
		parts = append(parts, fmt.Sprintf("exit %d was not accepted", result.ExitCode))
	} else if strings.TrimSpace(step.OutputContains) == "" {
		parts = append(parts, fmt.Sprintf("step exited %d", result.ExitCode))
	}
	if strings.TrimSpace(step.OutputContains) != "" {
		parts = append(parts, fmt.Sprintf("output did not contain %q", step.OutputContains))
	}
	return strings.Join(parts, "; ")
}

func outputContainsLoose(output, expected string) bool {
	expected = strings.Join(strings.Fields(expected), " ")
	if expected == "" {
		return false
	}
	return strings.Contains(strings.Join(strings.Fields(output), " "), expected)
}

func planPurposeIsTest(planJSON string) bool {
	var plan SandboxPlan
	if err := json.Unmarshal([]byte(planJSON), &plan); err != nil {
		return false
	}
	return plan.Purpose == SandboxPurposeTest
}

func validateTestingPlanSteps(plan *SandboxPlan) error {
	if plan == nil {
		return fmt.Errorf("plan is required")
	}
	if len(plan.Steps) == 0 {
		return fmt.Errorf("testing sandbox requires at least one test step")
	}
	for i, step := range plan.Steps {
		step.ServiceLabel = strings.TrimSpace(step.ServiceLabel)
		step.Name = strings.TrimSpace(step.Name)
		step.WorkDir = strings.TrimSpace(step.WorkDir)
		if step.ServiceLabel == "" {
			return fmt.Errorf("test step %d requires serviceLabel", i+1)
		}
		if len(step.Cmd) == 0 {
			return fmt.Errorf("test step %d requires cmd", i+1)
		}
		if step.Name == "" {
			step.Name = strings.Join(step.Cmd, " ")
		}
		step.OutputContains = strings.TrimSpace(step.OutputContains)
		for _, code := range step.ExpectedExitCodes {
			if code < 0 {
				return fmt.Errorf("test step %d has invalid expected exit code", i+1)
			}
		}
		plan.Steps[i] = step
	}
	return nil
}

// waitSandboxServicesReady polls until each non-linked node has a running
// deployment with a container id, or until timeout.
func (e *Engine) waitSandboxServicesReady(ctx context.Context, nodes []store.CanvasNode, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		pending := 0
		var lastReason string
		for _, node := range nodes {
			settings, _ := e.store.GetNodeSettings(node.ID)
			if ParseServiceLink(settings[SettingServiceLink]) != nil {
				// Shared/linked: readiness is the root's problem; skip.
				continue
			}
			// Skip nodes that cannot deploy (no image/dockerfile) — they will
			// never become running and would hang the suite.
			if !nodeLooksDeployable(settings) {
				continue
			}
			dep, err := e.store.ActiveDeployment(node.ID)
			if err != nil || dep == nil || dep.ContainerID == "" || dep.Status != "running" {
				pending++
				if dep != nil {
					lastReason = fmt.Sprintf("%s is %s", node.Label, dep.Status)
				} else {
					lastReason = fmt.Sprintf("%s has no active deployment", node.Label)
				}
				continue
			}
		}
		if pending == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			if lastReason == "" {
				lastReason = "services did not become ready"
			}
			return fmt.Errorf("timeout waiting for sandbox services: %s", lastReason)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func nodeLooksDeployable(settings map[string]string) bool {
	if settings == nil {
		return false
	}
	image := strings.TrimSpace(settings["image"])
	dockerfile := strings.TrimSpace(settings["dockerfile"])
	port := strings.TrimSpace(settings["service_port"])
	if port == "" {
		return false
	}
	return image != "" || dockerfile != ""
}
