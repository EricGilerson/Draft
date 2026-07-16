package draftmcp

import "context"

func envTools() []toolDef {
	return []toolDef{
		withHandler(tool("draft_list_env_vars", "List service environment variables; secrets are masked by default.", map[string]any{"nodeId": stringsSchema("Node ID"), "includeSecrets": map[string]any{"type": "boolean"}}, "nodeId"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argString(args, "nodeId")
			if e != nil {
				return nil, e
			}
			v, e := c.GetEnvVars(ctx, id)
			return redactSecrets(v, optionalBool(args, "includeSecrets")), e
		}),
		withHandler(tool("draft_set_env_var", "Set an applied environment variable.", map[string]any{"nodeId": stringsSchema("Node ID"), "key": stringsSchema("Key"), "value": stringsSchema("Value")}, "nodeId", "key", "value"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			n, e := argString(args, "nodeId")
			if e != nil {
				return nil, e
			}
			k, e := argString(args, "key")
			if e != nil {
				return nil, e
			}
			v, e := argString(args, "value")
			if e != nil {
				return nil, e
			}
			return map[string]string{"status": "updated"}, c.SetEnvVar(ctx, n, k, v)
		}),
		withHandler(tool("draft_delete_env_var", "Delete a service environment variable.", map[string]any{"nodeId": stringsSchema("Node ID"), "key": stringsSchema("Key"), "confirm": confirmSchema()}, "nodeId", "key", "confirm"), func(ctx context.Context, args map[string]any) (any, error) {
			if e := requireConfirm(args); e != nil {
				return nil, e
			}
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			n, e := argString(args, "nodeId")
			if e != nil {
				return nil, e
			}
			k, e := argString(args, "key")
			if e != nil {
				return nil, e
			}
			return map[string]string{"status": "deleted"}, c.DeleteEnvVar(ctx, n, k)
		}),
		withHandler(tool("draft_preview_env_vars", "Preview resolved runtime environment; secrets are masked by default.", map[string]any{"nodeId": stringsSchema("Node ID"), "includeSecrets": map[string]any{"type": "boolean"}}, "nodeId"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			n, e := argString(args, "nodeId")
			if e != nil {
				return nil, e
			}
			v, e := c.PreviewEnvVars(ctx, n)
			return redactSecrets(v, optionalBool(args, "includeSecrets")), e
		}),
		withHandler(tool("draft_import_env_file", "Import a .env file.", map[string]any{"nodeId": stringsSchema("Node ID"), "path": stringsSchema("Path")}, "nodeId", "path"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			n, e := argString(args, "nodeId")
			if e != nil {
				return nil, e
			}
			p, e := argString(args, "path")
			if e != nil {
				return nil, e
			}
			return c.ImportEnvFile(ctx, n, p)
		}),
		withHandler(tool("draft_export_env_file", "Export service variables to its configured .env file.", map[string]any{"nodeId": stringsSchema("Node ID")}, "nodeId"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			n, e := argString(args, "nodeId")
			if e != nil {
				return nil, e
			}
			return c.ExportEnvFile(ctx, n)
		}),
		withHandler(tool("draft_refresh_env_file", "Refresh variables from a configured .env file.", map[string]any{"nodeId": stringsSchema("Node ID")}, "nodeId"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			n, e := argString(args, "nodeId")
			if e != nil {
				return nil, e
			}
			return c.RefreshEnvFile(ctx, n)
		}),
		withHandler(tool("draft_suggest_env_file", "Suggest a .env path.", map[string]any{"nodeId": stringsSchema("Node ID"), "projectId": uintSchema("Project ID")}, "nodeId", "projectId"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			n, e := argString(args, "nodeId")
			if e != nil {
				return nil, e
			}
			p, e := argUint(args, "projectId")
			if e != nil {
				return nil, e
			}
			return c.SuggestEnvFile(ctx, n, p)
		}),
		withHandler(tool("draft_list_reference_targets", "List valid @{Service.ATTR} targets.", map[string]any{"nodeId": stringsSchema("Node ID")}, "nodeId"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			n, e := argString(args, "nodeId")
			if e != nil {
				return nil, e
			}
			return c.ListReferenceTargets(ctx, n)
		}),
		withHandler(tool("draft_list_reference_issues", "List broken environment references.", map[string]any{"nodeId": stringsSchema("Node ID")}, "nodeId"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			n, e := argString(args, "nodeId")
			if e != nil {
				return nil, e
			}
			return c.ListReferenceIssues(ctx, n)
		}),
		withHandler(tool("draft_environment_connections", "List service environment connections ( @{Service} edges).", map[string]any{"environmentId": uintSchema("Environment ID")}, "environmentId"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argUint(args, "environmentId")
			if e != nil {
				return nil, e
			}
			return c.GetEnvironmentConnections(ctx, id)
		}),
		withHandler(tool("draft_list_secrets", "List app-wide secrets; values masked unless includeSecrets=true.", map[string]any{"includeSecrets": map[string]any{"type": "boolean"}}), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			v, e := c.ListAppSecrets(ctx)
			return redactSecrets(v, optionalBool(args, "includeSecrets")), e
		}),
		withHandler(tool("draft_set_secret", "Create or update an app-wide secret ({{secret.KEY}}).", map[string]any{"key": stringsSchema("Secret key"), "value": stringsSchema("Secret value"), "description": stringsSchema("Optional description")}, "key", "value"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			k, e := argString(args, "key")
			if e != nil {
				return nil, e
			}
			v, e := argString(args, "value")
			if e != nil {
				return nil, e
			}
			return map[string]string{"status": "ok"}, c.SetAppSecret(ctx, k, v, optionalString(args, "description"))
		}),
		withHandler(tool("draft_delete_secret", "Delete an app-wide secret.", map[string]any{"key": stringsSchema("Secret key"), "confirm": confirmSchema()}, "key", "confirm"), func(ctx context.Context, args map[string]any) (any, error) {
			if e := requireConfirm(args); e != nil {
				return nil, e
			}
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			k, e := argString(args, "key")
			if e != nil {
				return nil, e
			}
			return map[string]string{"status": "deleted"}, c.DeleteAppSecret(ctx, k)
		}),
		withHandler(tool("draft_secret_usages", "List services referencing {{secret.KEY}}.", map[string]any{"key": stringsSchema("Secret key")}, "key"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			k, e := argString(args, "key")
			if e != nil {
				return nil, e
			}
			return c.ListAppSecretUsages(ctx, k)
		}),
		withHandler(tool("draft_list_project_env", "List project-scoped values ({{project.KEY}}); secret-flagged values masked by default.", map[string]any{"projectId": uintSchema("Project ID"), "includeSecrets": map[string]any{"type": "boolean"}}, "projectId"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argUint(args, "projectId")
			if e != nil {
				return nil, e
			}
			v, e := c.ListProjectEnvVars(ctx, id)
			return redactSecrets(v, optionalBool(args, "includeSecrets")), e
		}),
		withHandler(tool("draft_set_project_env", "Set a project-scoped value.", map[string]any{"projectId": uintSchema("Project ID"), "key": stringsSchema("Key"), "value": stringsSchema("Value"), "scope": stringsSchema("Ignored for project values; pass empty or both"), "secret": map[string]any{"type": "boolean"}}, "projectId", "key", "value"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argUint(args, "projectId")
			if e != nil {
				return nil, e
			}
			k, e := argString(args, "key")
			if e != nil {
				return nil, e
			}
			v, e := argString(args, "value")
			if e != nil {
				return nil, e
			}
			return map[string]string{"status": "ok"}, c.SetProjectEnvVar(ctx, id, k, v, optionalString(args, "scope"), optionalBool(args, "secret"))
		}),
		withHandler(tool("draft_delete_project_env", "Delete a project-scoped value.", map[string]any{"projectId": uintSchema("Project ID"), "key": stringsSchema("Key"), "confirm": confirmSchema()}, "projectId", "key", "confirm"), func(ctx context.Context, args map[string]any) (any, error) {
			if e := requireConfirm(args); e != nil {
				return nil, e
			}
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argUint(args, "projectId")
			if e != nil {
				return nil, e
			}
			k, e := argString(args, "key")
			if e != nil {
				return nil, e
			}
			return map[string]string{"status": "deleted"}, c.DeleteProjectEnvVar(ctx, id, k)
		}),
		withHandler(tool("draft_project_env_usages", "List services referencing {{project.KEY}}.", map[string]any{"projectId": uintSchema("Project ID"), "key": stringsSchema("Key")}, "projectId", "key"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argUint(args, "projectId")
			if e != nil {
				return nil, e
			}
			k, e := argString(args, "key")
			if e != nil {
				return nil, e
			}
			return c.ListProjectEnvVarUsages(ctx, id, k)
		}),
	}
}
