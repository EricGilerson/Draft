package daemon

import (
	"net/http"

	"Draft/internal/deploy"
)

func (s *Server) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID          uint   `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.store.UpdateProject(req.ID, req.Name, req.Description); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID uint `json:"id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.engine.DeleteProject(r.Context(), req.ID); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleListProjectEnvVars(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProjectID uint `json:"projectId"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	vars, err := s.store.ListProjectEnvVars(req.ProjectID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, vars)
}

func (s *Server) handleSetProjectEnvVar(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProjectID uint   `json:"projectId"`
		Key       string `json:"key"`
		Value     string `json:"value"`
		Scope     string `json:"scope"`
		Secret    bool   `json:"secret"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.store.SetProjectEnvVar(req.ProjectID, req.Key, req.Value, req.Scope, req.Secret); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleDeleteProjectEnvVar(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProjectID uint   `json:"projectId"`
		Key       string `json:"key"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	count, err := s.engine.CountProjectEnvVarReferences(req.ProjectID, req.Key)
	if err != nil {
		writeError(w, err)
		return
	}
	if count > 0 {
		http.Error(w, "project value is still referenced by services", http.StatusConflict)
		return
	}
	if err := s.store.DeleteProjectEnvVar(req.ProjectID, req.Key); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleListProjectEnvVarUsages(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProjectID uint   `json:"projectId"`
		Key       string `json:"key"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	usages, err := s.engine.ListProjectEnvVarUsages(req.ProjectID, req.Key)
	if err != nil {
		writeError(w, err)
		return
	}
	if usages == nil {
		usages = []deploy.SecretUsage{}
	}
	writeJSON(w, usages)
}

