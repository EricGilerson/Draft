package draftmcp

import (
	"context"
	"fmt"
	"time"

	"Draft/internal/deploy"
	"Draft/internal/store"
)

func nodeTool(name, description string, action func(context.Context, string) error) toolDef {
	return withHandler(tool(name, description, map[string]any{"nodeId": stringsSchema("Node ID")}, "nodeId"), func(ctx context.Context, args map[string]any) (any, error) {
		id, e := argString(args, "nodeId")
		if e != nil {
			return nil, e
		}
		return map[string]string{"status": "ok"}, action(ctx, id)
	})
}

func serviceTools() []toolDef {
	return []toolDef{
		withHandler(tool("draft_create_blank_service", "Create a blank service node (no template). Stage image/dockerfile + service_port, then deploy.", map[string]any{
			"id":            stringsSchema("Optional stable node ID"),
			"label":         stringsSchema("Service label"),
			"projectId":     uintSchema("Project ID (or session context)"),
			"environmentId": uintSchema("Environment ID (or session context)"),
			"x":             map[string]any{"type": "number"},
			"y":             map[string]any{"type": "number"},
		}, "label"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			label, e := argString(args, "label")
			if e != nil {
				return nil, e
			}
			pid, e := resolveProjectID(args)
			if e != nil {
				return nil, e
			}
			eid, e := resolveEnvironmentID(args)
			if e != nil {
				return nil, e
			}
			id := optionalString(args, "id")
			if id == "" {
				id = fmt.Sprintf("node-%d", time.Now().UnixNano())
			}
			x, _ := args["x"].(float64)
			y, _ := args["y"].(float64)
			return c.CreateNode(ctx, id, label, pid, eid, x, y)
		}),
		withHandler(tool("draft_create_service", "Create a service from a template. projectId/environmentId optional if context set.", map[string]any{"id": stringsSchema("Optional stable node ID"), "projectId": uintSchema("Project ID (or session context)"), "environmentId": uintSchema("Environment ID (or session context)"), "templateId": uintSchema("Template ID"), "label": stringsSchema("Service label"), "x": map[string]any{"type": "number"}, "y": map[string]any{"type": "number"}}, "templateId", "label"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			var req deploy.CreateNodeFromTemplateRequest
			if e = decodeArgs(args, &req); e != nil {
				return nil, e
			}
			if req.ProjectID == 0 {
				pid, e := resolveProjectID(args)
				if e != nil {
					return nil, e
				}
				req.ProjectID = pid
			}
			if req.EnvironmentID == 0 {
				eid, e := resolveEnvironmentID(args)
				if e != nil {
					return nil, e
				}
				req.EnvironmentID = eid
			}
			if req.ID == "" {
				req.ID = fmt.Sprintf("node-%d", time.Now().UnixNano())
			}
			return c.CreateNodeFromTemplate(ctx, req)
		}),
		nodeTool("draft_deploy_service", "Deploy a service.", func(ctx context.Context, id string) error {
			c, e := getClient(ctx)
			if e != nil {
				return e
			}
			return c.Deploy(ctx, id)
		}),
		nodeTool("draft_stop_service", "Stop a service.", func(ctx context.Context, id string) error {
			c, e := getClient(ctx)
			if e != nil {
				return e
			}
			return c.Stop(ctx, id)
		}),
		nodeTool("draft_restart_service", "Restart a service.", func(ctx context.Context, id string) error {
			c, e := getClient(ctx)
			if e != nil {
				return e
			}
			return c.Restart(ctx, id)
		}),
		withHandler(tool("draft_reapply_template", "Reapply a node's template while preserving user environment overrides.", map[string]any{"nodeId": stringsSchema("Node ID")}, "nodeId"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argString(args, "nodeId")
			if e != nil {
				return nil, e
			}
			return c.ReapplyTemplate(ctx, id)
		}),
		withHandler(tool("draft_service_config_status", "Get applied and staged service configuration status.", map[string]any{"nodeId": stringsSchema("Node ID")}, "nodeId"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argString(args, "nodeId")
			if e != nil {
				return nil, e
			}
			return c.GetNodeConfigStatus(ctx, id)
		}),
		withHandler(tool("draft_stage_service_settings", "Stage settings for the next successful deploy (or apply immediate keys like git_branch).", map[string]any{"nodeId": stringsSchema("Node ID"), "projectId": uintSchema("Project ID (required when staging service_root)"), "settings": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}}}, "nodeId", "projectId", "settings"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argString(args, "nodeId")
			if e != nil {
				return nil, e
			}
			pid, e := argUint(args, "projectId")
			if e != nil {
				return nil, e
			}
			settings, e := argStringMap(args, "settings")
			if e != nil {
				return nil, e
			}
			return map[string]string{"status": "staged"}, c.StageNodeSettings(ctx, id, pid, settings)
		}),
		withHandler(tool("draft_stage_env", "Stage env var upserts/deletes for the next successful deploy. Prefer this over draft_set_env_var for pending changes.", map[string]any{
			"nodeId":     stringsSchema("Node ID"),
			"upserts":    map[string]any{"type": "array", "description": "Objects with key, value, scope (runtime|build|both), optional source/envFile"},
			"deleteKeys": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		}, "nodeId"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argString(args, "nodeId")
			if e != nil {
				return nil, e
			}
			var body struct {
				Upserts    []store.EnvVarStageUpsert `json:"upserts"`
				DeleteKeys []string                  `json:"deleteKeys"`
			}
			if e = decodeArgs(args, &body); e != nil {
				return nil, e
			}
			return map[string]string{"status": "staged"}, c.StageEnvVarChanges(ctx, id, body.Upserts, body.DeleteKeys)
		}),
		withHandler(tool("draft_preview_staged_changes", "Preview effective changes before deployment.", map[string]any{"nodeId": stringsSchema("Node ID"), "proposedSettings": map[string]any{"type": "object"}}, "nodeId"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argString(args, "nodeId")
			if e != nil {
				return nil, e
			}
			settings, _ := argStringMap(args, "proposedSettings")
			return c.PreviewStagedChanges(ctx, id, settings)
		}),
		withHandler(tool("draft_discard_staged_changes", "Discard pending service configuration.", map[string]any{"nodeId": stringsSchema("Node ID"), "confirm": confirmSchema()}, "nodeId", "confirm"), func(ctx context.Context, args map[string]any) (any, error) {
			if e := requireConfirm(args); e != nil {
				return nil, e
			}
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argString(args, "nodeId")
			if e != nil {
				return nil, e
			}
			return map[string]string{"status": "discarded"}, c.DiscardStagedChanges(ctx, id)
		}),
		withHandler(tool("draft_preview_delete_service", "Preview consequences of deleting a service.", map[string]any{"nodeId": stringsSchema("Node ID")}, "nodeId"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argString(args, "nodeId")
			if e != nil {
				return nil, e
			}
			return c.PreviewDeleteService(ctx, id)
		}),
		withHandler(tool("draft_delete_service", "Delete a service.", map[string]any{"nodeId": stringsSchema("Node ID"), "confirm": confirmSchema()}, "nodeId", "confirm"), func(ctx context.Context, args map[string]any) (any, error) {
			if e := requireConfirm(args); e != nil {
				return nil, e
			}
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argString(args, "nodeId")
			if e != nil {
				return nil, e
			}
			return map[string]string{"status": "deleted"}, c.DeleteService(ctx, id)
		}),
		withHandler(tool("draft_list_deployments", "List a service's deployments.", map[string]any{"nodeId": stringsSchema("Node ID")}, "nodeId"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argString(args, "nodeId")
			if e != nil {
				return nil, e
			}
			return c.GetDeployments(ctx, id)
		}),
		withHandler(tool("draft_active_deployment", "Get the active deployment for a service.", map[string]any{"nodeId": stringsSchema("Node ID")}, "nodeId"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argString(args, "nodeId")
			if e != nil {
				return nil, e
			}
			return c.GetActiveDeployment(ctx, id)
		}),
		withHandler(tool("draft_get_build_log", "Get a deployment build log (truncated to 32KB).", map[string]any{"deploymentId": uintSchema("Deployment ID")}, "deploymentId"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argUint(args, "deploymentId")
			if e != nil {
				return nil, e
			}
			log, err := c.GetBuildLog(ctx, id)
			if err != nil {
				return nil, err
			}
			return map[string]string{"log": truncate(log)}, nil
		}),
		withHandler(tool("draft_rollback_eligibility", "List which deployments can be rolled back (retained images).", map[string]any{"nodeId": stringsSchema("Node ID")}, "nodeId"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argString(args, "nodeId")
			if e != nil {
				return nil, e
			}
			return c.RollbackEligibility(ctx, id)
		}),
		withHandler(tool("draft_service_health", "Get lightweight service health.", map[string]any{"nodeId": stringsSchema("Node ID")}, "nodeId"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argString(args, "nodeId")
			if e != nil {
				return nil, e
			}
			return c.GetNodeHealth(ctx, id)
		}),
		withHandler(tool("draft_service_metrics", "Get Docker service metrics.", map[string]any{"nodeId": stringsSchema("Node ID")}, "nodeId"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argString(args, "nodeId")
			if e != nil {
				return nil, e
			}
			return c.GetServiceMetrics(ctx, id)
		}),
		withHandler(tool("draft_rollback_service", "Roll back to a retained deployment image.", map[string]any{"deploymentId": uintSchema("Deployment ID"), "confirm": confirmSchema()}, "deploymentId", "confirm"), func(ctx context.Context, args map[string]any) (any, error) {
			if e := requireConfirm(args); e != nil {
				return nil, e
			}
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argUint(args, "deploymentId")
			if e != nil {
				return nil, e
			}
			return map[string]string{"status": "rollback started"}, c.RollbackDeployment(ctx, id)
		}),
	}
}
