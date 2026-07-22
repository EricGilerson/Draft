# Changelog

All notable changes to Draft are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

When cutting a desktop release, add a `## [x.y.z] - YYYY-MM-DD` section for that
version. The Release Desktop workflow copies that section into the GitHub
Releases page body.

## [Unreleased]

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
