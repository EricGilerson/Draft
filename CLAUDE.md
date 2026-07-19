# Draft

Draft is a native desktop app built with Wails v2 (Go backend + React frontend). It manages local Docker services as a visual workspace: projects contain multi-environment service nodes on a canvas, Draft builds and runs those services, assigns ports, injects environment wiring, and exposes stable local hostnames. Durable environments support day-to-day stacks; **sandboxes** are short-lived environment copies for previews and test recipes.

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
- The daemon owns the store, deployment engine, Docker watch hub, local routing/proxy, sandbox lifecycle, and SSE event stream.
- The daemon is single-instance. It writes state (`daemon.json` with addr/token/pid) under the user config dir (`~/.../Draft/`), reuses an existing healthy daemon, and idles out after **30 minutes** of inactivity.
- On app startup, the daemon reconciles Docker state, missed git-trigger events, installed hooks (`githooks.ReconcileAllHooks`), **service-link multi-network attachments** (`ReconcileServiceLinkNetworks`), and **sandbox lifecycle** (`ReconcileSandboxLifecycle`). A background ticker re-runs sandbox lifecycle every minute while the daemon is up.

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
  cloudconfig/             # Cloud-format adapters (compose, cloudrun, ecs, containerapps)
  draftpack/               # Draft-native portable packs (share project/env/service across machines)
  daemon/                  # HTTP API, SSE hub, daemon lifecycle, git-trigger reconcile
    paths.go               # Config dir + daemon.json state path
    gittrigger.go          # Hook fire + /hooks/recheck
    client.go              # Daemon HTTP client used by Wails bindings
    routes.go              # Routes listing API
    secrets.go             # App-secrets API
    exec.go                # Container exec / shell proxy
    project.go             # Project-scoped daemon helpers
    sandbox.go             # Sandbox lifecycle ticker + HTTP handlers helpers
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
    sandbox.go             # Preview/create/extend/suspend/resume/delete + source repos + refresh + lifecycle
    sandbox_test_run.go    # Testing sandbox step suites (fresh | steps)
    sandbox_source_test.go # Branch pin + refresh tip/same tests
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
  githooks/                # post-commit / pre-push / post-merge / post-rewrite hook install + chaining
  gitsrc/                  # Branch/ref export and ref/SHA helpers via git
  ignore/                  # .dockerignore / .gitignore matching
  networking/              # Port leases, proxy, hosts/domain routing, URL selection
    hostname.go            # Internal *.draft.local (+ sandbox sand segment) + public *.draft.resolv.sh
    router.go              # Local domain modes, GetLocalDomainStatus
    hosts.go               # Hosts-file block management
    dns.go                 # Optional machine-local *.draft resolver
  store/                   # GORM models, CRUD, auto-migration, validation helpers
    environments.go        # Multi-environment CRUD
    staging.go             # Staged node_settings / env_vars
    project_env_vars.go    # Project-scoped shared values
    app_secrets.go         # App-wide secrets
    app_settings.go        # App-wide preferences (sidebar, local domain, proxy ports, *.draft DNS)
    sandboxes.go           # Sandbox CRUD, profiles, links, repo sources, test runs
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
    ImportDraftPackDialog.tsx / ExportDraftPackDialog.tsx  # Draft-native .draftpack share
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
    SandboxExtendControl.tsx / SandboxHoursInput.tsx  # Sandbox TTL extend UI
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
    SandboxesView.tsx      # Sandbox list, profiles, preview/test create, test-run history (mounted)
  wailsjs/                 # Generated Wails bindings (do not hand-edit)
```

There is no root project README beyond a one-line `README.md`; `README.wails.md` is stock Wails template text. Line endings are normalized to LF via `.gitattributes` (CRLF kept for Windows `.ps1`/`.bat`/`.cmd`).

## Data Model

Primary store tables:

- `projects`
- `environments` — named, independently deployable copies of a project’s services (including sandbox-owned environments)
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
- `sandbox_project_settings` — project-wide sandbox lifecycle defaults (TTL / warning / grace / idle-suspend hours)
- `sandbox_profiles` — reusable creation plans (JSON `PlanJSON`; optional source-environment scope)
- `sandboxes` — durable lifecycle record for one disposable environment (`PlanJSON` is immutable at create)
- `sandbox_links` — open-ended external context (PR, ticket, URL, …) attached to a sandbox
- `sandbox_repository_sources` — per-repo resolved ref + commit SHA used by a sandbox
- `sandbox_test_runs` — history of testing-sandbox step suites (kept after sandbox purge)

Important model details:

- Every project has exactly one **default environment** (created with the project). Environment **slug** is immutable and is baked into hostnames, Docker network names, image/container tags, and volume names; renaming the display name does not disturb running resources. Sandbox environments are normal environments plus a `sandboxes` row; their slugs remain unique within the project.
- `canvas_nodes.uid` is the stable per-node hostname suffix (unique within an environment).
- `canvas_nodes.templateId` links a node to the `ServiceTemplate` it was created from (0 = blank/legacy node; not a foreign key).
- Hostnames:
  - Normal: `{service}.{project}.{environment}.{uid}.draft.local` (public: `*.draft.resolv.sh`).
  - Sandbox: `{service}.{project}.sand.{environment}.{uid}.draft.local` (fixed `sand` label so sandbox DNS cannot collide with durable envs).
  - Optional host DNS: machine-local `*.draft` via Draft’s local resolver (Docker still uses `*.draft.local` internally).
- Docker environment segment: normal envs use the slug; sandboxes use `sand-{slug}` in network / image / container / volume names for operator clarity.
- Docker networks: `draft-{projectId}-{project}-{environmentSegment}`.
- Image tags / container names include the environment segment: `draft-{project}-{environmentSegment}-{service}:{sequence}` / `draft-{project}-{environmentSegment}-{service}-{sequence}`.
- Draft-managed volumes: `draft-{projectId}-{project}-{environmentSegment}-{uid}-{target}` (labeled `draft.managed=true`).
- `deployments.source_sha` records the commit built for pinned git-branch deploys.
- `deployments.sequence` is the per-node deploy counter used in image tags and container names.
- Deployment lifecycle statuses include `pending|building|built|starting|running|stopped|failed|interrupted` (interrupted = cut short by app restart). UI status mapping preserves real lifecycle states rather than collapsing everything to a few buckets.
- Sandbox statuses include `active|warning|expired|suspended|cleanup_failed` (and lifecycle reconcile advances them over time).
- `env_vars` carry `scope` (runtime/build/both), `secret`, `source`, and `envFile`.
- `project_env_vars` are **key/value (+ optional secret flag) only**; scope is determined by the service variable that references `{{project.KEY}}`.
- Staged tables hold pending settings/env changes until the next **successful** deploy promotes them into the applied tables (see Staged Config).
- `node_settings` remains the extensible feature surface. Important keys include `dockerfile`, `image`, `service_port`, `service_root`, `git_branch`, `git_stream`, `deploy_trigger`, `redeploy_on_pull`, `git_repo_root`, `volume_mounts`, `use_buildkit_local_context`, `use_dockerignore`, `use_gitignore`, `route_protocol` (`http`|`tcp`), `host_port`, `keep_images` (`last`|`none`|`all`), `service_link` (JSON link to a root node in another env), plus settings-tab keys in `deploy/settings.go`.
- `service_templates` hold reusable blueprints (built-in + user-created) with embedded Dockerfiles, default env vars, volume defaults, **DefaultSettings** (JSON node_settings stamped at create), and a JSON schema that drives the create wizard and Settings tab section visibility.
- Volume mounts are stored as `volume_mounts` JSON in `node_settings`, not a separate SQL table.
- Sandbox `PlanJSON` captures purpose, TTL/warning/grace/idle-suspend, per-service copy/share/omit (+ data mode), per-repo refs, and (for testing) steps + `onComplete`. Changing a profile later never rewires an existing sandbox.

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
- **Image retention (`keep_images`)**: default `last` keeps N-1 under `…:N-previous` for fast `run-image` rollback; `none` removes priors on cutover; `all` keeps every image. When the image is gone, build-mode rollbacks with a recorded `source_sha` rebuild from that commit (`rebuild-sha`) using current settings.
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
- **Share targets**: `ListShareTargets` lists other environments’ roots for converting a node into a shared alias, with best “same service” match hints.
- Network reattach on daemon startup.

### Sandboxes (ephemeral environments)

- **Mounted Sandboxes view** plus in-project create from `EnvironmentSwitcher` (`SandboxesView` full page or `dialogOnly` overlay).
- Sandboxes are short-lived **environment copies** materialised through the same duplication path as durable envs (networks, UIDs, hostnames, volume clone, shared-service multi-network attach).
- **Purposes**:
  - `preview` — human-driven PR/feature copies (default longer TTL from project settings).
  - `test` — recipe + commands; shorter default TTL; optional `onComplete` = `leave` | `delete` | `suspend`.
- **Service plan rules** (keyed by source node ID): `copy` | `share` | `omit`, with copy data modes `fresh` | `share` | `clone` (+ `consistent` | `quick`).
- **Source code at create** (does not mutate durable env settings):
  - `ListSandboxSourceRepos` discovers git repos used by source services, local branches, and (when available) open GitHub PRs via `gh`.
  - Create dialog can keep source pins, pick a **branch/ref**, or pick a **PR** (gated per-repo: `gh` on PATH + `gh repo view` succeeds).
  - `plan.repositories` pins each repo root to a **deployable** git ref: PreferDeployableRef rewrites bare PR head names to `origin/<branch>` when needed; fork PRs fetch `pull/<n>/head` into `origin/pr/<n>`. Copied services’ `git_branch` stores that tip-following ref (not a raw SHA), so Settings is readable and redeploys follow tip. “Same SHA” refresh freezes `git_branch` to the recorded commit for a one-shot rebuild.
  - PR selection also records a structured `pr:N` sandbox link; freeform links remain available.
  - Preview create uses **Create & start** (`startOnCreate`) so the sandbox deploys immediately after materialize.
- **Refresh**: `RefreshSandbox` mode `tip` re-resolves human refs and redeploys; mode `same` redeploys at frozen SHAs (sandbox-native rebuild-from-SHA).
- **Profiles**: reusable project (or source-env-scoped) plans; create can merge profile + request overrides; resolved plan is frozen on the sandbox row.
- **Lifecycle**: project defaults (`sandbox_project_settings`) for TTL / warning / grace / idle-suspend hours; statuses `active` → `warning` → `expired` → purge after grace; `suspended` still expires on schedule; `cleanup_failed` is retriable on reconcile. Idle auto-suspend uses `LastActivityAt` (create/extend/resume + proxy hits) when `suspendIdleHours` is set.
- **Actions**: preview plan (no Docker), create (+ optional start), extend, suspend/resume, refresh tip/same SHA, delete (destructive: services + Draft-managed volumes + sandbox network).
- **Testing runs**: `RunTestingSandbox` with mode `fresh` (new sandbox + steps) or `steps` (re-run on live testing sandbox); step results and suite pass/fail stored in `sandbox_test_runs` (history survives sandbox delete).
- **Links**: optional PR/ticket/URL-style context rows on the sandbox.
- Daemon **ReconcileSandboxLifecycle** on startup and every minute while running.

### Staged config

- Most settings and env edits are **staged** until the next successful deploy promotes them.
- **Immediate settings** (apply without deploy): `git_branch`, `deploy_trigger`, `redeploy_on_pull`, `git_stream`, `service_root`, `env_file`.
- UI: `ServiceDraftBar`, per-setting staging notes, discard/preview staged changes.

### Git triggers

- Deploy-from-git for pinned branches/refs without touching the working tree.
- Automatic redeploy triggers on commit or push via local git hooks, with chaining to pre-existing foreign hooks.
- **Redeploy on pull**: independent `redeploy_on_pull` setting installs `post-merge` and `post-rewrite` hooks (merge and rebase pulls; orthogonal to `deploy_trigger`).
- Cold-start: when a hook fires with no live daemon, the payload is spooled under `pending-rechecks/` and drained on daemon startup (exact refs, not tip-only guess).
- Git repo-root awareness: cached `git_repo_root` per node for multi-repo projects; hook scripts normalize Windows paths.

### Cloud config & sync

- **Import/export** via `internal/cloudconfig`: formats `compose`, `cloudrun`, `ecs`, `containerapps`.
  - Import as a new project or into an existing project/environment.
  - Export single service or whole environment stack; optional write-to-path.
  - Round-trip: original document stored on the node for overlay on re-export.
  - Cloud-target runtime profile env injected when running locally as Cloud Run / Container Apps.
- **Config sync** between environments (or single matched services): preview diffs for settings/env; create missing source services; target-only dialog (`leave` | `delete` | `promote`); apply as `stage` or `stageAndRedeploy`.

### Draft packs (native share)

- **Draft-native `.draftpack`** files for sharing a service, environment, or whole project across machines (not cloud formats).
- Config only: settings, env vars, project values, canvas layout, optional git settings / source_config / sandbox profiles. No Docker runtime state, routes, port leases, or deployment history.
- **Export omit defaults** (safe cross-machine): strip absolute project path, `git_repo_root`, secret values, app-secret values, bind-mount host paths; keep project-relative `service_root`; Draft volumes export as container-path intent only.
- **Optional include**: secret values, app secrets, bind host paths, sandbox profiles (project scope).
- **Integrity**: export seals a `contentHash` (sha256 of the pack body); import verifies when present (tamper/corruption fails parse).
- **Import**: new project (folder picker) or into existing project/environment; paste JSON or choose file; partial service selection; remap service roots and binds; fill omitted secrets or **link to existing app secrets** (`{{secret.KEY}}`); regenerate node IDs/UIDs; restore `service_link` by label when both sides are in the pack; fidelity report of skipped/needs-attention items.
- **Placement**: layout modes `auto` (offset pack group away from existing nodes; grid when no coords), `preserve`, `grid`. Import UI shows a **canvas mini-map** (existing vs incoming nodes; translucent hatch when stacked) driven by a server-side layout preview so Auto matches what import will do.
- **Multi-env into project**: `flatten` (one target env) or `recreate` (map/create pack environments on the destination project).
- **Start after import**: optional stack start per imported environment (import still succeeds if start fails).
- Unique-field collisions (project name, service labels, host ports, path) are fixable in the import UI with auto-rename defaults (path remains blocking).
- UI: Projects **Import pack**; canvas **Import pack** / **Export pack**; environment switcher **Pack**; service drawer **Pack**.

### Workspace admin views

- **Overview**: live dashboard of projects + activity (mounted).
- **Secrets**, **Volumes**, **Docker**, **Routes**, **Sandboxes**: fully mounted management views.
- **Docker view**: list/start/stop/restart/remove containers; list/remove images (grouped, timestamps); networks; volumes; prune with optional Draft-only filter; bulk selection + confirm dialogs.
- **App settings** (persisted via `GetAppSettings` / `SetAppSettings` and related bindings):
  - Compact sidebar.
  - Local domain preference: `auto` | `public-hostname-port` | `localhost-port`.
  - Reverse-proxy port mode: `prefer80` | `prefer80_fallback` | `custom` (+ primary/fallback ports; default prefers 80 then stable fallback `38473`).
  - Optional machine-local `*.draft` DNS (`local_draft_domain_enabled` / `SetLocalDraftDomainEnabled` with status refresh/verify).

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
- Image tags and container names use the per-node `deployments.sequence` counter **and environment segment** (slug, or `sand-{slug}` for sandboxes), not the global deployment ID.
- Template stamping writes embedded Dockerfiles to the service root (best-effort), resolves `{{draft.*}}` env defaults, seeds volume mounts and DefaultSettings from the template.
- **Linked services** short-circuit deploy: ensure the root is multi-attached rather than building a second container.
- **Rollback** prefers a retained local image (`run-image`); image-mode can `re-pull`; build-mode with a recorded `source_sha` can `rebuild-sha` when the image was GC’d (current settings apply — storage stays lean via `keep_images`).

## Git Trigger Behavior

- Trigger values are `manual`, `on_commit`, and `on_push`.
- Triggers only matter when a node also has a pinned `git_branch`.
- **`redeploy_on_pull`** is orthogonal to `deploy_trigger`: when enabled, Draft installs **`post-merge`** and **`post-rewrite`** hooks that fire `on_pull` after merge-based `git pull` / `git merge`, and after `git pull --rebase` / rebase / amend. Works even when `deploy_trigger` is `manual`.
- Hook installation is per repo, not per node. Draft reference-counts hook need across all nodes in the project.
- Hooks are written into the repo's actual hooks directory using `git rev-parse --git-path hooks`, so `core.hooksPath` and worktrees are honored.
- Draft never overwrites a foreign hook destructively; it preserves and chains to it via `.draft-orig`. `GitHookStatus` reports `pullForeign` when a non-Draft `post-merge` or `post-rewrite` hook exists.
- Hook scripts use cached `git_repo_root` and normalize Windows paths.
- On cold start, hook payloads are spooled then drained; the daemon also reconciles missed commit/push/pull tips by comparing tracked branch SHAs against `deployments.source_sha`.

## Networking

- Each service gets dual hostnames: internal `*.draft.local` (Docker network DNS) and public `*.draft.resolv.sh` (internet-resolvable via Draft's resolver).
  - Normal: `{service}.{project}.{environment}.{uid}.draft.local`.
  - Sandbox: `{service}.{project}.sand.{environment}.{uid}.draft.local`.
- Optional **machine-local `*.draft`** host DNS (separate from Docker’s `*.draft.local`); enable/disable is a privileged local resolver install with verification in Settings.
- **Local domain modes**: preference stored in app settings as `auto` | `public-hostname-port` | `localhost-port`. Exposed via `GetLocalDomainStatus` / `RefreshLocalDomainStatus` (preference applied in bindings).
- **HTTP reverse proxy listen**: configurable port mode (`prefer80`, `prefer80_fallback`, `custom`) so public URLs can stay clean on 80 or stable on a fixed fallback rather than random ephemeral ports.
- HTTP services are proxied by Draft's built-in reverse proxy; TCP services resolve via the hosts file and expose `host:port` endpoints (no HTTP scheme).
- URL selection for deployments prefers the best available public URL (`networking/best_url.go`), protocol-aware for TCP, and aware of local public suffix when `*.draft` is active.
- Linked roots are attached to multiple environment networks so alias hostnames resolve inside each environment’s network.
- Sandbox Docker identity uses `sand-{slug}` as the environment segment in network/image/container/volume names while the environment **slug** itself stays project-unique.

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
- **Mounted workspace views**: Overview, Projects, Templates, Secrets, Volumes, Docker, Routes, Sandboxes, Settings.
- **Sandboxes**: full `SandboxesView` (profiles, live sandboxes, test-run history) plus dialog-only create from a project’s `EnvironmentSwitcher` (“create sandbox” opens over the canvas and can jump into the new sandbox env).
- Settings view **persists** compact sidebar, local-domain preference, reverse-proxy port mode/ports, and local `*.draft` DNS enablement.
- Topbar shows `ActivityTicker` (Docker events) and `DockerIndicator`.
- Service creation uses `CreateServiceDialog` (template picker + wizard); blank services fall back to `CreateNode`.
- Canvas nodes render template icons via `templateId` + `ListServiceTemplates`; volume mounts render as selectable chips; env-reference edges group by node pair.
- Environment chrome: `EnvironmentSwitcher` (switch, create, duplicate “based on”, stack start/stop/redeploy, sync config, create sandbox, delete).
- Import cloud config is available from the app chrome; export from project/service UI.
- Draft packs: import from Projects or canvas; export from canvas (project), environment switcher, or service drawer.

## Sandbox Behavior

- Create always goes through **PreviewSandbox → CreateSandbox**: resolve profile/defaults into an immutable plan, create the environment + sandbox row first (so identity helpers know the env is a sandbox), then duplicate selected services with copy/share/omit rules, then pin git-backed copies from `plan.repositories`.
- **StartOnCreate** (preview UI default) runs environment stack start after materialize; materialize success is returned even if start fails (`SandboxCreateResult.startError`). Testing sandboxes start via their own suite path instead.
- Failed mid-create cleanup disconnects shared-root attaches, removes the sandbox Docker network, and deletes the environment.
- **DeleteSandbox** is more destructive than normal environment delete: it removes Draft-managed volumes as well as containers/routes/network. Bind mounts and non-Draft volumes are not selected by this path. Test-run history rows are retained (live `sandbox_id` pointer cleared).
- **Extend** recalculates warning/grace from the sandbox’s frozen plan, not from a profile that may have changed since create.
- **Refresh tip / same SHA** rewrites repository pins and redeploys sandbox copies only; durable source environments are never modified.
- GitHub PR listing is local-only via `gh` and is hidden per repository when the CLI/auth/remote check fails (branch/ref mode always available).
- Testing steps run inside sandbox service containers (by service label in the sandbox env). Suite pass/fail is on the test-run record; sandbox lifecycle status is separate.
- Suspended sandboxes still expire and purge after grace so stopped previews are not left forever.
- Source pins use committed git objects (`git archive` / SHA); dirty working-tree changes are not included unless committed.

## Not Yet Implemented

- Branch/worktree UX beyond the current pinned-ref deploy path (sandboxes can pin per-repo refs at create, but there is no first-class worktree workspace model).
- Explicit depends-on edges beyond inferred `@{Service…}` connection waves for stack start (graph is derived from env refs today).
- Secrets-at-rest encryption (deferred; local desktop threat model similar to a committed `.env` once the filesystem is owned).
- Shell WebSocket auth without putting a token in the query string (token can appear in DevTools / local process lists).

## Conventions And Constraints

- Prefer the live mounted path over adjacent placeholder components.
- Docker labels are the runtime source of truth and daemon reconcile matters on startup (including service-link networks and sandbox lifecycle).
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
  - sandbox vs durable env: `sand` hostname label + `sand-{slug}` Docker segment
  - link mode: root container vs alias (linked) node
  - config layer: applied vs staged vs immediate settings
  - sandbox plan layer: profile defaults vs request overrides vs frozen `PlanJSON`
