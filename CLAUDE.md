# Draft

Draft is a native desktop app built with Wails v2 (Go backend + React frontend). It manages local Docker services as a visual workspace: projects contain service nodes on a canvas, Draft builds and runs those services, assigns ports, injects environment wiring, and exposes stable local hostnames.

## Tech Stack

| Layer | Choice |
|-------|--------|
| Desktop shell | Wails v2 |
| Backend | Go 1.25 |
| Frontend | React 18 + TypeScript + Vite |
| Canvas | `@xyflow/react` |
| Storage | GORM + `glebarez/sqlite` / `modernc.org/sqlite` |
| Docker integration | Docker SDK + optional `docker buildx` CLI path |
| Icons | `lucide-react` |

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
- The Wails app binds thin methods in top-level `*.go` files and talks to the daemon for long-running work.
- The daemon owns the store, deployment engine, Docker watch hub, local routing/proxy, and SSE event stream.
- The daemon is single-instance. It writes state (addr/token/pid), reuses an existing healthy daemon, and idles out after inactivity.

## Current Project Layout

```text
main.go                    # Wails app startup, daemon mode, git-hook mode
app.go                     # App lifecycle, daemon bootstrap, event bridge
projects.go, nodes.go, services.go, deployments.go,
env.go, metrics.go, docker.go, node_settings.go,
local_domain.go, url.go, git_triggers.go
                          # Wails-bound methods

internal/
  daemon/                 # HTTP API, SSE hub, daemon lifecycle, git-trigger reconcile
  deploy/                 # Build/run engine, metrics, env resolution, BuildKit/legacy paths
  dockerwatch/            # Docker daemon health and event watching
  envfile/                # .env import/export/refresh helpers
  dockerfile/             # Dockerfile EXPOSE parser
  gitsrc/                 # Branch/ref export via git archive
  githooks/               # post-commit / pre-push hook install and chaining
  ignore/                 # .dockerignore / .gitignore matching
  networking/             # Port leases, proxy, hosts/domain routing
  store/                  # GORM models, CRUD, auto-migration

frontend/src/
  App.tsx                 # Mounted shell; inline Overview/Sandboxes placeholders, Projects/Settings routing
  components/
    Sidebar.tsx           # Left nav + current brand mark
    ProjectCanvas.tsx     # Canvas, read-only env-reference edges, node selection
    NodeDetailPanel.tsx   # Service detail drawer with tabs
    OverviewTab.tsx       # Deploy/stop/restart + build output + URLs
    DeploymentsTab.tsx    # Deployment history
    VariablesTab.tsx      # Env vars, previews, linking, .env sync
    LogsTab.tsx           # Live container logs
    MetricsTab.tsx        # CPU/memory/network/reachability
    SettingsTab.tsx       # Service build/runtime/network/security settings
  views/
    ProjectsView.tsx      # Project list / project cards
    SettingsView.tsx      # App settings design pass only; local UI state, no backend persistence
```

## Data Model

Primary store tables:

- `projects`
- `canvas_nodes`
- `deployments`
- `env_vars`
- `routes`
- `port_leases`
- `node_settings`

Important model details:

- `canvas_nodes.uid` is the stable per-node hostname suffix.
- `deployments.source_sha` records the commit built for pinned git-branch deploys.
- `node_settings` is the extensible feature surface; most per-service behavior is driven by KV settings rather than schema changes.

## What Is Implemented

- Project creation and project listing.
- Canvas nodes with persisted positions, rename, delete, and add-service flow.
- Docker build + run deployments with streaming build logs.
- Deployment history, active deployment lookup, stop, restart, and cancel-build behavior.
- Docker status indicator and daemon-backed activity/event updates.
- Live container logs and service metrics.
- Port leasing plus local hostname/routing support.
- Service root and Dockerfile selection, including EXPOSE parsing.
- `.env` import, export, refresh, conflict reporting, and suggested env-file path.
- Dynamic env references between services using `@{Service.ATTR}`-style links, preview resolution, and read-only connection edges on the canvas.
- Draft-injected runtime vars such as `DRAFT_INTERNAL_URL`, `DRAFT_PUBLIC_URL`, and related service/project identity values.
- Deploy-from-git for pinned branches/refs without touching the working tree.
- Automatic redeploy triggers on commit or push via local git hooks, with chaining to pre-existing foreign hooks.
- Rich per-service settings for build, runtime command, restart policy, health checks, resource limits, volume mounts, lifecycle hooks, security flags, and custom labels.

## Build And Deploy Behavior

- Default source mode is the working tree on disk.
- If `git_branch` is set, Draft deploys committed content from that ref instead of the live working tree.
- Pinned git deploys have two modes:
  - Stream mode (`git_stream` default on): pipe `git archive` output directly to Docker. Fastest, but `.dockerignore`, `.gitignore`, and BuildKit local-context behavior do not apply.
  - Checkout mode (`git_stream=false`): export the ref into a temp directory and build from that on-disk workspace. Slower, but it can honor ignore rules and BuildKit local-context behavior.
- BuildKit local-context is optional and best-effort. Draft uses `docker buildx build --load` only when the current settings are compatible with the legacy context semantics.
- If BuildKit local-context would change ignore behavior, widen the context incorrectly, or otherwise diverge from Draft's legacy tar path, Draft logs the reason and falls back to the legacy Docker SDK upload path.
- `.gitignore`-based context filtering blocks the BuildKit local-context path entirely.
- Root `.dockerignore` compatibility is required before BuildKit local-context is used when Draft's `.dockerignore` toggle is on.

## Git Trigger Behavior

- Trigger values are `manual`, `on_commit`, and `on_push`.
- Triggers only matter when a node also has a pinned `git_branch`.
- Hook installation is per repo, not per node. Draft reference-counts hook need across all nodes in the project.
- Hooks are written into the repo's actual hooks directory using `git rev-parse --git-path hooks`, so `core.hooksPath` and worktrees are honored.
- Draft never overwrites a foreign hook destructively; it preserves and chains to it via `.draft-orig`.
- On startup, the daemon reconciles missed commit/push events by comparing tracked branch SHAs against `deployments.source_sha`.

## Frontend Reality

- `frontend/src/App.tsx` is the real mounted shell.
- Overview and Sandboxes are intentionally placeholder empty states in the mounted app, even though `frontend/src/views/OverviewView.tsx` and `SandboxesView.tsx` exist.
- `SettingsView.tsx` is also intentionally non-persistent today; its controls are local-only design scaffolding until a backend contract exists.
- The main user workflow today is Projects list -> open project canvas -> use the node detail panel tabs.

## Not Yet Implemented

- Real sandboxes / ephemeral environments.
- Branch/worktree UX beyond the current pinned-ref deploy path.
- A real Overview dashboard.
- Persistent app-level settings backend for `SettingsView`.

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
