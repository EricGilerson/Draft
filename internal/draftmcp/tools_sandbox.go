package draftmcp

import (
	"Draft/internal/deploy"
	"Draft/internal/store"
	"context"
	"fmt"
)

func sandboxTools() []toolDef {
	return []toolDef{
		withHandler(tool("draft_get_sandbox_project_settings", "Get project sandbox lifecycle defaults.", map[string]any{"projectId": uintSchema("Project ID")}, "projectId"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argUint(args, "projectId")
			if e != nil {
				return nil, e
			}
			return c.GetSandboxProjectSettings(ctx, id)
		}),
		withHandler(tool("draft_save_sandbox_project_settings", "Save project sandbox TTL/warning/grace defaults.", map[string]any{"settings": map[string]any{"type": "object"}}, "settings"), func(ctx context.Context, args map[string]any) (any, error) {
			raw, ok := args["settings"].(map[string]any)
			if !ok {
				return nil, fmt.Errorf("settings must be an object")
			}
			var settings store.SandboxProjectSettings
			if e := decodeArgs(raw, &settings); e != nil {
				return nil, e
			}
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			return c.SaveSandboxProjectSettings(ctx, settings)
		}),
		withHandler(tool("draft_sandbox_source_repos", "Discover git repos/branches/PRs for sandbox create. Call before preview/create.", map[string]any{"sourceEnvironmentId": uintSchema("Source environment ID")}, "sourceEnvironmentId"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argUint(args, "sourceEnvironmentId")
			if e != nil {
				return nil, e
			}
			return c.ListSandboxSourceRepos(ctx, id)
		}),
		withHandler(tool("draft_resolve_sandbox_ref", "Resolve a git ref/SHA for a sandbox repository pin.", map[string]any{"repoRoot": stringsSchema("Absolute repo root"), "ref": stringsSchema("Branch or ref"), "commitSha": stringsSchema("Optional commit SHA")}, "repoRoot"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			root, e := argString(args, "repoRoot")
			if e != nil {
				return nil, e
			}
			return c.ResolveSandboxRef(ctx, root, optionalString(args, "ref"), optionalString(args, "commitSha"))
		}),
		withHandler(tool("draft_save_sandbox_profile", "Create or update a sandbox profile.", map[string]any{"profile": map[string]any{"type": "object"}}, "profile"), func(ctx context.Context, args map[string]any) (any, error) {
			raw, ok := args["profile"].(map[string]any)
			if !ok {
				return nil, fmt.Errorf("profile must be an object")
			}
			var profile store.SandboxProfile
			if e := decodeArgs(raw, &profile); e != nil {
				return nil, e
			}
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			return c.SaveSandboxProfile(ctx, profile)
		}),
		withHandler(tool("draft_delete_sandbox_profile", "Delete a sandbox profile.", map[string]any{"profileId": uintSchema("Profile ID"), "confirm": confirmSchema()}, "profileId", "confirm"), func(ctx context.Context, args map[string]any) (any, error) {
			if e := requireConfirm(args); e != nil {
				return nil, e
			}
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argUint(args, "profileId")
			if e != nil {
				return nil, e
			}
			return map[string]string{"status": "deleted"}, c.DeleteSandboxProfile(ctx, id)
		}),
		withHandler(tool("draft_preview_sandbox", "Preview a sandbox plan before creating it.", map[string]any{"request": map[string]any{"type": "object"}}, "request"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			raw, ok := args["request"].(map[string]any)
			if !ok {
				return nil, fmt.Errorf("request must be an object")
			}
			var r deploy.SandboxCreateRequest
			if e = decodeArgs(raw, &r); e != nil {
				return nil, e
			}
			return c.PreviewSandbox(ctx, r)
		}),
		withHandler(tool("draft_create_sandbox", "Create a sandbox using a frozen plan.", map[string]any{"request": map[string]any{"type": "object"}}, "request"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			raw, ok := args["request"].(map[string]any)
			if !ok {
				return nil, fmt.Errorf("request must be an object")
			}
			var r deploy.SandboxCreateRequest
			if e = decodeArgs(raw, &r); e != nil {
				return nil, e
			}
			return c.CreateSandbox(ctx, r)
		}),
		withHandler(tool("draft_get_sandbox", "Get sandbox detail.", map[string]any{"sandboxId": uintSchema("Sandbox ID")}, "sandboxId"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argUint(args, "sandboxId")
			if e != nil {
				return nil, e
			}
			return c.GetSandboxDetail(ctx, id)
		}),
		withHandler(tool("draft_suspend_sandbox", "Suspend a sandbox.", map[string]any{"sandboxId": uintSchema("Sandbox ID")}, "sandboxId"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argUint(args, "sandboxId")
			if e != nil {
				return nil, e
			}
			return c.SuspendSandbox(ctx, id)
		}),
		withHandler(tool("draft_resume_sandbox", "Resume a sandbox.", map[string]any{"sandboxId": uintSchema("Sandbox ID")}, "sandboxId"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argUint(args, "sandboxId")
			if e != nil {
				return nil, e
			}
			return c.ResumeSandbox(ctx, id)
		}),
		withHandler(tool("draft_extend_sandbox", "Extend a sandbox's TTL.", map[string]any{"sandboxId": uintSchema("Sandbox ID"), "ttlHours": uintSchema("Hours")}, "sandboxId", "ttlHours"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argUint(args, "sandboxId")
			if e != nil {
				return nil, e
			}
			h, e := argUint(args, "ttlHours")
			if e != nil {
				return nil, e
			}
			return c.ExtendSandbox(ctx, id, int(h))
		}),
		withHandler(tool("draft_refresh_sandbox", "Refresh sandbox sources by tip or recorded SHA.", map[string]any{"request": map[string]any{"type": "object"}}, "request"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			raw, ok := args["request"].(map[string]any)
			if !ok {
				return nil, fmt.Errorf("request must be an object")
			}
			var r deploy.SandboxRefreshRequest
			if e = decodeArgs(raw, &r); e != nil {
				return nil, e
			}
			return c.RefreshSandbox(ctx, r)
		}),
		withHandler(tool("draft_delete_sandbox", "Delete a sandbox and its Draft-managed resources.", map[string]any{"sandboxId": uintSchema("Sandbox ID"), "confirm": confirmSchema()}, "sandboxId", "confirm"), func(ctx context.Context, args map[string]any) (any, error) {
			if e := requireConfirm(args); e != nil {
				return nil, e
			}
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argUint(args, "sandboxId")
			if e != nil {
				return nil, e
			}
			return map[string]string{"status": "deleted"}, c.DeleteSandbox(ctx, id)
		}),
		withHandler(tool("draft_list_sandbox_test_runs", "List testing sandbox run history.", map[string]any{"projectId": uintSchema("Project ID"), "limit": uintSchema("Maximum results")}, "projectId"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argUint(args, "projectId")
			if e != nil {
				return nil, e
			}
			return c.ListSandboxTestRuns(ctx, id, int(optionalUint(args, "limit")))
		}),
		withHandler(tool("draft_get_sandbox_test_run", "Get a testing sandbox run result.", map[string]any{"runId": uintSchema("Test run ID")}, "runId"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argUint(args, "runId")
			if e != nil {
				return nil, e
			}
			return c.GetSandboxTestRun(ctx, id)
		}),
		withHandler(tool("draft_run_testing_sandbox", "Run a testing sandbox suite (mode fresh|steps). Pass full SandboxTestRunRequest as request.", map[string]any{"request": map[string]any{"type": "object"}}, "request"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			raw, ok := args["request"].(map[string]any)
			if !ok {
				return nil, fmt.Errorf("request must be an object")
			}
			var r deploy.SandboxTestRunRequest
			if e = decodeArgs(raw, &r); e != nil {
				return nil, e
			}
			return c.RunTestingSandbox(ctx, r)
		}),
	}
}
