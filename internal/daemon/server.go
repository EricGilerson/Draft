package daemon

import (
	"bufio"
	"bytes"
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
	"sort"
	"strconv"
	"strings"
	"time"

	"Draft/internal/deploy"
	"Draft/internal/dockerwatch"
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
	dbPath, err := store.DefaultPath()
	if err != nil {
		return err
	}
	s, err := store.Open(store.FileDSN(dbPath))
	if err != nil {
		return err
	}
	defer s.Close()

	router := networking.NewRouter(s, "127.0.0.1:0")
	if err := router.Start(); err != nil {
		return err
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
	mux.HandleFunc("/stop", s.handleStop)
	mux.HandleFunc("/restart", s.handleRestart)
	mux.HandleFunc("/logs/start", s.handleStartLogStream)
	mux.HandleFunc("/logs/stop", s.handleStopLogStream)
	mux.HandleFunc("/deployments", s.handleDeployments)
	mux.HandleFunc("/active-deployment", s.handleActiveDeployment)
	mux.HandleFunc("/build-log", s.handleBuildLog)
	mux.HandleFunc("/docker", s.handleDocker)
	mux.HandleFunc("/env", s.handleGetEnv)
	mux.HandleFunc("/env/set", s.handleSetEnv)
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

func (s *Server) handleDocker(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.watch.CurrentDaemon())
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

func (s *Server) getEnvVars(nodeID string) ([]store.EnvVar, error) {
	node, err := s.store.GetNode(nodeID)
	if err != nil {
		return nil, err
	}
	settings, err := s.store.GetNodeSettings(nodeID)
	if err != nil {
		return nil, err
	}
	project, err := s.store.GetProject(node.ProjectID)
	if err != nil {
		return nil, err
	}
	root := project.Path
	if rel := settings["service_root"]; rel != "" {
		root = filepath.Join(project.Path, rel)
	}
	envPath := filepath.Join(root, ".env")
	f, err := os.Open(envPath)
	if os.IsNotExist(err) {
		return []store.EnvVar{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var vars []store.EnvVar
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if idx := strings.Index(line, "="); idx != -1 {
			key := strings.TrimSpace(line[:idx])
			val := strings.TrimSpace(line[idx+1:])
			if key != "" {
				vars = append(vars, store.EnvVar{Key: key, Value: val})
			}
		}
	}
	return vars, scanner.Err()
}

func (s *Server) setEnvVar(nodeID, key, value string) error {
	node, err := s.store.GetNode(nodeID)
	if err != nil {
		return err
	}
	settings, err := s.store.GetNodeSettings(nodeID)
	if err != nil {
		return err
	}
	project, err := s.store.GetProject(node.ProjectID)
	if err != nil {
		return err
	}
	root := project.Path
	if rel := settings["service_root"]; rel != "" {
		root = filepath.Join(project.Path, rel)
	}
	envPath := filepath.Join(root, ".env")

	// read existing
	existing := map[string]string{}
	if f, err := os.Open(envPath); err == nil {
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if idx := strings.Index(line, "="); idx != -1 {
				k := strings.TrimSpace(line[:idx])
				v := strings.TrimSpace(line[idx+1:])
				if k != "" {
					existing[k] = v
				}
			}
		}
		f.Close()
	}

	existing[key] = value

	// write back sorted
	keys := make([]string, 0, len(existing))
	for k := range existing {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b bytes.Buffer
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%s\n", k, existing[k])
	}
	return os.WriteFile(envPath, b.Bytes(), 0o644)
}
