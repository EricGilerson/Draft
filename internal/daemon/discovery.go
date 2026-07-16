package daemon

import (
	"net/http"
	"strconv"
	"strings"

	"Draft/internal/deploy"
	"Draft/internal/store"
)

// AppSettings is the persisted app preferences surface exposed over the daemon
// (same shape as the Wails AppSettings type).
type AppSettings struct {
	CompactSidebar          bool   `json:"compactSidebar"`
	LocalDomainPreference   string `json:"localDomainPreference"`
	LocalDraftDomainEnabled bool   `json:"localDraftDomainEnabled"`
	ProxyPortMode           string `json:"proxyPortMode"`
	ProxyPort               int    `json:"proxyPort"`
	ProxyFallbackPort       int    `json:"proxyFallbackPort"`
}

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	if !decodeJSON(w, r, &struct{}{}) {
		return
	}
	projects, err := s.store.ListProjects()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, projects)
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Path        string `json:"path"`
		Description string `json:"description"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	project, err := s.store.CreateProject(req.Name, req.Path, req.Description)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, project)
}

func (s *Server) handleListEnvironments(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProjectID uint `json:"projectId"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	envs, err := s.store.ListEnvironments(req.ProjectID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, envs)
}

func (s *Server) handleCreateEnvironment(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProjectID uint   `json:"projectId"`
		Name      string `json:"name"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	env, err := s.store.CreateEnvironment(req.ProjectID, req.Name)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, env)
}

func (s *Server) handleRenameEnvironment(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID   uint   `json:"id"`
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.store.RenameEnvironment(req.ID, req.Name); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleSetDefaultEnvironment(w http.ResponseWriter, r *http.Request) {
	var req struct {
		EnvironmentID uint `json:"environmentId"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.store.SetDefaultEnvironment(req.EnvironmentID); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleListNodes(w http.ResponseWriter, r *http.Request) {
	var req struct {
		EnvironmentID uint `json:"environmentId"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	nodes, err := s.store.ListNodesByEnvironment(req.EnvironmentID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, nodes)
}

func (s *Server) handleGetNode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	node, err := s.store.GetNode(req.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, node)
}

func (s *Server) handleGetNodeSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		NodeID string `json:"nodeId"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	settings, err := s.store.GetNodeSettings(req.NodeID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, settings)
}

func (s *Server) handleListTemplates(w http.ResponseWriter, r *http.Request) {
	if !decodeJSON(w, r, &struct{}{}) {
		return
	}
	templates, err := s.store.ListTemplates()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, templates)
}

func (s *Server) handleGetTemplate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID uint `json:"id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	tmpl, err := s.store.GetTemplate(req.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, tmpl)
}

func (s *Server) handleListSandboxes(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProjectID uint `json:"projectId"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	sandboxes, err := s.store.ListSandboxes(req.ProjectID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, sandboxes)
}

func (s *Server) handleListSandboxProfiles(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProjectID uint `json:"projectId"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	profiles, err := s.store.ListSandboxProfiles(req.ProjectID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, profiles)
}

func (s *Server) handleSaveSandboxProfile(w http.ResponseWriter, r *http.Request) {
	var profile store.SandboxProfile
	if !decodeJSON(w, r, &profile) {
		return
	}
	saved, err := s.store.SaveSandboxProfile(profile)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, saved)
}

func (s *Server) handleDeleteSandboxProfile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID uint `json:"id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.store.DeleteSandboxProfile(req.ID); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleGetSandboxProjectSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProjectID uint `json:"projectId"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	settings, err := s.store.GetSandboxProjectSettings(req.ProjectID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, settings)
}

func (s *Server) handleSaveSandboxProjectSettings(w http.ResponseWriter, r *http.Request) {
	var settings store.SandboxProjectSettings
	if !decodeJSON(w, r, &settings) {
		return
	}
	saved, err := s.store.SaveSandboxProjectSettings(settings)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, saved)
}

func (s *Server) handleProjectServicesSummary(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProjectID uint `json:"projectId"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	summary, err := deploy.ListProjectServicesSummary(s.store, req.ProjectID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, summary)
}

func (s *Server) handleGetAppSettings(w http.ResponseWriter, r *http.Request) {
	if !decodeJSON(w, r, &struct{}{}) {
		return
	}
	settings, err := loadAppSettings(s.store)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, settings)
}

func (s *Server) handleSetAppSettings(w http.ResponseWriter, r *http.Request) {
	var req AppSettings
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := saveAppSettings(s.store, req); err != nil {
		writeError(w, err)
		return
	}
	settings, err := loadAppSettings(s.store)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, settings)
}

func loadAppSettings(st *store.Store) (*AppSettings, error) {
	all, err := st.ListAppSettings()
	if err != nil {
		return nil, err
	}
	return &AppSettings{
		CompactSidebar:          all[store.AppSettingCompactSidebar] == "true",
		LocalDomainPreference:   all[store.AppSettingLocalDomainPreference],
		LocalDraftDomainEnabled: all[store.AppSettingLocalDraftDomainEnabled] == "true",
		ProxyPortMode:           all[store.AppSettingProxyPortMode],
		ProxyPort:               atoiPort(all[store.AppSettingProxyPort]),
		ProxyFallbackPort:       atoiPort(all[store.AppSettingProxyFallbackPort]),
	}, nil
}

func saveAppSettings(st *store.Store, settings AppSettings) error {
	compact := "false"
	if settings.CompactSidebar {
		compact = "true"
	}
	pref := settings.LocalDomainPreference
	if pref == "" {
		pref = store.LocalDomainPrefAuto
	}
	mode := settings.ProxyPortMode
	if mode == "" {
		mode = store.ProxyPortModePrefer80Fallback
	}
	return st.SetAppSettings(map[string]string{
		store.AppSettingCompactSidebar:        compact,
		store.AppSettingLocalDomainPreference: pref,
		store.AppSettingProxyPortMode:         mode,
		store.AppSettingProxyPort:             strconv.Itoa(settings.ProxyPort),
		store.AppSettingProxyFallbackPort:     strconv.Itoa(settings.ProxyFallbackPort),
	})
}

func atoiPort(raw string) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 1 || n > 65535 {
		return 0
	}
	return n
}
