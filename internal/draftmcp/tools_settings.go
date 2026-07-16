package draftmcp

import (
	"context"
	"fmt"

	"Draft/internal/daemon"
)

func settingsTools() []toolDef {
	return []toolDef{
		withHandler(tool("draft_get_app_settings", "Get Draft application settings.", map[string]any{}), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			return c.GetAppSettings(ctx)
		}),
		withHandler(tool("draft_set_app_settings", "Replace Draft application settings.", map[string]any{"settings": map[string]any{"type": "object"}, "confirm": confirmSchema()}, "settings", "confirm"), func(ctx context.Context, args map[string]any) (any, error) {
			if e := requireConfirm(args); e != nil {
				return nil, e
			}
			raw, ok := args["settings"].(map[string]any)
			if !ok {
				return nil, fmt.Errorf("settings must be an object")
			}
			var settings daemon.AppSettings
			if e := decodeArgs(raw, &settings); e != nil {
				return nil, e
			}
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			return c.SetAppSettings(ctx, settings)
		}),
		withHandler(tool("draft_local_domain_status", "Get machine-local Draft domain status.", map[string]any{}), func(ctx context.Context, args map[string]any) (any, error) {
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			return c.LocalDomainStatus(ctx)
		}),
		withHandler(tool("draft_enable_local_domain", "Enable privileged machine-local *.draft DNS.", map[string]any{"confirm": confirmSchema()}, "confirm"), func(ctx context.Context, args map[string]any) (any, error) {
			if e := requireConfirm(args); e != nil {
				return nil, e
			}
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			return c.EnableLocalDraftDomain(ctx)
		}),
		withHandler(tool("draft_disable_local_domain", "Disable machine-local *.draft DNS.", map[string]any{"confirm": confirmSchema()}, "confirm"), func(ctx context.Context, args map[string]any) (any, error) {
			if e := requireConfirm(args); e != nil {
				return nil, e
			}
			c, e := getClient(ctx)
			if e != nil {
				return nil, e
			}
			return c.DisableLocalDraftDomain(ctx)
		}),
	}
}
