package draftmcp

import (
	"context"
	"strings"
)

func metaTools() []toolDef {
	return []toolDef{
		withHandler(tool("draft_help", "Draft workflows, recipes, and tool index by domain. Call when unsure which tool to use.", map[string]any{}), func(_ context.Context, _ map[string]any) (any, error) {
			byDomain := map[string][]string{}
			for _, t := range allTools() {
				domain := toolDomain(t.Name)
				byDomain[domain] = append(byDomain[domain], t.Name)
			}
			return map[string]any{
				"instructions": agentInstructions,
				"recipes":      agentRecipes,
				"context":      getSessionContext(),
				"toolsByDomain": byDomain,
			}, nil
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
			return map[string]any{"daemon": "ok", "docker": docker, "localDomain": domain, "context": getSessionContext()}, nil
		}),
		withHandler(tool("draft_set_context", "Set default projectId/environmentId for later tools that omit those fields.", map[string]any{
			"projectId":     uintSchema("Default project ID"),
			"environmentId": uintSchema("Default environment ID"),
		}), func(_ context.Context, args map[string]any) (any, error) {
			cur := getSessionContext()
			if id := optionalUint(args, "projectId"); id != 0 {
				cur.ProjectID = id
			}
			if id := optionalUint(args, "environmentId"); id != 0 {
				cur.EnvironmentID = id
			}
			if cur.ProjectID == 0 && cur.EnvironmentID == 0 {
				return nil, errf("provide projectId and/or environmentId")
			}
			setSessionContext(cur)
			return cur, nil
		}),
		withHandler(tool("draft_get_context", "Get the current default project/environment context.", map[string]any{}), func(_ context.Context, _ map[string]any) (any, error) {
			return getSessionContext(), nil
		}),
		withHandler(tool("draft_clear_context", "Clear default project/environment context.", map[string]any{}), func(_ context.Context, _ map[string]any) (any, error) {
			clearSessionContext()
			return map[string]string{"status": "cleared"}, nil
		}),
	}
}

func toolDomain(name string) string {
	switch {
	case strings.Contains(name, "sandbox"):
		return "sandbox"
	case strings.Contains(name, "docker") || name == "draft_run_command":
		return "docker"
	case strings.Contains(name, "secret") || strings.Contains(name, "env") || strings.Contains(name, "project_env") || strings.Contains(name, "reference"):
		return "env_secrets"
	case strings.Contains(name, "draftpack") || strings.Contains(name, "config_import") || strings.Contains(name, "export_"):
		return "import_export"
	case strings.Contains(name, "volume") || strings.Contains(name, "link") || strings.Contains(name, "share") || strings.Contains(name, "promote") || strings.Contains(name, "unlink"):
		return "links_volumes"
	case strings.Contains(name, "environment") || strings.Contains(name, "sync") || strings.Contains(name, "duplicate"):
		return "environment"
	case strings.Contains(name, "deploy") || strings.Contains(name, "service") || strings.Contains(name, "rollback") || strings.Contains(name, "template") || strings.Contains(name, "blank") || strings.Contains(name, "build_log") || strings.Contains(name, "health") || strings.Contains(name, "metric"):
		return "service"
	case strings.Contains(name, "app_settings") || strings.Contains(name, "local_domain"):
		return "settings"
	case name == "draft_help" || name == "draft_ping" || name == "draft_status" || strings.Contains(name, "context"):
		return "meta"
	default:
		return "discovery"
	}
}

func errf(msg string) error {
	return &simpleError{msg}
}

type simpleError struct{ s string }

func (e *simpleError) Error() string { return e.s }
