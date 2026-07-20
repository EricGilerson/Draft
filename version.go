package main

// appVersion is set by the release workflow with -ldflags. Keep the default
// useful for local builds, where Wails' packaging version is not available at
// runtime.
var appVersion = "0.1.0-dev"

// updaterPublicKey is the base64 raw Ed25519 public key used to verify release
// manifests. It is deliberately public and is injected at release build time.
// An empty key disables updates in development builds.
var updaterPublicKey = ""
