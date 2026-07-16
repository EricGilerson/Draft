package draftmcp

import "context"

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
		withHandler(tool("draft_list_environments", "List project environments.", map[string]any{"projectId": uintSchema("Project ID")}, "projectId"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argUint(args, "projectId")
			if e != nil {
				return nil, e
			}
			return c.ListEnvironments(ctx, id)
		}),
		withHandler(tool("draft_create_environment", "Create an environment in a project.", map[string]any{"projectId": uintSchema("Project ID"), "name": stringsSchema("Environment name")}, "projectId", "name"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			projectID, e := argUint(args, "projectId")
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
		withHandler(tool("draft_list_nodes", "List service nodes in an environment.", map[string]any{"environmentId": uintSchema("Environment ID")}, "environmentId"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argUint(args, "environmentId")
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
		withHandler(tool("draft_list_templates", "List service templates.", map[string]any{}), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			return c.ListTemplates(ctx)
		}),
		withHandler(tool("draft_get_template", "Get a service template.", map[string]any{"templateId": uintSchema("Template ID")}, "templateId"), func(ctx context.Context, args map[string]any) (any, error) {
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
		withHandler(tool("draft_list_routes", "List routes, optionally for a project.", map[string]any{"projectId": uintSchema("Optional project ID")}), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id := optionalUint(args, "projectId")
			if id == 0 {
				return c.ListRoutes(ctx, nil)
			}
			return c.ListRoutes(ctx, &id)
		}),
		withHandler(tool("draft_list_sandboxes", "List sandboxes for a project.", map[string]any{"projectId": uintSchema("Project ID")}, "projectId"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argUint(args, "projectId")
			if e != nil {
				return nil, e
			}
			return c.ListSandboxes(ctx, id)
		}),
		withHandler(tool("draft_list_sandbox_profiles", "List sandbox profiles.", map[string]any{"projectId": uintSchema("Project ID")}, "projectId"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argUint(args, "projectId")
			if e != nil {
				return nil, e
			}
			return c.ListSandboxProfiles(ctx, id)
		}),
		withHandler(tool("draft_project_summary", "Summarize project services across environments.", map[string]any{"projectId": uintSchema("Project ID")}, "projectId"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argUint(args, "projectId")
			if e != nil {
				return nil, e
			}
			return c.ListProjectServicesSummary(ctx, id)
		}),
	}
}
