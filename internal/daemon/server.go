package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"Draft/internal/deploy"
	"Draft/internal/dockerwatch"
	"Draft/internal/envfile"
	"Draft/internal/networking"
	"Draft/internal/store"
)

const (
	tokenHeader = "X-Draft-Token"
	idleTimeout = 30 * time.Minute
)

type State struct {
	Addr  string `json:"addr"`
	Token string `json:"token"`
	PID   int    `json:"pid"`
}

type Server struct {
	store         *store.Store
	router        *networking.Router
	engine        *deploy.Engine
	hub           *eventHub
	watch         *dockerwatch.Hub
	state         State
	disableDocker bool
	disableIdle   bool
}

func RunProcess(ctx context.Context) error {
	// Single-instance guard: if a daemon is already recorded and answering,
	// don't start a second one — two daemons would bind separate ports and
	// contend over the one SQLite file. This is what lets a git hook (or any
	// caller) blindly launch the daemon without risking a double-up.
	if existing, err := NewClientFromState(); err == nil {
		pingCtx, cancel := context.WithTimeout(ctx, time.Second)
		alive := existing.Ping(pingCtx) == nil
		cancel()
		if alive {
			log.Printf("[draft-daemon] another daemon is already running; exiting")
			return nil
		}
	}

	dbPath, err := store.DefaultPath()
	if err != nil {
		return err
	}
	s, err := store.Open(store.FileDSN(dbPath))
	if err != nil {
		return err
	}
	defer s.Close()

	router := networking.NewRouter(s, "127.0.0.1:80")
	if err := router.Start(); err != nil {
		log.Printf("[draft-daemon] local domain proxy on port 80 unavailable, falling back to random port: %v", err)
		router = networking.NewRouter(s, "127.0.0.1:0")
		if err := router.Start(); err != nil {
			return err
		}
	}
	defer router.Stop()

	cfgDir, err := ConfigDir()
	if err != nil {
		return err
	}
	logDir := filepath.Join(cfgDir, "logs")
	hub := newEventHub()
	engine := deploy.New(s, router, logDir, hub.publish)
	watch := dockerwatch.New()
	srv := &Server{store: s, router: router, engine: engine, hub: hub, watch: watch}
	return srv.Run(ctx)
}

func (s *Server) Run(ctx context.Context) error {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer listener.Close()

	token, err := randomToken()
	if err != nil {
		return err
	}
	s.state = State{Addr: listener.Addr().String(), Token: token, PID: os.Getpid()}
	if err := s.writeState(); err != nil {
		return err
	}
	defer removeStateIfOwned(s.state)

	if !s.disableDocker {
		go s.watchDocker(ctx)
		reconcileCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		if err := s.engine.Reconcile(reconcileCtx); err != nil {
			log.Printf("[draft-daemon] reconcile: %v", err)
		}
		cancel()
	}

	// Catch commits/pushes to tracked branches that landed while the daemon was
	// down — including the event whose git hook just launched this daemon.
	go s.reconcileAllGitTriggersOnStartup(ctx)

	httpServer := &http.Server{Handler: s.routes()}
	errCh := make(chan error, 1)
	go func() {
		if err := httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	idleCtx, idleCancel := context.WithCancel(ctx)
	defer idleCancel()
	if !s.disableIdle {
		go s.stopWhenIdle(idleCtx, idleCancel)
	}

	select {
	case <-ctx.Done():
	case err := <-errCh:
		if err != nil {
			return err
		}
	case <-idleCtx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return httpServer.Shutdown(shutdownCtx)
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/events", s.handleEvents)
	mux.HandleFunc("/deploy", s.handleDeploy)
	mux.HandleFunc("/node/create-from-template", s.handleCreateNodeFromTemplate)
	mux.HandleFunc("/node/delete", s.handleDeleteService)
	mux.HandleFunc("/node/reapply-template", s.handleReapplyTemplate)
	mux.HandleFunc("/node/delete-preview", s.handlePreviewDeleteService)
	mux.HandleFunc("/node/config-status", s.handleNodeConfigStatus)
	mux.HandleFunc("/node/stage-settings", s.handleStageNodeSettings)
	mux.HandleFunc("/node/stage-env", s.handleStageEnvVarChanges)
	mux.HandleFunc("/node/discard-staged", s.handleDiscardStagedChanges)
	mux.HandleFunc("/node/preview-staged", s.handlePreviewStagedChanges)
	mux.HandleFunc("/stop", s.handleStop)
	mux.HandleFunc("/restart", s.handleRestart)
	mux.HandleFunc("/logs/start", s.handleStartLogStream)
	mux.HandleFunc("/logs/stop", s.handleStopLogStream)
	mux.HandleFunc("/deployments", s.handleDeployments)
	mux.HandleFunc("/active-deployment", s.handleActiveDeployment)
	mux.HandleFunc("/build-log", s.handleBuildLog)
	mux.HandleFunc("/metrics", s.handleMetrics)
	mux.HandleFunc("/node/health", s.handleNodeHealth)
	mux.HandleFunc("/docker", s.handleDocker)
	mux.HandleFunc("/local-domain", s.handleLocalDomain)
	mux.HandleFunc("/env", s.handleGetEnv)
	mux.HandleFunc("/env/set", s.handleSetEnv)
	mux.HandleFunc("/env/delete", s.handleDeleteEnv)
	mux.HandleFunc("/env/scope", s.handleSetEnvScope)
	mux.HandleFunc("/env/secret", s.handleSetEnvSecret)
	mux.HandleFunc("/env/rotate", s.handleRotateEnvSecret)
	mux.HandleFunc("/env/suggest", s.handleSuggestEnv)
	mux.HandleFunc("/env/import", s.handleImportEnv)
	mux.HandleFunc("/env/refresh", s.handleRefreshEnv)
	mux.HandleFunc("/env/export", s.handleExportEnv)
	mux.HandleFunc("/env/preview", s.handlePreviewEnv)
	mux.HandleFunc("/env/reference-targets", s.handleReferenceTargets)
	mux.HandleFunc("/env/reference-issues", s.handleReferenceIssues)
	mux.HandleFunc("/connections", s.handleConnections)
	mux.HandleFunc("/volumes", s.handleListVolumes)
	mux.HandleFunc("/volumes/overview", s.handleVolumesOverview)
	mux.HandleFunc("/volumes/delete", s.handleDeleteVolume)
	mux.HandleFunc("/hooks/recheck", s.handleGitRecheck)
	return s.auth(mux)
}

func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" && r.Header.Get(tokenHeader) != s.state.Token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"ok": true, "pid": s.state.PID})
}

func (s *Server) handleDeploy(w http.ResponseWriter, r *http.Request) {
	var req nodeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	writeError(w, s.engine.Deploy(context.Background(), req.NodeID))
}

func (s *Server) handleCreateNodeFromTemplate(w http.ResponseWriter, r *http.Request) {
	var req deploy.CreateNodeFromTemplateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	res, err := s.engine.CreateNodeFromTemplate(req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, res)
}

func (s *Server) handleDeleteService(w http.ResponseWriter, r *http.Request) {
	var req nodeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	writeError(w, s.engine.DeleteService(context.Background(), req.NodeID))
}

func (s *Server) handleReapplyTemplate(w http.ResponseWriter, r *http.Request) {
	var req nodeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	result, err := s.engine.ReapplyTemplate(req.NodeID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, result)
}

func (s *Server) handlePreviewDeleteService(w http.ResponseWriter, r *http.Request) {
	var req nodeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	preview, err := s.engine.PreviewDeleteService(context.Background(), req.NodeID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, preview)
}

func (s *Server) handleNodeConfigStatus(w http.ResponseWriter, r *http.Request) {
	var req nodeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	status, err := deploy.NodeConfigStatusFromStore(s.store, req.NodeID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, status)
}

type stageSettingsRequest struct {
	NodeID    string            `json:"nodeId"`
	ProjectID uint              `json:"projectId"`
	Settings  map[string]string `json:"settings"`
}

func (s *Server) handleStageNodeSettings(w http.ResponseWriter, r *http.Request) {
	var req stageSettingsRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := stageNodeSettings(s.store, req.NodeID, req.ProjectID, req.Settings); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

type stageEnvRequest struct {
	NodeID     string                    `json:"nodeId"`
	Upserts    []store.EnvVarStageUpsert   `json:"upserts"`
	DeleteKeys []string                  `json:"deleteKeys"`
}

func (s *Server) handleStageEnvVarChanges(w http.ResponseWriter, r *http.Request) {
	var req stageEnvRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.store.StageEnvVarChanges(req.NodeID, req.Upserts, req.DeleteKeys); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleDiscardStagedChanges(w http.ResponseWriter, r *http.Request) {
	var req nodeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	writeError(w, s.store.DiscardAllStagedChanges(req.NodeID))
}

type previewStagedRequest struct {
	NodeID           string            `json:"nodeId"`
	ProposedSettings map[string]string `json:"proposedSettings"`
}

func (s *Server) handlePreviewStagedChanges(w http.ResponseWriter, r *http.Request) {
	var req previewStagedRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	preview, err := deploy.PreviewStagedChangesFromStore(s.store, req.NodeID, req.ProposedSettings)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, preview)
}

func stageNodeSettings(s *store.Store, nodeID string, projectID uint, settings map[string]string) error {
	if len(settings) == 0 {
		return nil
	}
	if root, ok := settings["service_root"]; ok {
		project, err := s.GetProject(projectID)
		if err != nil {
			return fmt.Errorf("project not found: %w", err)
		}
		if err := store.ValidateInsideProject(project.Path, root); err != nil {
			return err
		}
		rel, _ := filepath.Rel(project.Path, root)
		settings["service_root"] = rel
	}
	return s.StageNodeSettings(nodeID, settings)
}

func (s *Server) handleGitRecheck(w http.ResponseWriter, r *http.Request) {
	var req recheckRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	// Reconcile asynchronously so the hook's HTTP call returns immediately and
	// never delays the user's git command.
	go s.reconcileGitTriggers(context.Background(), req)
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	var req nodeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	writeError(w, s.engine.Stop(context.Background(), req.NodeID))
}

func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	var req nodeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	writeError(w, s.engine.Restart(context.Background(), req.NodeID))
}

func (s *Server) handleStartLogStream(w http.ResponseWriter, r *http.Request) {
	var req nodeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	writeError(w, s.engine.StartLogStream(context.Background(), req.NodeID))
}

func (s *Server) handleStopLogStream(w http.ResponseWriter, r *http.Request) {
	var req nodeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	s.engine.StopLogStream(req.NodeID)
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleDeployments(w http.ResponseWriter, r *http.Request) {
	nodeID := r.URL.Query().Get("nodeId")
	deps, err := s.engine.GetDeployments(nodeID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, deps)
}

func (s *Server) handleActiveDeployment(w http.ResponseWriter, r *http.Request) {
	nodeID := r.URL.Query().Get("nodeId")
	dep, err := s.engine.GetActiveDeployment(nodeID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, dep)
}

func (s *Server) handleBuildLog(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseUint(r.URL.Query().Get("id"), 10, 64)
	log, err := s.engine.GetBuildLog(uint(id))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"log": log})
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	nodeID := r.URL.Query().Get("nodeId")
	metrics, err := s.engine.GetServiceMetrics(r.Context(), nodeID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, metrics)
}

func (s *Server) handleNodeHealth(w http.ResponseWriter, r *http.Request) {
	nodeID := r.URL.Query().Get("nodeId")
	health, err := s.engine.GetNodeHealth(r.Context(), nodeID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, health)
}

func (s *Server) handleDocker(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.watch.CurrentDaemon())
}

func (s *Server) handleLocalDomain(w http.ResponseWriter, r *http.Request) {
	if s.router == nil {
		writeJSON(w, networking.LocalDomainStatus{
			Mode:           "localhost-port",
			PublicSuffix:   networking.PublicSuffix,
			LoopbackSuffix: networking.PublicSuffix,
		})
		return
	}
	writeJSON(w, s.router.LocalDomainStatus())
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	ch, unsubscribe := s.hub.subscribe()
	defer unsubscribe()

	s.emitActiveSnapshots(ch)
	flusher.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-ch:
			payload, _ := json.Marshal(ev)
			fmt.Fprintf(w, "data: %s\n\n", payload)
			flusher.Flush()
		}
	}
}

func (s *Server) emitActiveSnapshots(ch chan Event) {
	deps, err := s.store.ListActiveDeployments()
	if err != nil {
		return
	}
	for _, dep := range deps {
		ch <- Event{Name: "deploy:status:" + dep.NodeID, Data: deploy.StatusEvent{
			DeploymentID: dep.ID,
			Status:       dep.Status,
			Hostname:     dep.Hostname,
			HostPort:     dep.HostPort,
			Error:        dep.Error,
		}}
		ch <- Event{Name: "deploy:status", Data: map[string]any{
			"nodeId": dep.NodeID,
			"event": deploy.StatusEvent{
				DeploymentID: dep.ID,
				Status:       dep.Status,
				Hostname:     dep.Hostname,
				HostPort:     dep.HostPort,
				Error:        dep.Error,
			},
		}}
	}
}

func (s *Server) watchDocker(ctx context.Context) {
	s.watch.Subscribe(func(ev dockerwatch.Event) {
		if ev.Kind == "daemon" && ev.Daemon != nil {
			s.hub.publish("docker:status", ev.Daemon)
		}
		if ev.Raw != nil {
			s.hub.publish("docker:activity", map[string]string{
				"type":   string(ev.Raw.Type),
				"action": string(ev.Raw.Action),
				"actor":  ev.Raw.Actor.ID,
				"name":   ev.Raw.Actor.Attributes["name"],
				"image":  ev.Raw.Actor.Attributes["image"],
			})
		}
	})
	s.watch.Run(ctx)
}

func (s *Server) stopWhenIdle(ctx context.Context, cancel context.CancelFunc) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	idleSince := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			active, err := s.store.ListActiveDeployments()
			if err != nil || len(active) > 0 {
				idleSince = time.Now()
				continue
			}
			if time.Since(idleSince) >= idleTimeout {
				cancel()
				return
			}
		}
	}
}

func (s *Server) writeState() error {
	path, err := StatePath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func removeStateIfOwned(state State) {
	path, err := StatePath()
	if err != nil {
		return
	}
	var current State
	data, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(data, &current) != nil {
		return
	}
	if current.PID == state.PID && current.Addr == state.Addr {
		_ = os.Remove(path)
	}
}

func randomToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func decodeJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(out); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, err error) {
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

type nodeRequest struct {
	NodeID string `json:"nodeId"`
}

func (s *Server) handleGetEnv(w http.ResponseWriter, r *http.Request) {
	var req nodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	vars, err := s.getEnvVars(req.NodeID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, vars)
}

func (s *Server) handleSuggestEnv(w http.ResponseWriter, r *http.Request) {
	var req struct {
		NodeID    string `json:"nodeId"`
		ProjectID uint   `json:"projectId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	path, err := s.suggestEnvFile(req.NodeID, req.ProjectID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]string{"path": path})
}

func (s *Server) handleSetEnv(w http.ResponseWriter, r *http.Request) {
	var req struct {
		NodeID string `json:"nodeId"`
		Key    string `json:"key"`
		Value  string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := s.setEnvVar(req.NodeID, req.Key, req.Value); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleDeleteEnv(w http.ResponseWriter, r *http.Request) {
	var req struct {
		NodeID string `json:"nodeId"`
		Key    string `json:"key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := s.store.DeleteEnvVar(req.NodeID, req.Key); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleSetEnvScope(w http.ResponseWriter, r *http.Request) {
	var req struct {
		NodeID string `json:"nodeId"`
		Key    string `json:"key"`
		Scope  string `json:"scope"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := s.store.SetEnvVarScope(req.NodeID, req.Key, req.Scope); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleSetEnvSecret(w http.ResponseWriter, r *http.Request) {
	var req struct {
		NodeID string `json:"nodeId"`
		Key    string `json:"key"`
		Secret bool   `json:"secret"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := s.store.SetEnvVarSecret(req.NodeID, req.Key, req.Secret); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleRotateEnvSecret(w http.ResponseWriter, r *http.Request) {
	var req struct {
		NodeID string `json:"nodeId"`
		Key    string `json:"key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	newValue, err := s.engine.RotateEnvSecret(r.Context(), req.NodeID, req.Key)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "value": newValue})
}

func (s *Server) handleImportEnv(w http.ResponseWriter, r *http.Request) {
	var req struct {
		NodeID string `json:"nodeId"`
		Path   string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	result, err := s.importEnvFile(req.NodeID, req.Path)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleRefreshEnv(w http.ResponseWriter, r *http.Request) {
	var req nodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	result, err := s.refreshEnvFile(req.NodeID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleExportEnv(w http.ResponseWriter, r *http.Request) {
	var req struct {
		NodeID         string `json:"nodeId"`
		IncludeSecrets bool   `json:"includeSecrets"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	result, err := s.exportEnvFile(req.NodeID, req.IncludeSecrets)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, result)
}

func (s *Server) handlePreviewEnv(w http.ResponseWriter, r *http.Request) {
	var req nodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	preview, err := s.engine.PreviewEnvVars(req.NodeID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, preview)
}

func (s *Server) handleReferenceTargets(w http.ResponseWriter, r *http.Request) {
	var req nodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	targets, err := s.engine.ListReferenceTargets(req.NodeID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, targets)
}

func (s *Server) handleReferenceIssues(w http.ResponseWriter, r *http.Request) {
	var req nodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	issues, err := s.engine.ListReferenceIssues(req.NodeID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, issues)
}

func (s *Server) handleConnections(w http.ResponseWriter, r *http.Request) {
	projectID, err := strconv.ParseUint(r.URL.Query().Get("projectId"), 10, 64)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	conns, err := s.engine.GetProjectConnections(uint(projectID))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, conns)
}

// handleListVolumes returns Draft-managed Docker volumes, optionally filtered by
// nodeId and/or projectId query params. Used by the per-node Volumes management
// UI and (later) a project-wide orphan view. nodeId filtering works even after
// the node row is deleted because it matches the draft.node label on the volume.
func (s *Server) handleListVolumes(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var projectID *uint
	if v := q.Get("projectId"); v != "" {
		pid, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			http.Error(w, "bad projectId", http.StatusBadRequest)
			return
		}
		p := uint(pid)
		projectID = &p
	}
	vols, err := s.engine.ListManagedVolumes(r.Context(), projectID, q.Get("nodeId"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, vols)
}

// handleVolumesOverview returns every Draft-managed volume across all projects,
// enriched with its owning node's label and an orphaned flag. Backs the global
// Volumes tab.
func (s *Server) handleVolumesOverview(w http.ResponseWriter, r *http.Request) {
	vols, err := s.engine.ListVolumesOverview(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, vols)
}

// handleDeleteVolume removes a Draft-managed volume by name. force=true also
// removes volumes still referenced by a container; the binding defaults to
// false so a running service's volume can't be yanked accidentally.
func (s *Server) handleDeleteVolume(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name  string `json:"name"`
		Force bool   `json:"force"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	if err := s.engine.DeleteManagedVolume(r.Context(), req.Name, req.Force); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) getEnvVars(nodeID string) ([]store.EnvVar, error) {
	return s.store.ListEnvVars(nodeID)
}

func (s *Server) setEnvVar(nodeID, key, value string) error {
	return s.store.SetEnvVar(nodeID, key, value)
}

func (s *Server) suggestEnvFile(nodeID string, projectID uint) (string, error) {
	settings, err := s.store.GetNodeSettings(nodeID)
	if err != nil {
		return "", err
	}
	if settings["env_file"] != "" {
		return "", nil // already set
	}
	project, err := s.store.GetProject(projectID)
	if err != nil {
		return "", err
	}
	root := project.Path
	if rel := settings["service_root"]; rel != "" {
		root = filepath.Join(project.Path, rel)
	}
	candidate := filepath.Join(root, ".env")
	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	}
	return "", nil
}

func (s *Server) importEnvFile(nodeID, path string) (store.EnvFileSyncResult, error) {
	path = strings.TrimSpace(path)
	result := store.EnvFileSyncResult{Path: path}
	if path == "" {
		return result, fmt.Errorf("env file path is required")
	}
	values, err := envfile.Read(path)
	if err != nil {
		return result, err
	}
	if err := s.store.SetNodeSetting(nodeID, "env_file", path); err != nil {
		return result, err
	}
	return s.store.ImportEnvVars(nodeID, path, values)
}

func (s *Server) refreshEnvFile(nodeID string) (store.EnvFileSyncResult, error) {
	path, err := s.resolveEnvPath(nodeID)
	if err != nil {
		return store.EnvFileSyncResult{}, err
	}
	values, err := envfile.Read(path)
	if err != nil {
		return store.EnvFileSyncResult{Path: path}, err
	}
	return s.store.ImportEnvVars(nodeID, path, values)
}

func (s *Server) exportEnvFile(nodeID string, includeSecrets bool) (store.EnvFileSyncResult, error) {
	path, err := s.resolveEnvPath(nodeID)
	if err != nil {
		return store.EnvFileSyncResult{}, err
	}
	// Resolved, not raw: a written .env file is read by tools outside Draft,
	// which have no notion of an @{Label.ATTR} reference token.
	vars, err := s.engine.ResolveEnvVars(nodeID)
	if err != nil {
		return store.EnvFileSyncResult{Path: path}, err
	}
	count, err := envfile.Write(path, vars, includeSecrets)
	if err != nil {
		return store.EnvFileSyncResult{Path: path}, err
	}
	return store.EnvFileSyncResult{Path: path, Exported: count}, nil
}

func (s *Server) resolveEnvPath(nodeID string) (string, error) {
	node, err := s.store.GetNode(nodeID)
	if err != nil {
		return "", err
	}
	settings, err := s.store.GetNodeSettings(nodeID)
	if err != nil {
		return "", err
	}
	project, err := s.store.GetProject(node.ProjectID)
	if err != nil {
		return "", err
	}
	envPath := strings.TrimSpace(settings["env_file"])
	if envPath != "" {
		return envPath, nil
	}
	root := project.Path
	if rel := strings.TrimSpace(settings["service_root"]); rel != "" {
		root = filepath.Join(project.Path, rel)
	}
	return filepath.Join(root, ".env"), nil
}
