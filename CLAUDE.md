# Draft

Draft is a native desktop app built with Wails v2 (Go backend + React frontend). It manages local Docker services as a visual workspace: projects contain multi-environment service nodes on a canvas, Draft builds and runs those services, assigns ports, injects environment wiring, and exposes stable local hostnames.

## Tech Stack

| Layer | Choice |
|-------|--------|
| Desktop shell | Wails v2 (`v2.12.0`) |
| Backend | Go 1.25 |
| Frontend | React 18 + TypeScript + Vite |
| Canvas | `@xyflow/react` |
| Storage | GORM + `glebarez/sqlite` / `modernc.org/sqlite` |
| Docker integration | Docker SDK (`v28.5.x`) + optional `docker buildx` CLI path |
| Icons | `lucide-react` + `@icons-pack/react-simple-icons` (template brand icons) |

## Runtime Architecture

```text
┌─────────────┐     Wails IPC + daemon HTTP/SSE     ┌──────────────────┐
│  Wails App  │ ◄──────────────────────────────────► │     Daemon       │
│  frontend   │                                      │  (--daemon flag) │
└─────────────┘                                      └────────┬─────────┘
                                                              │
                                         ┌────────────────────┼────────────────────┐
                                         │                    │                    │
                                     SQLite store        Deploy engine        Local router
                                                         Docker builds        hostnames/proxy
                                                         logs/metrics         hosts-file mode
```

- `main.go` starts either the desktop app, the background daemon (`--daemon`), or a fast git-hook entrypoint (`--git-hook`).
- The Wails app binds thin methods in `bindings.go` and talks to the daemon for long-running work.
- The daemon owns the store, deployment engine, Docker watch hub, local routing/proxy, and SSE event stream.
- The daemon is single-instance. It writes state (`daemon.json` with addr/token/pid) under the user config dir (`~/.../Draft/`), reuses an existing healthy daemon, and idles out after **30 minutes** of inactivity.
- On app startup, the daemon reconciles Docker state, missed git-trigger events, installed hooks (`githooks.ReconcileAllHooks`), and **service-link multi-network attachments** (`ReconcileServiceLinkNetworks`).

## Current Project Layout

```text
main.go                    # Wails app startup, daemon mode, git-hook mode
app.go                     # App lifecycle, daemon bootstrap, event bridge, native dialogs
bindings.go                # Wails-bound methods (thin delegates)
cgo_darwin.go              # macOS cgo linker flags for Wails/WebKit
wails.json                 # Wails project config
build/                     # Wails packaging assets (darwin/, windows/, installer)
scripts/
  kill-daemon.sh           # Dev helper to stop the background daemon
  kill-daemon.ps1          # Windows equivalent

internal/
  cloudconfig/             # Import/export adapters (compose, cloudrun, ecs, containerapps)
  daemon/                  # HTTP API, SSE hub, daemon lifecycle, git-trigger reconcile
    paths.go               # Config dir + daemon.json state path
    gittrigger.go          # Hook fire + /hooks/recheck
    client.go              # Daemon HTTP client used by Wails bindings
    routes.go              # Routes listing API
    secrets.go             # App-secrets API
    exec.go                # Container exec / shell proxy
    project.go             # Project-scoped daemon helpers
  deploy/                  # Build/run engine, metrics, env resolution, service projection
    engine.go              # Deploy orchestration (build, image-pull, git-sourced build)
    image_deploy.go        # Image-pull deploy path (no Docker build)
    image_tags.go          # N-1 rollback image retention (…:N-previous)
    stamp.go               # CreateNodeFromTemplate + ReapplyTemplate
    template_expr.go       # {{draft.*}} expression expansion at stamp time
    project_expr.go        # {{project.KEY}} resolution
    secret_expr.go         # {{secret.KEY}} resolution
    volumes.go             # Volume specs + Draft-managed Docker volumes + overview
    volume_clone.go        # Cross-service volume data clone (env duplicate / promote)
    refs.go                # @{Service.ATTR} reference resolution + env connections
    project_services.go    # ListProjectServices + multi-env rollup summary
    settings.go            # Node-setting keys and defaults
    staging.go             # Effective settings/env + promote staged after deploy
    settings_preview.go    # Staged change previews
    service_delete.go      # DeleteService + PreviewDeleteService
    service_link.go        # Cross-environment linked services (share/promote/unlink)
    environment_duplicate.go  # DuplicateEnvironment + stateful data choices
    environment_stack.go   # Start/stop/redeploy all services in an environment
    environment_delete.go  # DeleteEnvironment teardown
    project_delete.go      # DeleteProject teardown
    rollback.go            # RollbackDeployment + RollbackEligibility
    config_sync.go         # PreviewSync / ApplySync between environments
    import.go / export.go  # Cloud-config import/export bridge
    exec.go                # RunCommand (service shell)
    node_health.go         # Lightweight node health for canvas pills
    docker_admin.go        # Docker resource list/remove/prune for Docker view
    buildenv.go            # Dockerfile ARG / build-step facts for Variables UI
    secrets_usage.go       # Secret/project-var usage scan
    profile.go             # Cloud-target runtime env profile (Cloud Run / Container Apps)
  dockerdesktop/           # Best-effort local Docker Desktop launcher
  dockerfile/              # Dockerfile EXPOSE + ARG/build-info parser
  dockerwatch/             # Docker daemon health and event watching
  envfile/                 # .env import/export/refresh helpers
  githooks/                # post-commit / pre-push / post-merge hook install + chaining
  gitsrc/                  # Branch/ref export and ref/SHA helpers via git
  ignore/                  # .dockerignore / .gitignore matching
  networking/              # Port leases, proxy, hosts/domain routing, URL selection
    hostname.go            # Internal *.draft.local + public *.draft.resolv.sh (+ TCP endpoints)
    router.go              # Local domain modes, GetLocalDomainStatus
    hosts.go               # Hosts-file block management
  store/                   # GORM models, CRUD, auto-migration, validation helpers
    environments.go        # Multi-environment CRUD
    staging.go             # Staged node_settings / env_vars
    project_env_vars.go    # Project-scoped shared values
    app_secrets.go         # App-wide secrets
    app_settings.go        # App-wide preferences (sidebar, local domain)
    immediate_settings.go  # Settings that apply without staging/deploy
    default_settings.go    # Template DefaultSettings JSON helpers
    templates.go           # Template CRUD + seeding
    templates_builtin.go   # Built-in template definitions
    template_schema.go     # Wizard/settings schema model
    image_tags.go          # Curated image tag JSON helpers
    volumes.go             # TemplateVolume JSON normalization
    git_repo.go            # Cached git_repo_root per node

frontend/src/
  App.tsx                  # Mounted shell; BuildLogProvider + AppDialogProvider; nav + topbar
  lib/
    dashboardData.ts       # Project summaries for Overview/Projects
    logStreamManager.ts    # Ref-counted log stream lifecycle
    useActivityLog.ts      # Activity feed derivation for Overview
    envStaging.ts          # Client-side staged env draft helpers
    settingStaging.ts      # Immediate vs staged setting keys
    referenceIssues.ts     # Client-side @{}/{{project}}/{{secret}} issue scan
    linkedService.ts       # Linked-service target hook
    buildEnvWarnings.ts    # Build-arg / scope warnings for Variables tab
    serviceConfigEditor.tsx
  utils/
    imageRef.ts            # Image tag parsing for templates
    templateDefaults.ts    # Template default settings helpers
  components/
    Sidebar.tsx            # Left nav (overview/projects/templates/secrets/volumes/docker/routes/sandboxes/settings)
    ActivityTicker.tsx     # Docker activity feed in topbar
    DockerIndicator.tsx    # Docker status in topbar
    BuildLogProvider.tsx   # Global build log + upload progress state
    AppDialogProvider.tsx  # Centralized confirm/alert dialogs
    CreateProjectDialog.tsx
    CreateServiceDialog.tsx  # Template picker + multi-step wizard
    ImportConfigDialog.tsx / ExportConfigDialog.tsx / ConfigReport.tsx
    SyncConfigDialog.tsx   # Cross-environment config sync
    ProjectSettingsDialog.tsx  # Project metadata + project env vars
    EnvironmentSwitcher.tsx    # Multi-env switcher + create/duplicate/stack ops
    ProjectCanvas.tsx      # Canvas, env-reference edges, ServiceNode, volume chips, drawer
    ServiceNode.tsx        # Custom canvas node with template icon + inline volumes
    EnvReferenceEdge.tsx   # Grouped env-reference edges with popover
    NodeDetailPanel.tsx    # Service detail drawer with tabs
    OverviewTab.tsx        # Deploy/stop/restart + build output + URLs + linked info
    DeploymentsTab.tsx     # Deployment history + rollback eligibility
    VariablesTab.tsx       # Env vars, previews, linking, .env sync, staged drafts
    LogsTab.tsx            # Live container logs
    MetricsTab.tsx         # CPU/memory/network/reachability
    ShellTab.tsx           # Interactive container shell (exec + resize)
    SettingsTab.tsx        # Service build/runtime/network/security settings (staged)
    ServiceDraftBar.tsx    # Pending staged changes bar (deploy / discard)
    SettingStagingNote.tsx # Per-setting staged vs applied hint
    VolumeDetailPanel.tsx  # Volume focus drawer from canvas chips
    VolumeEditor.tsx       # Volume mount editor (wizard + settings)
    TemplateEditorDialog.tsx  # Includes default node settings editor
    TemplateIcon.tsx
    Dialog.tsx, EmptyState.tsx, PageHeader.tsx, StatusBadge.tsx, ServicePill.tsx
    ScopedValueUsages.tsx  # Secret / project-var usage lists
  views/
    OverviewView.tsx       # Live overview dashboard (mounted; project + activity data)
    ProjectsView.tsx       # Project list / project cards with multi-env rollups
    TemplatesView.tsx      # Template library with search + category grouping
    SecretsView.tsx        # App-wide secrets management (mounted)
    VolumesView.tsx        # Draft-managed volumes overview (mounted)
    DockerView.tsx         # Docker resources: containers/images/networks/volumes + prune
    RoutesView.tsx         # Hostname routes listing (mounted)
    SettingsView.tsx       # App settings; persists via GetAppSettings/SetAppSettings
    SandboxesView.tsx      # Mock sandbox cards; NOT mounted
  wailsjs/                 # Generated Wails bindings (do not hand-edit)
```

There is no root project README beyond a one-line `README.md`; `README.wails.md` is stock Wails template text. Line endings are normalized to LF via `.gitattributes` (CRLF kept for Windows `.ps1`/`.bat`/`.cmd`).

## Data Model

Primary store tables:

- `projects`
- `environments` — named, independently deployable copies of a project’s services
- `canvas_nodes` — scoped to both `projectId` and `environmentId`
- `service_templates`
- `deployments`
- `env_vars` / `env_vars_staged`
- `node_settings` / `node_settings_staged`
- `project_env_vars` — project-scoped shared values (`{{project.KEY}}`)
- `app_secrets` — app-wide credentials (`{{secret.KEY}}`)
- `app_settings` — app-wide preferences
- `routes`
- `port_leases`

Important model details:

- Every project has exactly one **default environment** (created with the project). Environment **slug** is immutable and is baked into hostnames, Docker network names, image/container tags, and volume names; renaming the display name does not disturb running resources.
- `canvas_nodes.uid` is the stable per-node hostname suffix (unique within an environment).
- `canvas_nodes.templateId` links a node to the `ServiceTemplate` it was created from (0 = blank/legacy node; not a foreign key).
- Hostnames use four segments: `{service}.{project}.{environment}.{uid}.draft.local` (public: `*.draft.resolv.sh`).
- Docker networks: `draft-{projectId}-{project}-{environment}`.
- Image tags / container names include the environment segment: `draft-{project}-{environment}-{service}:{sequence}` / `draft-{project}-{environment}-{service}-{sequence}`.
- Draft-managed volumes: `draft-{projectId}-{project}-{environment}-{uid}-{target}` (labeled `draft.managed=true`).
- `deployments.source_sha` records the commit built for pinned git-branch deploys.
- `deployments.sequence` is the per-node deploy counter used in image tags and container names.
- Deployment lifecycle statuses include `pending|building|built|starting|running|stopped|failed|interrupted` (interrupted = cut short by app restart). UI status mapping preserves real lifecycle states rather than collapsing everything to a few buckets.
- `env_vars` carry `scope` (runtime/build/both), `secret`, `source`, and `envFile`.
- `project_env_vars` are **key/value (+ optional secret flag) only**; scope is determined by the service variable that references `{{project.KEY}}`.
- Staged tables hold pending settings/env changes until the next **successful** deploy promotes them into the applied tables (see Staged Config).
- `node_settings` remains the extensible feature surface. Important keys include `dockerfile`, `image`, `service_port`, `service_root`, `git_branch`, `git_stream`, `deploy_trigger`, `redeploy_on_pull`, `git_repo_root`, `volume_mounts`, `use_buildkit_local_context`, `use_dockerignore`, `use_gitignore`, `route_protocol` (`http`|`tcp`), `host_port`, `keep_images` (`last`|`none`|`all`), `service_link` (JSON link to a root node in another env), plus settings-tab keys in `deploy/settings.go`.
- `service_templates` hold reusable blueprints (built-in + user-created) with embedded Dockerfiles, default env vars, volume defaults, **DefaultSettings** (JSON node_settings stamped at create), and a JSON schema that drives the create wizard and Settings tab section visibility.
- Volume mounts are stored as `volume_mounts` JSON in `node_settings`, not a separate SQL table.

## What Is Implemented

### Core workspace

- Project creation, update, delete (delete stops services; Draft volumes are left as orphans for reclaim in Volumes view).
- Project listing with multi-environment service rollups (`ListProjectServicesSummary`).
- **Multi-environment projects**: create / rename / set-default / delete environments; canvas is per-environment; `EnvironmentSwitcher` in the project chrome.
- **Environment stack ops**: start / stop / redeploy all services in an environment (independent per node; no depends-on ordering).
- **Duplicate environment** with per-stateful-service data choices: `fresh` | `share` | `clone` (clone supports `consistent` vs `quick` consistency).
- Canvas nodes with persisted positions, rename, delete-with-preview, and add-service flow via template wizard or blank node.
- Project settings dialog: project metadata + project-level env vars.

### Templates

- Built-in library spanning web/language/datastore/tooling, including: Next.js, Node.js, Go, Rust, Deno, FastAPI, Flask, Vite, Express, Django, Ruby on Rails, Phoenix, .NET, Spring Boot, Static Site, PostgreSQL, Redis, MySQL, MongoDB, MinIO, RabbitMQ, Meilisearch, Memcached, ClickHouse, Mailpit, Adminer, **Prebuilt Image**.
- User create/edit/clone/delete, template editor UI (including **default node settings**), search + category grouping.
- `CreateNodeFromTemplate` stamping (settings, env vars, optional Dockerfile, volumes, DefaultSettings).
- **`{{draft.*}}` template expressions** in template defaults resolved at stamp time.
- **`ReapplyTemplate`**: re-stamps a node from its original template while preserving user env overrides.
- Image-mode services auto-start (background deploy) after stamp when settings are deployable.

### Deploy & runtime

- **Image-mode deploy**: when `image` is set and `dockerfile` is empty, Draft pulls and runs without building.
- Docker build + run deployments with streaming build logs and upload progress events.
- Deployment history, active deployment lookup, stop, restart, cancel-build behavior.
- **Rollback**: re-run a historical deployment’s image when still retained; `RollbackEligibility` drives UI; image-mode can re-pull if missing.
- **Image retention (`keep_images`)**: default `last` keeps N-1 under `…:N-previous` for rollback; `none` removes priors on cutover; `all` keeps every image.
- Live container logs, **service shell** (`ShellTab` / `RunCommand` with terminal resize), and service metrics with port-based reachability checks.
- Lightweight **node health** API for canvas status pills.
- Port leasing plus local hostname/routing (internal + public hostnames).
- **TCP routes**: `route_protocol=tcp` + host port; public/internal endpoints without `http://` scheme; HTTP remains reverse-proxied.
- **Volume mounts**: bind mounts and Draft-managed named volumes, list/delete, inline volume chips on service nodes, volume detail panel, and workspace **Volumes** view (refcount, orphan detection).
- Service root and Dockerfile selection, including EXPOSE + ARG/build-step parsing for Variables warnings.
- Per-project Docker networks (environment-scoped).
- Native folder/file picker dialogs (`SelectFolder`, `SelectFile`).
- Expanded lifecycle statuses surfaced in UI (`building`, `starting`, `pending`, `interrupted`, etc.).

### Env wiring & secrets

- `.env` import, export, refresh, conflict reporting, and suggested env-file path.
- Dynamic env references between services using `@{Service.ATTR}`-style links, preview resolution, grouped read-only connection edges on the canvas, and broken-reference badges.
- Client-side reference-issue computation against live drafts (`referenceIssues.ts`) so warnings update before save.
- Draft-injected runtime vars: `DRAFT_SERVICE_PORT`, `DRAFT_INTERNAL_HOSTNAME`, `DRAFT_INTERNAL_URL`, `DRAFT_PUBLIC_HOSTNAME`, `DRAFT_PUBLIC_URL`, `DRAFT_SERVICE_NAME`, `DRAFT_PROJECT_NAME`, `DRAFT_ENVIRONMENT` (plus deprecated aliases `DRAFT_PORT`, `DRAFT_HOSTNAME`).
- **Project values** via explicit `{{project.KEY}}` tokens (not auto-injected).
- **App secrets** via `{{secret.KEY}}` tokens; Secrets view with usage scan.
- Build-time env wiring warnings (Dockerfile ARG / scope analysis).
- Duplicate env-key validation; staged env drafts normalized before apply.

### Multi-environment linking

- **Linked services**: an alias node in one environment can share a root service in another via `service_link` JSON setting.
- Root container is multi-attached onto each linker environment’s Docker network with aliases matching the alias node’s identity (hostname/service name stay local to the alias env).
- Linked env previews rewrite URLs/hostnames to the **alias** identity so dependents see the local env’s names.
- Promote alias → real service (`empty` or `clone` seed); unlink with become options; guard root delete while linkers exist.
- Network reattach on daemon startup.

### Staged config

- Most settings and env edits are **staged** until the next successful deploy promotes them.
- **Immediate settings** (apply without deploy): `git_branch`, `deploy_trigger`, `redeploy_on_pull`, `git_stream`, `service_root`, `env_file`.
- UI: `ServiceDraftBar`, per-setting staging notes, discard/preview staged changes.

### Git triggers

- Deploy-from-git for pinned branches/refs without touching the working tree.
- Automatic redeploy triggers on commit or push via local git hooks, with chaining to pre-existing foreign hooks.
- **Redeploy on pull**: independent `redeploy_on_pull` setting installs a `post-merge` hook for merge-based `git pull` (orthogonal to `deploy_trigger`).
- Git repo-root awareness: cached `git_repo_root` per node for multi-repo projects; hook scripts normalize Windows paths.

### Cloud config & sync

- **Import/export** via `internal/cloudconfig`: formats `compose`, `cloudrun`, `ecs`, `containerapps`.
  - Import as a new project or into an existing project/environment.
  - Export single service or whole environment stack; optional write-to-path.
  - Round-trip: original document stored on the node for overlay on re-export.
  - Cloud-target runtime profile env injected when running locally as Cloud Run / Container Apps.
- **Config sync** between environments (or single matched services): preview diffs for settings/env; apply as `stage` or `stageAndRedeploy`.

### Workspace admin views

- **Overview**: live dashboard of projects + activity (mounted).
- **Secrets**, **Volumes**, **Docker**, **Routes**: fully mounted management views.
- **Docker view**: list/start/stop/restart/remove containers; list/remove images (grouped, timestamps); networks; volumes; prune with optional Draft-only filter; bulk selection + confirm dialogs.
- **App settings**: compact sidebar + local domain preference persisted in `app_settings` (`auto` | `public-hostname-port` | `localhost-port`).

## Build And Deploy Behavior

Draft has three deploy paths, selected by node settings:

1. **Build mode** (`dockerfile` + `service_port`): Docker build from working tree or git ref, then run.
2. **Image mode** (`image` + `service_port`, no `dockerfile`): `docker pull` + run — used by datastore templates, Prebuilt Image, and custom image services.
3. **Git-sourced build**: same as build mode but source comes from a pinned ref instead of the working tree.

Shared tail for all paths: resolve env (including `{{project.*}}` / `{{secret.*}}` / `@{Service.ATTR}`), attach volumes, join project network, register routes (HTTP or TCP), start container, promote staged config on success, apply image-retention policy.

- Default source mode is the working tree on disk.
- If `git_branch` is set, Draft deploys committed content from that ref instead of the live working tree.
- Pinned git deploys have two transport modes:
  - Stream mode (`git_stream` default on): pipe `git archive` output directly to Docker. Fastest, but `.dockerignore`, `.gitignore`, and BuildKit local-context behavior do not apply.
  - Checkout mode (`git_stream=false`): export the ref into a temp directory and build from that on-disk workspace. Slower, but it can honor ignore rules and BuildKit local-context behavior.
- BuildKit local-context is optional and best-effort (`use_buildkit_local_context` per service). Draft uses `docker buildx build --load` only when the current settings are compatible with the legacy context semantics. Inactive while git-streaming.
- If BuildKit local-context would change ignore behavior, widen the context incorrectly, or otherwise diverge from Draft's legacy tar path, Draft logs the reason and falls back to the legacy Docker SDK upload path (with upload progress events).
- `.gitignore`-based context filtering blocks the BuildKit local-context path entirely.
- Root `.dockerignore` compatibility is required before BuildKit local-context is used when Draft's `.dockerignore` toggle is on.
- Redeploy cancels any in-flight build for the same node.
- Image tags and container names use the per-node `deployments.sequence` counter **and environment slug**, not the global deployment ID.
- Template stamping writes embedded Dockerfiles to the service root (best-effort), resolves `{{draft.*}}` env defaults, seeds volume mounts and DefaultSettings from the template.
- **Linked services** short-circuit deploy: ensure the root is multi-attached rather than building a second container.
- **Rollback** reuses the shared start/register tail with a historical image tag; it does not rebuild from git SHA yet.

## Git Trigger Behavior

- Trigger values are `manual`, `on_commit`, and `on_push`.
- Triggers only matter when a node also has a pinned `git_branch`.
- **`redeploy_on_pull`** is orthogonal to `deploy_trigger`: when enabled, Draft installs a **`post-merge`** hook that fires `on_pull` events after merge-based `git pull` or `git merge`. Works even when `deploy_trigger` is `manual`. Does not detect `git pull --rebase`.
- Hook installation is per repo, not per node. Draft reference-counts hook need across all nodes in the project.
- Hooks are written into the repo's actual hooks directory using `git rev-parse --git-path hooks`, so `core.hooksPath` and worktrees are honored.
- Draft never overwrites a foreign hook destructively; it preserves and chains to it via `.draft-orig`. `GitHookStatus` reports `pullForeign` when a non-Draft `post-merge` hook exists.
- Hook scripts use cached `git_repo_root` and normalize Windows paths.
- On startup, the daemon reconciles missed commit/push/pull events by comparing tracked branch SHAs against `deployments.source_sha`.

## Networking

- Each service gets dual hostnames: internal `*.draft.local` (Docker network DNS) and public `*.draft.resolv.sh` (internet-resolvable via Draft's resolver). Pattern: `{service}.{project}.{environment}.{uid}.draft.local`.
- **Local domain modes**: preference stored in app settings as `auto` | `public-hostname-port` | `localhost-port`. Exposed via `GetLocalDomainStatus` (preference applied in bindings).
- HTTP services are proxied by Draft's built-in reverse proxy; TCP services resolve via the hosts file and expose `host:port` endpoints (no HTTP scheme).
- URL selection for deployments prefers the best available public URL (`networking/best_url.go`), protocol-aware for TCP.
- Linked roots are attached to multiple environment networks so alias hostnames resolve inside each environment’s network.

## Staged Config And Immediate Settings

- Applied config = live `node_settings` + `env_vars` (what the last successful deploy used, plus any immediate writes).
- Staged config = pending overrides in `node_settings_staged` / `env_vars_staged`.
- Effective config (previews, next deploy) = applied merged with staged.
- On successful deploy, staged rows are promoted into applied and cleared.
- Immediate keys bypass staging and write applied settings directly (git/deploy-trigger/service-root/env-file path).
- Frontend keeps local drafts for responsive editing; backend staging is the source of truth after save/stage calls.

## Frontend Reality

- `frontend/src/App.tsx` is the real mounted shell, wrapped in `BuildLogProvider` + `AppDialogProvider`.
- Default nav view is **Overview** (`OverviewView` with live project summaries and activity).
- Main workflow: Projects → pick environment → canvas → node detail panel tabs (Overview / Deployments / Variables / Logs / Metrics / Shell / Settings).
- **Mounted workspace views**: Overview, Projects, Templates, Secrets, Volumes, Docker, Routes, Settings.
- **Sandboxes** remains a placeholder empty state in the mounted app (`SandboxesView.tsx` exists as a richer design mock but is not mounted).
- Settings view **persists** compact sidebar and local-domain preference via `GetAppSettings` / `SetAppSettings`.
- Topbar shows `ActivityTicker` (Docker events) and `DockerIndicator`.
- Service creation uses `CreateServiceDialog` (template picker + wizard); blank services fall back to `CreateNode`.
- Canvas nodes render template icons via `templateId` + `ListServiceTemplates`; volume mounts render as selectable chips; env-reference edges group by node pair.
- Environment chrome: `EnvironmentSwitcher` (switch, create, duplicate “based on”, stack start/stop/redeploy, sync config, delete).
- Import cloud config is available from the app chrome; export from project/service UI.

## Not Yet Implemented

- Real sandboxes / ephemeral environments.
- Branch/worktree UX beyond the current pinned-ref deploy path.
- Rebuild-from-SHA rollback for git-sourced builds (rollback requires a retained local image under `keep_images`).
- `git pull --rebase` detection for redeploy-on-pull.
- Depends-on / ordered start for environment stack ops (actions currently run independently per node).

## Conventions And Constraints

- Prefer the live mounted path over adjacent placeholder components.
- Docker labels are the runtime source of truth and daemon reconcile matters on startup (including service-link networks).
- Prefer event-driven updates (SSE / Docker events) over polling.
- Keep Windows compatibility in mind; the repo is intentionally using pure-Go SQLite to avoid CGO on Windows.
- macOS still requires CGO for the Wails/WebKit build.
- Line endings: LF in-repo (see `.gitattributes`); do not fight Wails-generated `frontend/wailsjs` with CRLF.
- When reasoning about build regressions in this repo, separate:
  - source mode: working tree vs pinned git ref
  - transport mode: BuildKit local-context vs legacy tar upload
  - ignore semantics: Draft filters vs `.dockerignore` / `.gitignore`
  - deploy path: build vs image-pull vs git-sourced build
  - environment scope: which environment’s network/hostname/volume namespace is in play
  - link mode: root container vs alias (linked) node
  - config layer: applied vs staged vs immediate settings
