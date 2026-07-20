// Package appupdate stages and applies signed Draft desktop updates.
package appupdate

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	githubLatestURL = "https://api.github.com/repos/EricGilerson/Draft/releases/latest"
	manifestName    = "draft-update-manifest.json"
	signatureName   = "draft-update-manifest.sig"
)

type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

type githubRelease struct {
	TagName string  `json:"tag_name"`
	Assets  []Asset `json:"assets"`
}

type PlatformAsset struct {
	Asset  string `json:"asset"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type Manifest struct {
	Version   string                   `json:"version"`
	Channel   string                   `json:"channel"`
	Notes     string                   `json:"notes"`
	Platforms map[string]PlatformAsset `json:"platforms"`
}

// Status is safe to expose directly to the Wails frontend.
type Status struct {
	State   string `json:"state"` // disabled, checking, downloading, ready, unavailable, failed
	Version string `json:"version,omitempty"`
	Message string `json:"message,omitempty"`
	Notes   string `json:"notes,omitempty"`
}

type persisted struct {
	Status   Status `json:"status"`
	Artifact string `json:"artifact,omitempty"`
	Platform string `json:"platform,omitempty"`
}

type Manager struct {
	currentVersion string
	publicKey      ed25519.PublicKey
	dir            string
	http           *http.Client
	mu             sync.Mutex
	state          persisted
	generation     uint64
}

func New(currentVersion, publicKeyBase64 string) *Manager {
	dir, _ := os.UserConfigDir()
	dir = filepath.Join(dir, "Draft", "updates")
	m := &Manager{currentVersion: currentVersion, dir: dir, http: &http.Client{Timeout: 30 * time.Second}}
	if raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(publicKeyBase64)); err == nil && len(raw) == ed25519.PublicKeySize {
		m.publicKey = ed25519.PublicKey(raw)
	}
	_ = os.MkdirAll(dir, 0o700)
	m.load()
	if len(m.publicKey) == 0 {
		m.state.Status = Status{State: "disabled", Message: "Updates are not configured for this build."}
	}
	return m
}

func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state.Status
}

func (m *Manager) UpdateDir() string { return m.dir }

func (m *Manager) ReadyArtifact() (string, Status, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.Status.State != "ready" || m.state.Artifact == "" {
		return "", m.state.Status, false
	}
	if _, err := os.Stat(m.state.Artifact); err != nil {
		return "", m.state.Status, false
	}
	return m.state.Artifact, m.state.Status, true
}

func (m *Manager) Check(ctx context.Context) Status {
	m.mu.Lock()
	if len(m.publicKey) == 0 {
		m.mu.Unlock()
		return m.Status()
	}
	m.generation++
	gen := m.generation
	m.state.Status = Status{State: "checking"}
	m.saveLocked()
	m.mu.Unlock()

	release, err := m.latest(ctx)
	if err != nil {
		return m.finish(gen, persisted{Status: Status{State: "failed", Message: "Could not check for updates: " + err.Error()}})
	}
	manifestBytes, sig, err := m.fetchManifest(ctx, release)
	if err != nil {
		return m.finish(gen, persisted{Status: Status{State: "failed", Message: err.Error()}})
	}
	if !ed25519.Verify(m.publicKey, manifestBytes, sig) {
		return m.finish(gen, persisted{Status: Status{State: "failed", Message: "Update manifest signature is invalid."}})
	}
	var manifest Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil || manifest.Version == "" {
		return m.finish(gen, persisted{Status: Status{State: "failed", Message: "Update manifest is invalid."}})
	}
	if compareVersion(manifest.Version, m.currentVersion) <= 0 {
		return m.finish(gen, persisted{Status: Status{State: "unavailable"}})
	}
	platform := platformKey()
	entry, ok := manifest.Platforms[platform]
	if !ok || entry.Asset == "" || entry.SHA256 == "" {
		return m.finish(gen, persisted{Status: Status{State: "failed", Message: "This release has no update for this computer."}})
	}
	url := assetURL(release.Assets, entry.Asset)
	if url == "" {
		return m.finish(gen, persisted{Status: Status{State: "failed", Message: "Update asset is missing from the release."}})
	}

	m.mu.Lock()
	if gen != m.generation {
		m.mu.Unlock()
		return m.Status()
	}
	if m.state.Status.State == "ready" && m.state.Status.Version == manifest.Version && m.state.Artifact != "" {
		if _, err := os.Stat(m.state.Artifact); err == nil {
			status := m.state.Status
			m.mu.Unlock()
			return status
		}
	}
	m.removeStagedLocked()
	m.state = persisted{Status: Status{State: "downloading", Version: manifest.Version, Notes: manifest.Notes}, Platform: platform}
	m.saveLocked()
	m.mu.Unlock()

	path, err := m.download(ctx, gen, manifest.Version, entry, url)
	if err != nil {
		return m.finish(gen, persisted{Status: Status{State: "failed", Version: manifest.Version, Message: "Could not download update: " + err.Error()}})
	}
	return m.finish(gen, persisted{Status: Status{State: "ready", Version: manifest.Version, Notes: manifest.Notes}, Artifact: path, Platform: platform})
}

func (m *Manager) latest(ctx context.Context) (githubRelease, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubLatestURL, nil)
	if err != nil {
		return githubRelease{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Draft-updater")
	resp, err := m.http.Do(req)
	if err != nil {
		return githubRelease{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return githubRelease{}, fmt.Errorf("GitHub returned %s", resp.Status)
	}
	var out githubRelease
	return out, json.NewDecoder(resp.Body).Decode(&out)
}

func (m *Manager) fetchManifest(ctx context.Context, release githubRelease) ([]byte, []byte, error) {
	manifestURL, sigURL := assetURL(release.Assets, manifestName), assetURL(release.Assets, signatureName)
	if manifestURL == "" || sigURL == "" {
		return nil, nil, errors.New("This release is not published as an auto-update.")
	}
	manifest, err := m.get(ctx, manifestURL)
	if err != nil {
		return nil, nil, err
	}
	sigText, err := m.get(ctx, sigURL)
	if err != nil {
		return nil, nil, err
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigText)))
	if err != nil {
		return nil, nil, errors.New("Update manifest signature is malformed.")
	}
	return manifest, sig, nil
}

func (m *Manager) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := m.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download returned %s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 64<<20))
}

func (m *Manager) download(ctx context.Context, gen uint64, version string, entry PlatformAsset, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := m.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download returned %s", resp.Status)
	}
	if entry.Size > 0 && resp.ContentLength > 0 && resp.ContentLength != entry.Size {
		return "", errors.New("update size does not match manifest")
	}
	dir := filepath.Join(m.dir, version)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	tmp := filepath.Join(dir, entry.Asset+".part")
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(out, h), resp.Body)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return "", copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return "", closeErr
	}
	if !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), entry.SHA256) {
		_ = os.Remove(tmp)
		return "", errors.New("update checksum does not match manifest")
	}
	m.mu.Lock()
	obsolete := gen != m.generation
	m.mu.Unlock()
	if obsolete {
		_ = os.RemoveAll(dir)
		return "", errors.New("a newer update is available")
	}
	final := filepath.Join(dir, entry.Asset)
	if err := os.Rename(tmp, final); err != nil {
		return "", err
	}
	return final, nil
}

func (m *Manager) finish(gen uint64, next persisted) Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	if gen != m.generation {
		return m.state.Status
	}
	m.state = next
	m.saveLocked()
	return m.state.Status
}

func (m *Manager) load() {
	data, err := os.ReadFile(filepath.Join(m.dir, "status.json"))
	if err != nil {
		return
	}
	var p persisted
	if json.Unmarshal(data, &p) == nil && p.Status.State == "ready" {
		if _, err := os.Stat(p.Artifact); err == nil {
			m.state = p
		}
	}
}

func (m *Manager) saveLocked() {
	data, err := json.Marshal(m.state)
	if err == nil {
		_ = os.WriteFile(filepath.Join(m.dir, "status.json"), data, 0o600)
	}
}
func (m *Manager) removeStagedLocked() {
	if m.state.Artifact != "" {
		_ = os.RemoveAll(filepath.Dir(m.state.Artifact))
	}
	m.state.Artifact = ""
}
func assetURL(assets []Asset, name string) string {
	for _, asset := range assets {
		if asset.Name == name {
			return asset.URL
		}
	}
	return ""
}
func platformKey() string {
	if runtime.GOOS == "darwin" {
		return "darwin-universal"
	}
	return runtime.GOOS + "-" + runtime.GOARCH
}

func compareVersion(a, b string) int {
	clean := func(v string) []int {
		v = strings.TrimPrefix(strings.SplitN(v, "-", 2)[0], "v")
		parts := strings.Split(v, ".")
		out := make([]int, 3)
		for i, p := range parts {
			if i >= 3 {
				break
			}
			var n int
			_, _ = fmt.Sscanf(p, "%d", &n)
			out[i] = n
		}
		return out
	}
	aa, bb := clean(a), clean(b)
	for i := range aa {
		if aa[i] > bb[i] {
			return 1
		}
		if aa[i] < bb[i] {
			return -1
		}
	}
	return 0
}
