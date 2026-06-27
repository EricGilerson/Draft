# Draft

> A visual, drag-and-drop **"Local Railway"** for managing local Docker infrastructure on macOS and Windows.

## What we're building

Draft is a native desktop app (Wails: Go backend + React frontend) that gives developers a
[Railway](https://railway.app)-style visual canvas for spinning up and managing **local** Docker
services during development. Think of it as the local-first answer to Railway/Render/Coolify — but
where those target hosted Linux VPS environments, Draft is explicitly designed to run a real
development deployment environment on a developer's own **Mac or Windows** machine.

The core insight: tools like Coolify only really work on hosted Linux servers — you can't comfortably
stand up a full dev environment on Mac/Windows with them. Docker Compose works locally but is static,
file-driven, and has no visual model, no dynamic port arbitration, and no environment/branch concept.
Draft fills that gap with a visual, dynamic, local orchestration layer.

> **Important framing:** This is *not* primarily about giving AI agents sandbox access (though that's
> possible). The point is **local development deployment** — making it trivial to run, fork, and
> manage real service topologies on your own machine.

## Core features (product vision)

- **Visual node canvas** — services, databases, and their connections rendered as a drag-and-drop
  graph using [React Flow](https://reactflow.dev).
- **Dynamic port management** — Draft is the authority on host ports. If a desired port is taken, it
  auto-reassigns to a free one and propagates the change everywhere it's referenced. No more manual
  port-conflict whack-a-mole.
- **Automatic `.env` synchronization** — resolved connection values (ports, URLs, credentials) are
  written into the local project folder's `.env` automatically, so the developer's app picks them up
  with zero manual copying.
- **Dynamic variable linking** — Railway-style references (e.g. `${{ postgres.DATABASE_URL }}`) that
  resolve at deploy time, so changing one service's port/host updates every dependent automatically.
- **Ephemeral testing sandboxes** — throwaway environments for testing that tear down cleanly.
- **Persistent / non-ephemeral environments** — long-lived environments that survive restarts.
- **Fork to sandbox** — snapshot an environment's topology + state into a new isolated sandbox
  (lineage tracked), like a branchable copy of your whole local stack.
- **Attach branches to deployments** — pin a git branch/ref to a deployment **without** mutating the
  developer's working tree (see "Git strategy" below).

## Tech stack & key decisions

| Area | Choice | Why |
|------|--------|-----|
| Shell | **Wails v2** (Go + native webview) | Small binaries, native feel, Go backend for Docker orchestration. Chosen over Tauri/Electron. |
| Frontend | React + TypeScript + Vite | Wails `react-ts` template. |
| Canvas | **React Flow** | Node/edge graph for the visual topology. |
| Backend | **Go** (1.23+) | Native Docker SDK access, single binary. |
| Docker | `github.com/docker/docker/client` (Go SDK) | Programmatic orchestration. |
| Storage | **`modernc.org/sqlite`** (pure Go, no CGO) | Keeps the Windows build CGO-free. Avoid `mattn/go-sqlite3` (CGO). |
| Config | Per-project JSON/YAML manifest (declarative topology) | Source of intent. |
| Runtime state | **Docker labels** as source-of-truth, reconciled on startup | Survives app restarts; Docker is the truth. |

### Storage model (SQLite tables)

- `projects` — registered projects and their manifests.
- `port_leases` — cross-project port authority (who owns which host port).
- `sandboxes` — sandbox lineage / fork tracking.
- `settings` — app-level settings.

Avoid: KV stores (bbolt/Badger), server DBs, Redis-as-app-state. SQLite + manifests + Docker labels
covers declarative intent, queryable cross-project state, and durable runtime truth.

## Git strategy — "virtual checkout"

Deployments can be pinned to a git branch/ref, but a plain `git checkout` mutates the developer's HEAD,
working tree, and index — which we must never do. Instead Draft materializes a ref's contents
elsewhere:

- **Snapshot (ephemeral / build context)** — `git archive <ref>` piped straight into
  `docker build -t img -f Dockerfile -` (zero temp dir, no working-tree mutation, no branch lock; the
  same branch can feed many deployments simultaneously). Lowest-level alternative:
  `read-tree`/`checkout-index` with a throwaway `GIT_INDEX_FILE`.
- **Live, mutable, git-aware deployment** — `git worktree add <path> <branch>` (use `--detach` so the
  same ref can appear in multiple deployments without the one-branch-per-worktree lock). Draft owns
  the worktree lifecycle (add/prune/remove).
- **In-process option** — `go-git` (pure Go, fits the CGO-free stack) can materialize any ref's tree
  into an `osfs` directory or in-memory `memfs` without shelling out or touching the working tree.

Rule of thumb: ephemeral build/test → `git archive | docker build -`; persistent editable
deployment → `git worktree`.

## Cross-platform builds

- **No cross-compilation** — Wails builds each OS on its own platform.
- **Windows** — pure-Go WebView2Loader, **no CGO needed** (keep it that way; pick pure-Go deps).
- **macOS** — requires CGO (clang) for the native WebKit binding. A single universal binary is
  achievable: `wails build -platform darwin/universal` (arm64 + amd64, ~2× size).
- **CI** — a GitHub Actions matrix (`windows-latest` + `macos-latest`), or
  `dAppServer/wails-build-action`, can produce both artifacts without owning both machines.

## Current state of the codebase

- Wails `react-ts` project scaffolded into the pre-existing repo (`.git` + original `README.md`
  preserved; template README moved to `README.wails.md`).
- `wails.json` — name/outputfilename `Draft`, author `ericgilerson@gmail.com`.
- `go.mod` — `module Draft`, `go 1.23.0`, Wails v2.12.0.
- `frontend/src/App.tsx` + `App.css` — default demo removed; replaced with a left sidebar (title
  "Draft") + a single "Dashboard" nav button that shows "Hi 👋" when active. Placeholder layout only.
- `app.go` — still has the default bound `Greet(name string)` method (unused by the frontend; safe to
  remove later).
- `build/bin/Draft.exe` — last successful build artifact.

## Conventions / environment notes

- Go is not always on PATH in fresh shells — prepend `C:\Program Files\Go\bin` and
  `%USERPROFILE%\go\bin` when needed.
- Keep all dependencies **pure Go where possible** to preserve the CGO-free Windows build.
- PowerShell may surface Go's stderr download progress as "NativeCommandError" noise — usually benign
  when the command otherwise succeeds.
