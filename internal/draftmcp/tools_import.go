package draftmcp

import (
	"context"

	"Draft/internal/deploy"
)

func importTools() []toolDef {
	return []toolDef{
		withHandler(tool("draft_preview_config_import", "Preview a cloud configuration import (compose/cloudrun/ecs/containerapps).", map[string]any{"path": stringsSchema("Config file path")}, "path"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			p, e := argString(args, "path")
			if e != nil {
				return nil, e
			}
			return c.ImportConfigPreview(ctx, p)
		}),
		withHandler(tool("draft_import_config_as_project", "Import cloud configuration as a new project.", map[string]any{"path": stringsSchema("Config file path"), "projectName": stringsSchema("Project name")}, "path", "projectName"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			p, e := argString(args, "path")
			if e != nil {
				return nil, e
			}
			n, e := argString(args, "projectName")
			if e != nil {
				return nil, e
			}
			return c.ImportConfigAsProject(ctx, p, n)
		}),
		withHandler(tool("draft_import_config_into_project", "Import cloud configuration into an existing project/environment.", map[string]any{"projectId": uintSchema("Project ID"), "environmentId": uintSchema("Environment ID"), "path": stringsSchema("Config path"), "x": map[string]any{"type": "number"}, "y": map[string]any{"type": "number"}}, "projectId", "environmentId", "path"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			pid, e := argUint(args, "projectId")
			if e != nil {
				return nil, e
			}
			eid, e := argUint(args, "environmentId")
			if e != nil {
				return nil, e
			}
			p, e := argString(args, "path")
			if e != nil {
				return nil, e
			}
			x, _ := args["x"].(float64)
			y, _ := args["y"].(float64)
			return c.ImportConfigIntoProject(ctx, pid, eid, p, x, y)
		}),
		withHandler(tool("draft_export_config", "Export a service to a cloud format.", map[string]any{"nodeId": stringsSchema("Node ID"), "format": stringsSchema("compose, cloudrun, ecs, or containerapps")}, "nodeId", "format"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			n, e := argString(args, "nodeId")
			if e != nil {
				return nil, e
			}
			f, e := argString(args, "format")
			if e != nil {
				return nil, e
			}
			return c.ExportConfig(ctx, n, f)
		}),
		withHandler(tool("draft_export_project_config", "Export a project's default stack to a cloud format.", map[string]any{"projectId": uintSchema("Project ID"), "format": stringsSchema("Output format")}, "projectId", "format"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argUint(args, "projectId")
			if e != nil {
				return nil, e
			}
			f, e := argString(args, "format")
			if e != nil {
				return nil, e
			}
			return c.ExportProjectConfig(ctx, id, f)
		}),
		withHandler(tool("draft_export_config_to_path", "Export a service cloud config to a directory.", map[string]any{"nodeId": stringsSchema("Node ID"), "format": stringsSchema("Format"), "destDir": stringsSchema("Destination directory")}, "nodeId", "format", "destDir"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			n, e := argString(args, "nodeId")
			if e != nil {
				return nil, e
			}
			f, e := argString(args, "format")
			if e != nil {
				return nil, e
			}
			d, e := argString(args, "destDir")
			if e != nil {
				return nil, e
			}
			return c.ExportConfigToPath(ctx, n, f, d)
		}),
		withHandler(tool("draft_export_draftpack_service", "Export a .draftpack for one service. opts optional; defaults are safe cross-machine.", map[string]any{"nodeId": stringsSchema("Node ID"), "opts": map[string]any{"type": "object"}}, "nodeId"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			n, e := argString(args, "nodeId")
			if e != nil {
				return nil, e
			}
			opts := deploy.DefaultDraftPackExportOptions()
			if raw, ok := args["opts"].(map[string]any); ok {
				_ = decodeArgs(raw, &opts)
			}
			return c.ExportDraftPackService(ctx, n, opts)
		}),
		withHandler(tool("draft_export_draftpack_environment", "Export a .draftpack for an environment.", map[string]any{"environmentId": uintSchema("Environment ID"), "opts": map[string]any{"type": "object"}}, "environmentId"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argUint(args, "environmentId")
			if e != nil {
				return nil, e
			}
			opts := deploy.DefaultDraftPackExportOptions()
			if raw, ok := args["opts"].(map[string]any); ok {
				_ = decodeArgs(raw, &opts)
			}
			return c.ExportDraftPackEnvironment(ctx, id, opts)
		}),
		withHandler(tool("draft_export_draftpack_project", "Export a .draftpack for a whole project.", map[string]any{"projectId": uintSchema("Project ID"), "opts": map[string]any{"type": "object"}}, "projectId"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			id, e := argUint(args, "projectId")
			if e != nil {
				return nil, e
			}
			opts := deploy.DefaultDraftPackExportOptions()
			if raw, ok := args["opts"].(map[string]any); ok {
				_ = decodeArgs(raw, &opts)
			}
			return c.ExportDraftPackProject(ctx, id, opts)
		}),
		withHandler(tool("draft_preview_draftpack_import", "Preview importing a .draftpack from a file path.", map[string]any{"path": stringsSchema("Pack file path"), "opts": map[string]any{"type": "object"}}, "path"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			p, e := argString(args, "path")
			if e != nil {
				return nil, e
			}
			var opts deploy.DraftPackPreviewOptions
			if raw, ok := args["opts"].(map[string]any); ok {
				_ = decodeArgs(raw, &opts)
			}
			return c.PreviewDraftPackImport(ctx, p, opts)
		}),
		withHandler(tool("draft_import_draftpack", "Import a .draftpack from a file path. Destructive placement — use preview first; confirm required.", map[string]any{"path": stringsSchema("Pack file path"), "opts": map[string]any{"type": "object"}, "confirm": confirmSchema()}, "path", "confirm"), func(ctx context.Context, args map[string]any) (any, error) {
			if e := requireConfirm(args); e != nil {
				return nil, e
			}
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			p, e := argString(args, "path")
			if e != nil {
				return nil, e
			}
			var opts deploy.DraftPackImportOptions
			if raw, ok := args["opts"].(map[string]any); ok {
				_ = decodeArgs(raw, &opts)
			}
			return c.ImportDraftPack(ctx, p, opts)
		}),
		withHandler(tool("draft_preview_draftpack_json", "Preview importing a .draftpack from JSON text.", map[string]any{"jsonText": stringsSchema("Pack JSON"), "opts": map[string]any{"type": "object"}}, "jsonText"), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			text, e := argString(args, "jsonText")
			if e != nil {
				return nil, e
			}
			var opts deploy.DraftPackPreviewOptions
			if raw, ok := args["opts"].(map[string]any); ok {
				_ = decodeArgs(raw, &opts)
			}
			return c.PreviewDraftPackJSON(ctx, text, opts)
		}),
		withHandler(tool("draft_import_draftpack_json", "Import a .draftpack from JSON text. confirm required.", map[string]any{"jsonText": stringsSchema("Pack JSON"), "opts": map[string]any{"type": "object"}, "confirm": confirmSchema()}, "jsonText", "confirm"), func(ctx context.Context, args map[string]any) (any, error) {
			if e := requireConfirm(args); e != nil {
				return nil, e
			}
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			text, e := argString(args, "jsonText")
			if e != nil {
				return nil, e
			}
			var opts deploy.DraftPackImportOptions
			if raw, ok := args["opts"].(map[string]any); ok {
				_ = decodeArgs(raw, &opts)
			}
			return c.ImportDraftPackJSON(ctx, text, opts)
		}),
	}
}
