package daemon

import (
	"net/http"

	"Draft/internal/deploy"
	"Draft/internal/store"
)

func (s *Server) handleListAppSecrets(w http.ResponseWriter, r *http.Request) {
	var req struct{}
	if !decodeJSON(w, r, &req) {
		return
	}
	secrets, err := s.store.ListAppSecrets()
	if err != nil {
		writeError(w, err)
		return
	}
	if secrets == nil {
		secrets = []store.AppSecret{}
	}
	writeJSON(w, secrets)
}

func (s *Server) handleSetAppSecret(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key         string `json:"key"`
		Value       string `json:"value"`
		Description string `json:"description"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.store.SetAppSecret(req.Key, req.Value, req.Description); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleDeleteAppSecret(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key string `json:"key"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	count, err := s.engine.CountAppSecretReferences(req.Key)
	if err != nil {
		writeError(w, err)
		return
	}
	if count > 0 {
		http.Error(w, "secret is still referenced by services", http.StatusConflict)
		return
	}
	if err := s.store.DeleteAppSecret(req.Key); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleListAppSecretUsages(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key string `json:"key"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	usages, err := s.engine.ListAppSecretUsages(req.Key)
	if err != nil {
		writeError(w, err)
		return
	}
	if usages == nil {
		usages = []deploy.SecretUsage{}
	}
	writeJSON(w, usages)
}

