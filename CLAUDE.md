# Draft

A native desktop app (Wails v2: Go backend + React frontend) that provides a visual, drag-and-drop
canvas for managing local Docker services during development. Think "local Railway" — dynamic port
management, automatic .env sync, and a visual topology graph, all running on macOS/Windows without
needing a hosted Linux server.

## Tech stack

| Layer | Choice |
|-------|--------|
| Framework | Wails v2 (Go 1.25 + native webview) |
| Frontend | React 18 + TypeScript + Vite, @xyflow/react (React Flow) for canvas |
| Backend | Go, Docker SDK (`github.com/docker/docker`), GORM |
| Storage | `modernc.org/sqlite` via `glebarez/sqlite` (pure Go, no CGO) |
| Icons | lucide-react |

## Architecture

```
┌─────────────┐       IPC (HTTP + token)       ┌──────────────────┐
│  Wails App  │ ◄────────────────────────────► │     Daemon       │
│  (frontend) │                                 │  (--daemon flag) │
└─────────────┘                                 └────────┬─────────┘
                                                         │
                                         ┌───────────────┼───────────────┐
                                         │               │               │
                                    Store (SQLite)  Deploy Engine   Networking
                                                    (Docker SDK)   (reverse proxy)
```

**Daemon** — background process owning the SQLite store, Docker orchestration engine, SSE event hub,
and HTTP API server (token-authenticated). Reconciles Docker container state on startup via labels.

**Frontend ↔ Backend** — Wails IPC for bound methods + daemon HTTP API for long-running ops (deploy,
logs, metrics). Events delivered via SSE stream and Wails `EventsOn`.

## Project layout

```
app.go                    # Main Wails-bound App struct (all frontend-callable methods)
main.go                   # Entrypoint, daemon flag handling
deployments.go, docker.go, nodes.go, services.go, env.go,
metrics.go, projects.go, node_settings.go, local_domain.go, url.go
                          # Wails-bound method files (thin wrappers → daemon client)

internal/
  store/                  # SQLite via GORM — models, CRUD (projects, nodes, deployments, env_vars, routes, port_leases, node_settings)
  daemon/                 # HTTP server + client, SSE events, reconciliation, idle timeout
  deploy/                 # Docker build & run engine, metrics collection, reachability probes
  networking/             # Reverse proxy, hostname routing, hosts file management, port leases
  dockerwatch/            # Docker event stream hub (event-driven, no polling)
  dockerfile/             # EXPOSE directive parser
  envfile/                # .env file read/write with conflict detection
  ignore/                 # .dockerignore / .gitignore filtering

frontend/src/
  App.tsx                 # Root: sidebar nav, project selection, event subscriptions
  views/                  # ProjectsView, OverviewView, SandboxesView, SettingsView
  components/
    ProjectCanvas.tsx     # React Flow graph (drag-and-drop nodes)
    ServiceNode.tsx       # Custom node renderer
    NodeDetailPanel.tsx   # Right-side panel with tabs:
      OverviewTab.tsx       # Deploy/stop/restart, build log viewer
      DeploymentsTab.tsx    # Deployment history timeline
      VariablesTab.tsx      # Env var CRUD, .env import/export/sync
      LogsTab.tsx           # Real-time container logs
      MetricsTab.tsx        # CPU, memory, network graphs, reachability
      SettingsTab.tsx       # Dockerfile, root path, port, volumes, labels
    Sidebar.tsx, DockerIndicator.tsx, ActivityTicker.tsx,
    CreateProjectDialog.tsx, StatusBadge.tsx, ServicePill.tsx, ...
  lib/
    dashboardData.ts      # Status colors, relative time, project decoration
    logStreamManager.ts   # Log stream subscription management
```

## Database tables (GORM auto-migrated)

| Table | Purpose |
|-------|---------|
| `projects` | Registered projects (name, path, description) |
| `canvas_nodes` | Visual nodes on canvas (project_id, label, x, y) |
| `deployments` | Build+run cycles (status, image_tag, container_id, timestamps, exit_code) |
| `env_vars` | Per-node env vars (key, value, scope: runtime/build/both, source) |
| `routes` | Hostname → container mappings (protocol, target_host/port, host_port) |
| `port_leases` | Cross-project host port authority |
| `node_settings` | Extensible KV config per node |

## What's implemented

- Project creation & management
- Canvas with drag-and-drop nodes (React Flow)
- Service deployment: Docker build + run with streaming build logs
- Service lifecycle: stop, restart, status tracking
- Deployment history with timeline UI
- Environment variables: manual edit, .env import/export, scope (runtime/build/both), conflict detection
- Live container logs (streaming)
- Service metrics: CPU, memory, network I/O, uptime, reachability probes
- Docker daemon status indicator + activity ticker
- Port management (cross-project authority, auto-assignment)
- Node settings: dockerfile path, service root, ports, volumes, labels
- Local domain routing infrastructure (hostname → container reverse proxy)
- Upload progress bar (event-driven)

## Not yet implemented

- Sandboxes (ephemeral environments, fork/branch workflows)
- Git branch pinning & virtual checkouts (planned: `git archive` for ephemeral, `git worktree` for persistent)
- Dynamic variable linking (`${{ service.VAR }}` Railway-style references)
- Overview dashboard (placeholder only)
- App-level settings persistence (UI exists, backend not wired)

## Conventions

- **No CGO on Windows** — all deps must be pure Go to keep the Windows build CGO-free.
- **macOS** — CGO required (WebKit binding); universal binary via `wails build -platform darwin/universal`.
- **Docker labels** are runtime source-of-truth; reconciled on daemon startup.
- **Event-driven** — prefer Docker event streams and SSE over polling.
- Go may not be on PATH in fresh shells on Windows — prepend standard Go bin paths when needed.
