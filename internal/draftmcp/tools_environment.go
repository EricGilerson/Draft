package draftmcp

import (
	"Draft/internal/deploy"
	"context"
	"fmt"
)

func environmentTools() []toolDef {
	return []toolDef{
		withHandler(tool("draft_run_environment_stack", "Start, stop, or redeploy every service in an environment.", map[string]any{"environmentId": uintSchema("Environment ID"), "action": stringsSchema("start, stop, or redeploy")}, "environmentId", "action"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argUint(args, "environmentId")
			if e != nil {
				return nil, e
			}
			a, e := argString(args, "action")
			if e != nil {
				return nil, e
			}
			return c.RunEnvironmentStack(ctx, id, a)
		}),
		withHandler(tool("draft_preview_duplicate_environment", "Preview stateful services before duplicating.", map[string]any{"environmentId": uintSchema("Source environment ID")}, "environmentId"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argUint(args, "environmentId")
			if e != nil {
				return nil, e
			}
			return c.PreviewEnvironmentDuplicate(ctx, id)
		}),
		withHandler(tool("draft_duplicate_environment", "Duplicate an environment. Optional repositories stamps git_branch on copied services (branch/PR per repo); omit to keep source pins. Shared aliases are not pinned.", map[string]any{"sourceEnvironmentId": uintSchema("Source ID"), "newName": stringsSchema("New environment name"), "choices": map[string]any{"type": "array"}, "startAfter": map[string]any{"type": "boolean"}, "repositories": map[string]any{"type": "array", "description": "Optional SandboxRepositoryRef pins (repoRoot, ref, commitSha?, prNumber?)"}}, "sourceEnvironmentId", "newName"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argUint(args, "sourceEnvironmentId")
			if e != nil {
				return nil, e
			}
			name, e := argString(args, "newName")
			if e != nil {
				return nil, e
			}
			var choices []deploy.ServiceDataChoice
			if e = decodeArgs(map[string]any{"choices": args["choices"]}, &struct {
				Choices *[]deploy.ServiceDataChoice `json:"choices"`
			}{&choices}); e != nil {
				return nil, e
			}
			var repositories []deploy.SandboxRepositoryRef
			if raw, ok := args["repositories"]; ok && raw != nil {
				if e = decodeArgs(map[string]any{"repositories": raw}, &struct {
					Repositories *[]deploy.SandboxRepositoryRef `json:"repositories"`
				}{&repositories}); e != nil {
					return nil, e
				}
			}
			return c.DuplicateEnvironment(ctx, id, name, choices, optionalBool(args, "startAfter"), repositories)
		}),
		withHandler(tool("draft_preview_sync", "Preview configuration sync between environments/services. Set createMissing=true to plan adding source-only services onto the target (clone + restamp). Does not delete target-only services. Secrets masked unless includeSecrets=true.", map[string]any{"request": map[string]any{"type": "object"}, "includeSecrets": map[string]any{"type": "boolean"}}, "request"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			raw, ok := args["request"].(map[string]any)
			if !ok {
				return nil, fmt.Errorf("request must be an object")
			}
			var r deploy.SyncRequest
			if e = decodeArgs(raw, &r); e != nil {
				return nil, e
			}
			v, e := c.PreviewSync(ctx, r)
			return redactSecrets(v, optionalBool(args, "includeSecrets")), e
		}),
		withHandler(tool("draft_apply_sync", "Apply configuration sync (stage or stageAndRedeploy). With createMissing, clones missing source services into the target with restamped identity.", map[string]any{"request": map[string]any{"type": "object"}, "mode": stringsSchema("stage or stageAndRedeploy"), "confirm": confirmSchema()}, "request", "mode", "confirm"), func(ctx context.Context, args map[string]any) (any, error) {
			if e := requireConfirm(args); e != nil {
				return nil, e
			}
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			var r deploy.SyncRequest
			raw, ok := args["request"].(map[string]any)
			if !ok {
				return nil, fmt.Errorf("request must be an object")
			}
			if e = decodeArgs(raw, &r); e != nil {
				return nil, e
			}
			mode, e := argString(args, "mode")
			if e != nil {
				return nil, e
			}
			return c.ApplySync(ctx, r, mode)
		}),
		withHandler(tool("draft_delete_environment", "Delete an environment.", map[string]any{"environmentId": uintSchema("Environment ID"), "confirm": confirmSchema()}, "environmentId", "confirm"), func(ctx context.Context, args map[string]any) (any, error) {
			if e := requireConfirm(args); e != nil {
				return nil, e
			}
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argUint(args, "environmentId")
			if e != nil {
				return nil, e
			}
			return map[string]string{"status": "deleted"}, c.DeleteEnvironment(ctx, id)
		}),
	}
}
