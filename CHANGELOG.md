# Changelog

All notable changes to Draft are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

When cutting a desktop release, add a `## [x.y.z] - YYYY-MM-DD` section for that
version. The Release Desktop workflow copies that section into the GitHub
Releases page body.

## [Unreleased]

## [0.0.8] - 2026-09-21

### Fixed

- Git-branch deploys on Windows no longer rewrite shell scripts to CRLF inside
  `git archive` / detached worktree checkouts. Hosts with `core.autocrlf=true`
  previously produced `#!/bin/sh\r` shebangs, so containers failed readiness
  with exit 255 (`exec …: no such file or directory`) while the previous
  deployment was left running. Draft now forces `core.autocrlf=false` on every
  archive and build-context worktree path.
- Git-branch deploys that fall back to a worktree + `submodule update --init`
  now pass `-c protocol.file.allow=always` on that Draft-owned command only.
  Monorepos and sibling-repo layouts with local/relative submodule URLs no
  longer fail on Git 2.38+ with `transport 'file' not allowed` when module
  objects are not already cached. The user's git config is left unchanged.
- Draft pack import and deploy keep template identity secrets aligned when a
  service gets a new UID. Generated env (e.g. `REDIS_URL`) was already
  re-derived from `{{draft.password}}` at deploy, but `cmd_override` (e.g.
  Redis `--requirepass`) stayed on the pack’s old expanded password — Celery
  then failed with `invalid username-password pair`. Import now restamps
  template-owned env + command fields, and deploy rehydrates those command
  settings from the template against the current node identity.

### Changed

- Bump desktop product version to 0.0.8

## [0.0.7] - 2026-09-18

### Fixed

- Draft pack export rewrites an absolute Dockerfile path under the project as
  project-relative (same policy as `service_root`), so another machine is not
  left with `/Users/.../Dockerfile`. Paths outside the project are omitted.

### Changed

- Bump desktop product version to 0.0.7

## [0.0.6] - 2026-09-18

### Fixed

- MinIO template now creates the `AWS_BUCKET` (`app`) on first start so S3
  clients are not left with NoSuchBucket
- MinIO template stamps `AWS_ALLOW_HTTP=true` and path-style addressing so
  LanceDB / rust object_store clients can use the `http://` S3 endpoint
  (bucket create alone still left those clients with BadScheme)

### Changed

- Bump desktop product version to 0.0.6
- MinIO reverse-proxies the S3 API (port 9000) and stamps `S3_PUBLIC_ENDPOINT`
  from `{{draft.public_url}}` so browsers can use the same hostname path as
  other HTTP services. Sibling containers still use `S3_ENDPOINT` on the
  Docker network. The console remains on container port 9001 (not proxied).

## [0.0.5] - 2026-09-18

### Fixed

- Blank services can start their first deploy after staging a Dockerfile or image
  and port (draft bar Deploy, Overview, and Stage & deploy)
- Stopped services with staged settings rebuild instead of resuming the old
  container, so staged config actually applies
- Environment stack and sandbox start wait on staged dockerfile/image+port
  instead of treating those services as non-deployable

### Changed

- Picking a Dockerfile with `EXPOSE` fills the container port when none is set
- MinIO built-in image is now `coollabsio/minio:latest`, with `AWS_BUCKET=app`
  stamped for S3 clients
- MongoDB built-in image is now `mongo:8`, with a single `/data/db` volume
  (no unused `/data/configdb` mount)
- Bump desktop product version to 0.0.5

## [0.0.4] - 2026-07-22

### Changed

- Bump desktop product version to 0.0.4

## [0.0.3] - 2026-07-22

### Added

- Desktop auto-update improvements for Windows and macOS, including signed
  update manifests and broader updater test coverage

### Fixed

- Deployment input scope constraint handling
- Canceled deployment image cleanup
- macOS updater test bundle signing
- Release workflow compatibility on Windows runners

### Changed

- More reliable detached command execution across platforms
- Signing and Authenticode verification scripts

## [0.0.2] - 2026-07-21

### Added

- Paginated deployment history in the Deployments tab
- Git commit hash recorded on deployments
