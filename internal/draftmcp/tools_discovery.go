package draftmcp

import (
	"context"

	"Draft/internal/store"
)

func discoveryTools() []toolDef {
	return []toolDef{
		withHandler(tool("draft_list_projects", "List Draft projects.", map[string]any{}), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			return c.ListProjects(ctx)
		}),
		withHandler(tool("draft_create_project", "Create a Draft project.", map[string]any{"name": stringsSchema("Project name"), "path": stringsSchema("Project folder path"), "description": stringsSchema("Optional description")}, "name", "path"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			n, e := argString(args, "name")
			if e != nil {
				return nil, e
			}
			return c.CreateProject(ctx, n, optionalString(args, "path"), optionalString(args, "description"))
		}),
		withHandler(tool("draft_list_environments", "List project environments. projectId optional if draft_set_context was used.", map[string]any{"projectId": uintSchema("Project ID (or session context)")}), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := resolveProjectID(args)
			if e != nil {
				return nil, e
			}
			return c.ListEnvironments(ctx, id)
		}),
		withHandler(tool("draft_create_environment", "Create an environment in a project.", map[string]any{"projectId": uintSchema("Project ID (or session context)"), "name": stringsSchema("Environment name")}, "name"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			projectID, e := resolveProjectID(args)
			if e != nil {
				return nil, e
			}
			name, e := argString(args, "name")
			if e != nil {
				return nil, e
			}
			return c.CreateEnvironment(ctx, projectID, name)
		}),
		withHandler(tool("draft_rename_environment", "Rename an environment display name.", map[string]any{"environmentId": uintSchema("Environment ID"), "name": stringsSchema("New display name")}, "environmentId", "name"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argUint(args, "environmentId")
			if e != nil {
				return nil, e
			}
			name, e := argString(args, "name")
			if e != nil {
				return nil, e
			}
			return map[string]string{"status": "renamed"}, c.RenameEnvironment(ctx, id, name)
		}),
		withHandler(tool("draft_set_default_environment", "Set a project's default environment.", map[string]any{"environmentId": uintSchema("Environment ID")}, "environmentId"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argUint(args, "environmentId")
			if e != nil {
				return nil, e
			}
			return map[string]string{"status": "updated"}, c.SetDefaultEnvironment(ctx, id)
		}),
		withHandler(tool("draft_list_nodes", "List service nodes in an environment. environmentId optional if context set.", map[string]any{"environmentId": uintSchema("Environment ID (or session context)")}), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := resolveEnvironmentID(args)
			if e != nil {
				return nil, e
			}
			return c.ListNodes(ctx, id)
		}),
		withHandler(tool("draft_get_node", "Get a service node.", map[string]any{"nodeId": stringsSchema("Node ID")}, "nodeId"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argString(args, "nodeId")
			if e != nil {
				return nil, e
			}
			return c.GetNode(ctx, id)
		}),
		withHandler(tool("draft_get_node_settings", "Get applied settings for a service node.", map[string]any{"nodeId": stringsSchema("Node ID")}, "nodeId"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argString(args, "nodeId")
			if e != nil {
				return nil, e
			}
			return c.GetNodeSettings(ctx, id)
		}),
		withHandler(tool("draft_list_templates", "List service templates. Compact by default (no dockerfile/schema). Pass detail=true for full rows.", map[string]any{"detail": map[string]any{"type": "boolean", "description": "Include dockerfile, schema, envVars, etc."}}), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			templates, e := c.ListTemplates(ctx)
			if e != nil {
				return nil, e
			}
			if optionalBool(args, "detail") {
				return templates, nil
			}
			return compactTemplates(templates), nil
		}),
		withHandler(tool("draft_get_template", "Get a full service template (including dockerfile/schema).", map[string]any{"templateId": uintSchema("Template ID")}, "templateId"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argUint(args, "templateId")
			if e != nil {
				return nil, e
			}
			return c.GetTemplate(ctx, id)
		}),
		withHandler(tool("draft_list_routes", "List routes, optionally for a project.", map[string]any{"projectId": uintSchema("Optional project ID (or session context)")}), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id := optionalUint(args, "projectId")
			if id == 0 {
				id = getSessionContext().ProjectID
			}
			if id == 0 {
				return c.ListRoutes(ctx, nil)
			}
			return c.ListRoutes(ctx, &id)
		}),
		withHandler(tool("draft_list_sandboxes", "List sandboxes for a project.", map[string]any{"projectId": uintSchema("Project ID (or session context)")}), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := resolveProjectID(args)
			if e != nil {
				return nil, e
			}
			return c.ListSandboxes(ctx, id)
		}),
		withHandler(tool("draft_list_sandbox_profiles", "List sandbox profiles.", map[string]any{"projectId": uintSchema("Project ID (or session context)")}), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := resolveProjectID(args)
			if e != nil {
				return nil, e
			}
			return c.ListSandboxProfiles(ctx, id)
		}),
		withHandler(tool("draft_project_summary", "Summarize project services across environments.", map[string]any{"projectId": uintSchema("Project ID (or session context)")}), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := resolveProjectID(args)
			if e != nil {
				return nil, e
			}
			return c.ListProjectServicesSummary(ctx, id)
		}),
	}
}

type templateSummary struct {
	ID          uint   `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Icon        string `json:"icon,omitempty"`
	Mode        string `json:"mode"`
	Image       string `json:"image,omitempty"`
	Port        int    `json:"port"`
	Builtin     bool   `json:"builtin"`
}

func compactTemplates(templates []store.ServiceTemplate) []templateSummary {
	out := make([]templateSummary, 0, len(templates))
	for _, t := range templates {
		out = append(out, templateSummary{
			ID: t.ID, Name: t.Name, Description: t.Description, Category: t.Category,
			Icon: t.Icon, Mode: t.Mode, Image: t.Image, Port: t.Port, Builtin: t.Builtin,
		})
	}
	return out
}
