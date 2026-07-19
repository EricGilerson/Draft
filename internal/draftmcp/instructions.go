package draftmcp

const agentInstructions = `Draft MCP controls the local Draft desktop daemon: Docker services as projects → environments → service nodes. Sandboxes are short-lived environment copies.

## Quick start
1. draft_list_projects (or draft_set_context once you know projectId/environmentId)
2. draft_list_environments / draft_project_summary
3. draft_list_nodes — use returned IDs; never invent them

Optional: draft_set_context {projectId, environmentId} so later tools can omit those fields. draft_get_context / draft_clear_context manage it.

## Recipes
Redeploy after config change:
  draft_stage_service_settings or draft_stage_env → draft_deploy_service → draft_service_health / draft_active_deployment

Blank custom image service:
  draft_create_blank_service → draft_stage_service_settings {image, service_port, cmd_override?} → draft_deploy_service

From template:
  draft_list_templates (compact by default) → draft_get_template if needed → draft_create_service

PR / preview sandbox:
  draft_sandbox_source_repos → draft_preview_sandbox → draft_create_sandbox (startOnCreate) → draft_refresh_sandbox tip|same

Whole environment:
  Prefer draft_run_environment_stack {start|stop|redeploy} over N single deploys.

## Config model
Applied = last successful deploy (+ immediate keys). Staged = pending until successful deploy.
Immediate keys: git_branch, deploy_trigger, redeploy_on_pull, git_stream, service_root, env_file.
Check draft_service_config_status / draft_preview_staged_changes before deploying.

## Safety
Secrets redacted unless includeSecrets=true. Prefer {{secret.KEY}} / {{project.KEY}}.
Destructive ops and sensitive writes need confirm:true (deletes, draft_run_command, draft_set_secret). Prefer preview_* first.

Call draft_help for recipes + full tool index grouped by domain.`

var agentRecipes = map[string]string{
	"discover":          "draft_list_projects → draft_set_context → draft_list_environments → draft_list_nodes / draft_project_summary",
	"redeploy_config":   "draft_stage_service_settings|draft_stage_env → draft_deploy_service → draft_service_health",
	"blank_image":       "draft_create_blank_service → draft_stage_service_settings(image,service_port,cmd_override) → draft_deploy_service",
	"from_template":     "draft_list_templates → draft_create_service → (image templates may auto-start)",
	"preview_sandbox":   "draft_sandbox_source_repos → draft_preview_sandbox → draft_create_sandbox(startOnCreate=true)",
	"environment_stack": "draft_run_environment_stack action=start|stop|redeploy",
}
