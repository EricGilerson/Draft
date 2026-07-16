package draftmcp

import "context"

func metaTools() []toolDef {
	return []toolDef{
		withHandler(tool("draft_help", "Explain Draft workflows and list every available Draft MCP tool.", map[string]any{}), func(_ context.Context, _ map[string]any) (any, error) {
			names := make([]string, 0, len(allTools()))
			for _, t := range allTools() {
				names = append(names, t.Name)
			}
			return map[string]any{"instructions": agentInstructions, "tools": names}, nil
		}),
		withHandler(tool("draft_ping", "Confirm that the Draft daemon is reachable.", map[string]any{}), func(ctx context.Context, _ map[string]any) (any, error) {
			c, err := getClient(ctx)
			if err != nil {
				return nil, err
			}
			return map[string]string{"status": "ok"}, c.Ping(ctx)
		}),
		withHandler(tool("draft_status", "Return Draft daemon, Docker, and local-domain status.", map[string]any{}), func(ctx context.Context, _ map[string]any) (any, error) {
			c, err := getClient(ctx)
			if err != nil {
				return nil, err
			}
			docker, err := c.CheckDocker(ctx)
			if err != nil {
				return nil, err
			}
			domain, err := c.LocalDomainStatus(ctx)
			if err != nil {
				return nil, err
			}
			return map[string]any{"daemon": "ok", "docker": docker, "localDomain": domain}, nil
		}),
	}
}
