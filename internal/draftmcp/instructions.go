package draftmcp

const agentInstructions = `Draft MCP controls the local Draft desktop daemon: Docker services laid out as projects → environments → service nodes on a canvas. Sandboxes are short-lived environment copies.

Always discover before acting:
1) draft_list_projects
2) draft_list_environments / draft_project_summary
3) draft_list_nodes (or draft_get_node)

Use numeric/string IDs returned by those tools — do not invent IDs from labels.

Config model:
- Applied settings/env = what last successful deploy used (plus immediate keys).
- Staged settings/env = pending until the next successful deploy.
- Prefer draft_stage_service_settings / draft_stage_env, then draft_deploy_service (or draft_run_environment_stack).
- Immediate keys (git_branch, deploy_trigger, redeploy_on_pull, git_stream, service_root, env_file) apply without staging.
- Check draft_service_config_status / draft_preview_staged_changes before deploying; then draft_service_health / draft_active_deployment.

Create services with draft_create_service (from draft_list_templates). Image-mode templates may auto-start.

Sandboxes (PR/feature/test copies):
1) draft_sandbox_source_repos
2) draft_preview_sandbox
3) draft_create_sandbox (startOnCreate for previews)
4) draft_refresh_sandbox mode tip|same; extend/suspend/resume/delete as needed

Secrets and project values are redacted unless includeSecrets=true. Prefer {{secret.KEY}} / {{project.KEY}} references over pasting secrets.

Destructive ops (delete/prune/import/promote/unlink/apply sync) require confirm:true. Prefer preview_* tools first.

Call draft_help anytime for the full tool index and this guide.`
