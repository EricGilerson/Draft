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
		withHandler(tool("draft_duplicate_environment", "Duplicate an environment.", map[string]any{"sourceEnvironmentId": uintSchema("Source ID"), "newName": stringsSchema("New environment name"), "choices": map[string]any{"type": "array"}, "startAfter": map[string]any{"type": "boolean"}}, "sourceEnvironmentId", "newName"), func(ctx context.Context, args map[string]any) (any, error) {
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
			return c.DuplicateEnvironment(ctx, id, name, choices, optionalBool(args, "startAfter"))
		}),
		withHandler(tool("draft_preview_sync", "Preview configuration synchronization between environments/services. Secrets masked unless includeSecrets=true.", map[string]any{"request": map[string]any{"type": "object"}, "includeSecrets": map[string]any{"type": "boolean"}}, "request"), func(ctx context.Context, args map[string]any) (any, error) {
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
		withHandler(tool("draft_apply_sync", "Apply configuration synchronization.", map[string]any{"request": map[string]any{"type": "object"}, "mode": stringsSchema("stage or stageAndRedeploy"), "confirm": confirmSchema()}, "request", "mode", "confirm"), func(ctx context.Context, args map[string]any) (any, error) {
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
