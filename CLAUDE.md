# Draft

Draft is a native desktop app built with Wails v2 (Go backend + React frontend). It manages local Docker services as a visual workspace: projects contain service nodes on a canvas, Draft builds and runs those services, assigns ports, injects environment wiring, and exposes stable local hostnames.

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
- On app startup, the daemon reconciles Docker state, missed git-trigger events, and installed hooks (`githooks.ReconcileAllHooks`).

## Current Project Layout

```text
main.go                    # Wails app startup, daemon mode, git-hook mode
app.go                     # App lifecycle, daemon bootstrap, event bridge, native dialogs
bindings.go                # Wails-bound methods (thin delegates)
cgo_darwin.go              # macOS cgo linker flags for Wails/WebKit
wails.json                 # Wails project config
build/                     # Wails packaging assets (darwin/, windows/, installer)
scripts/kill-daemon.sh     # Dev helper to stop the background daemon

internal/
  daemon/                  # HTTP API, SSE hub, daemon lifecycle, git-trigger reconcile
    paths.go               # Config dir + daemon.json state path
    gittrigger.go          # Hook fire + /hooks/recheck
    client.go              # Daemon HTTP client used by Wails bindings
  deploy/                  # Build/run engine, metrics, env resolution, service projection
    engine.go              # Deploy orchestration (build, image-pull, git-sourced build)
    image_deploy.go        # Image-pull deploy path (no Docker build)
    stamp.go               # CreateNodeFromTemplate stamping
    template_expr.go       # {{draft.*}} expression expansion at stamp time
    volumes.go             # Volume specs + Draft-managed Docker volumes
    refs.go                # @{Service.ATTR} reference resolution
    project_services.go    # ListProjectServices projection for project cards
    settings.go            # Node-setting keys and defaults
  dockerdesktop/           # Best-effort local Docker Desktop launcher
  dockerfile/              # Dockerfile EXPOSE parser
  dockerwatch/             # Docker daemon health and event watching
  envfile/                 # .env import/export/refresh helpers
  githooks/                # post-commit / pre-push / post-merge hook install + chaining
  gitsrc/                  # Branch/ref export and ref/SHA helpers via git
  ignore/                  # .dockerignore / .gitignore matching
  networking/              # Port leases, proxy, hosts/domain routing, URL selection
    hostname.go            # Internal *.draft.local + public *.draft.resolv.sh hostnames
    router.go              # Local domain modes, GetLocalDomainStatus
    hosts.go               # Hosts-file block management
  store/                   # GORM models, CRUD, auto-migration, validation helpers
    templates.go           # Template CRUD + seeding
    templates_builtin.go   # Built-in template definitions
    template_schema.go     # Wizard/settings schema model
    image_tags.go          # Curated image tag JSON helpers
    volumes.go             # TemplateVolume JSON normalization
    git_repo.go            # Cached git_repo_root per node

frontend/src/
  App.tsx                  # Mounted shell; BuildLogProvider wrapper; nav + topbar
  lib/
    dashboardData.ts       # Project summaries for ProjectsView
    logStreamManager.ts    # Ref-counted log stream lifecycle
  utils/
    imageRef.ts            # Image tag parsing for templates
  components/
    Sidebar.tsx            # Left nav (overview/projects/templates/sandboxes/settings)
    ActivityTicker.tsx     # Docker activity feed in topbar
    DockerIndicator.tsx    # Docker status in topbar
    BuildLogProvider.tsx   # Global build log + upload progress state
    CreateProjectDialog.tsx
    CreateServiceDialog.tsx  # Template picker + multi-step wizard
    ProjectCanvas.tsx      # Canvas, env-reference edges, ServiceNode, ResizablePanel drawer
    ServiceNode.tsx        # Custom canvas node with template icon
    NodeDetailPanel.tsx    # Service detail drawer with tabs
    OverviewTab.tsx        # Deploy/stop/restart + build output + URLs
    DeploymentsTab.tsx     # Deployment history
    VariablesTab.tsx       # Env vars, previews, linking, .env sync
    LogsTab.tsx            # Live container logs
    MetricsTab.tsx       # CPU/memory/network/reachability
    SettingsTab.tsx        # Service build/runtime/network/security settings
    TemplateEditorDialog.tsx
    TemplateIcon.tsx
    VolumeEditor.tsx       # Volume mount editor (wizard + settings)
    Dialog.tsx, EmptyState.tsx, PageHeader.tsx, StatusBadge.tsx, ServicePill.tsx
  views/
    ProjectsView.tsx       # Project list / project cards with live service status
    TemplatesView.tsx      # Template library (mounted, fully functional)
    SettingsView.tsx       # App settings design pass only; local UI state, no backend persistence
    OverviewView.tsx       # Rich mock dashboard UI; NOT mounted
    SandboxesView.tsx      # Mock sandbox cards; NOT mounted
  wailsjs/                 # Generated Wails bindings (do not hand-edit)
```

There is no root project README beyond a one-line `README.md`; `README.wails.md` is stock Wails template text.

## Data Model

Primary store tables:

- `projects`
- `canvas_nodes`
- `service_templates`
- `deployments`
- `env_vars`
- `routes`
- `port_leases`
- `node_settings`

Important model details:

- `canvas_nodes.uid` is the stable per-node hostname suffix.
- `canvas_nodes.templateId` links a node to the `ServiceTemplate` it was created from (0 = blank/legacy node; not a foreign key).
- `deployments.source_sha` records the commit built for pinned git-branch deploys.
- `deployments.sequence` is the per-node deploy counter used in image tags and container names.
- `env_vars` carry `scope` (runtime/build/both), `secret`, `source`, and `envFile`.
- `node_settings` is the extensible feature surface; most per-service behavior is driven by KV settings rather than schema changes. Important keys include `dockerfile`, `image`, `service_port`, `service_root`, `git_branch`, `git_stream`, `deploy_trigger`, `redeploy_on_pull`, `git_repo_root`, `volume_mounts`, `use_buildkit_local_context`, `use_dockerignore`, `use_gitignore`, plus all settings-tab keys in `deploy/settings.go`.
- `service_templates` hold reusable blueprints (built-in + user-created) with embedded Dockerfiles, default env vars, volume defaults, and a JSON schema that drives the create wizard and Settings tab section visibility.
- Volume mounts are stored as `volume_mounts` JSON in `node_settings`, not a separate SQL table. Draft-managed named volumes are Docker objects labeled `draft.managed=true`.

## What Is Implemented

- Project creation and project listing with live per-service status on project cards (`ListProjectServices`).
- Canvas nodes with persisted positions, rename, delete, and add-service flow via template wizard or blank node.
- **Service templates**: built-in library (Next.js, Node.js, FastAPI, Flask, Vite, PostgreSQL, Redis, MySQL, MongoDB), user create/edit/clone/delete, template editor UI, and `CreateNodeFromTemplate` stamping (settings, env vars, optional Dockerfile, volumes).
- **`{{draft.*}}` template expressions** in template defaults (password, hostname, port, etc.) resolved at stamp time.
- **Image-mode deploy**: when `image` is set and `dockerfile` is empty, Draft pulls and runs without building.
- Docker build + run deployments with streaming build logs and upload progress events.
- Deployment history, active deployment lookup, stop, restart, and cancel-build behavior.
- Docker status indicator, activity ticker, and daemon-backed activity/event updates.
- Live container logs and service metrics with port-based reachability checks.
- Port leasing plus local hostname/routing support (internal + public hostnames).
- **Volume mounts**: bind mounts and Draft-managed named volumes (`draft-{projectId}-...`), with list/delete bindings and UI editor.
- Service root and Dockerfile selection, including EXPOSE parsing.
- `.env` import, export, refresh, conflict reporting, and suggested env-file path.
- Dynamic env references between services using `@{Service.ATTR}`-style links, preview resolution, and read-only connection edges on the canvas.
- Draft-injected runtime vars: `DRAFT_SERVICE_PORT`, `DRAFT_INTERNAL_HOSTNAME`, `DRAFT_INTERNAL_URL`, `DRAFT_PUBLIC_HOSTNAME`, `DRAFT_PUBLIC_URL`, `DRAFT_SERVICE_NAME`, `DRAFT_PROJECT_NAME`, `DRAFT_ENVIRONMENT` (plus deprecated aliases `DRAFT_PORT`, `DRAFT_HOSTNAME`).
- Deploy-from-git for pinned branches/refs without touching the working tree.
- Automatic redeploy triggers on commit or push via local git hooks, with chaining to pre-existing foreign hooks.
- **Redeploy on pull**: independent `redeploy_on_pull` setting installs a `post-merge` hook for merge-based `git pull` (orthogonal to `deploy_trigger`).
- Git repo-root awareness: cached `git_repo_root` per node for multi-repo projects; hook scripts normalize Windows paths.
- Duplicate service names rejected (normalized label uniqueness).
- Rich per-service settings for build, runtime command, restart policy, health checks, resource limits, volume mounts, lifecycle hooks, security flags, and custom labels. Template schema can hide irrelevant Settings sections (e.g. Dockerfile for image-mode datastores).
- Per-project Docker networks (`draft-{project}-{environment}`).
- Native folder/file picker dialogs (`SelectFolder`, `SelectFile`).

## Build And Deploy Behavior

Draft has three deploy paths, selected by node settings:

1. **Build mode** (`dockerfile` + `service_port`): Docker build from working tree or git ref, then run.
2. **Image mode** (`image` + `service_port`, no `dockerfile`): `docker pull` + run — used by datastore templates and custom image services.
3. **Git-sourced build**: same as build mode but source comes from a pinned ref instead of the working tree.

Shared tail for all paths: resolve env, attach volumes, join project network, register routes, start container.

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
- Image tags and container names use the per-node `deployments.sequence` counter, not the global deployment ID.
- Template stamping writes embedded Dockerfiles to the service root (best-effort), resolves `{{draft.*}}` env defaults, and seeds volume mounts from template defaults.

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

- Each service gets dual hostnames: internal `*.draft.local` (Docker network DNS) and public `*.draft.resolv.sh` (internet-resolvable via Draft's resolver).
- **Local domain modes**: `public-hostname-port` (default) vs `localhost-port`. Exposed via `GetLocalDomainStatus`.
- HTTP services are proxied by Draft's built-in reverse proxy; TCP services resolve via the hosts file.
- URL selection for deployments prefers the best available public URL (`networking/best_url.go`).

## Frontend Reality

- `frontend/src/App.tsx` is the real mounted shell, wrapped in `BuildLogProvider`.
- Default nav view is **Overview** (inline placeholder). Main workflow: Projects → open project canvas → node detail panel tabs.
- **Templates** nav routes to `TemplatesView` (fully implemented template library).
- Overview and Sandboxes are intentionally placeholder empty states in the mounted app, even though `OverviewView.tsx` and `SandboxesView.tsx` exist as richer design mocks.
- `SettingsView.tsx` is also intentionally non-persistent today; its controls are local-only design scaffolding until a backend contract exists.
- Topbar shows `ActivityTicker` (Docker events) and `DockerIndicator`.
- Service creation uses `CreateServiceDialog` (template picker + wizard); blank services fall back to `CreateNode`.
- Canvas nodes render template icons via `templateId` + `ListServiceTemplates`.

## Not Yet Implemented

- Real sandboxes / ephemeral environments.
- Branch/worktree UX beyond the current pinned-ref deploy path.
- A real Overview dashboard (wired to live data).
- Persistent app-level settings backend for `SettingsView`.
- "Re-apply template" action (templateId is stored for future use).

## Conventions And Constraints

- Prefer the live mounted path over adjacent placeholder components.
- Docker labels are the runtime source of truth and daemon reconcile matters on startup.
- Prefer event-driven updates (SSE / Docker events) over polling.
- Keep Windows compatibility in mind; the repo is intentionally using pure-Go SQLite to avoid CGO on Windows.
- macOS still requires CGO for the Wails/WebKit build.
- When reasoning about build regressions in this repo, separate:
  - source mode: working tree vs pinned git ref
  - transport mode: BuildKit local-context vs legacy tar upload
  - ignore semantics: Draft filters vs `.dockerignore` / `.gitignore`
  - deploy path: build vs image-pull vs git-sourced build
