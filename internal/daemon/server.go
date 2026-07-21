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
	"sync"
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
	// daemonProtocolVersion changes whenever a desktop client requires daemon
	// routes or behavior that older daemon processes do not provide.
	daemonProtocolVersion = 2
)

type State struct {
	Addr     string `json:"addr"`
	Token    string `json:"token"`
	PID      int    `json:"pid"`
	Protocol int    `json:"protocol"`
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
	shellTickets  *shellTicketStore
	shutdownMu    sync.Mutex
	shutdown      context.CancelFunc
}

func RunProcess(ctx context.Context) error {
	// Process-lifetime file lock is the real single-instance guard. The older
	// ping-only check raced when Ensure stopped an incompatible daemon and two
	// launchers (app + git hook, or two app paths) started at once: both saw
	// "not alive", both bound a proxy, and only the last writer of daemon.json
	// was discoverable — leaving the other owning DNS / stable proxy ports.
	unlock, err := acquireInstanceLock()
	if err != nil {
		if existing, stateErr := NewClientFromState(); stateErr == nil {
			pingCtx, cancel := context.WithTimeout(ctx, time.Second)
			alive := existing.Ping(pingCtx) == nil
			cancel()
			if alive {
				log.Printf("[draft-daemon] another daemon is already running; exiting")
				return nil
			}
		}
		log.Printf("[draft-daemon] could not acquire instance lock: %v", err)
		return nil
	}
	defer unlock()

	// Re-check after the lock: a peer may have finished writing state while we
	// waited (should be rare with LOCK_NB, but keeps the happy path honest).
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

	router, err := networking.StartRouterFromSettings(s)
	if err != nil {
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
	router.SetProxyAccessHandler(engine.NoteProxyHostAccess)
	watch := dockerwatch.New()
	srv := &Server{store: s, router: router, engine: engine, hub: hub, watch: watch, shellTickets: newShellTicketStore()}
	return srv.Run(ctx)
}

func (s *Server) Run(ctx context.Context) error {
	runCtx, requestShutdown := context.WithCancel(ctx)
	s.shutdownMu.Lock()
	s.shutdown = requestShutdown
	s.shutdownMu.Unlock()
	defer requestShutdown()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer listener.Close()

	token, err := randomToken()
	if err != nil {
		return err
	}
	s.state = State{Addr: listener.Addr().String(), Token: token, PID: os.Getpid(), Protocol: daemonProtocolVersion}
	if err := s.writeState(); err != nil {
		return err
	}
	defer removeStateIfOwned(s.state)

	if !s.disableDocker {
		go s.watchDocker(runCtx)
		reconcileCtx, cancel := context.WithTimeout(runCtx, 10*time.Second)
		if err := s.engine.Reconcile(reconcileCtx); err != nil {
			log.Printf("[draft-daemon] reconcile: %v", err)
		}
		// Re-multi-attach shared (linked) roots onto linker env networks after
		// restart so Staging consumers keep resolving shared Main services.
		if err := s.engine.ReconcileServiceLinkNetworks(reconcileCtx); err != nil {
			log.Printf("[draft-daemon] service-link network reconcile: %v", err)
		}
		cancel()
	}
	if err := s.engine.ReconcileSandboxLifecycle(runCtx, time.Now().UTC()); err != nil {
		log.Printf("[draft-daemon] sandbox lifecycle reconcile: %v", err)
	}
	go s.reconcileSandboxLifecycle(runCtx)

	// Catch commits/pushes to tracked branches that landed while the daemon was
	// down — including the event whose git hook just launched this daemon.
	go s.reconcileAllGitTriggersOnStartup(runCtx)

	httpServer := &http.Server{Handler: s.routes()}
	errCh := make(chan error, 1)
	go func() {
		if err := httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	idleCtx, idleCancel := context.WithCancel(runCtx)
	defer idleCancel()
	if !s.disableIdle {
		go s.stopWhenIdle(idleCtx, idleCancel)
	}

	select {
	case <-runCtx.Done():
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
	mux.HandleFunc("/update/prepare", s.handlePrepareUpdate)
	mux.HandleFunc("/events", s.handleEvents)
	mux.HandleFunc("/deploy", s.handleDeploy)
	mux.HandleFunc("/node/create-from-template", s.handleCreateNodeFromTemplate)
	mux.HandleFunc("/node/delete", s.handleDeleteService)
	mux.HandleFunc("/node/reapply-template", s.handleReapplyTemplate)
	mux.HandleFunc("/environment/delete", s.handleDeleteEnvironment)
	mux.HandleFunc("/environment/duplicate", s.handleDuplicateEnvironment)
	mux.HandleFunc("/environment/duplicate-preview", s.handlePreviewEnvironmentDuplicate)
	mux.HandleFunc("/environment/stack", s.handleEnvironmentStack)
	mux.HandleFunc("/sandbox/preview", s.handlePreviewSandbox)
	mux.HandleFunc("/sandbox/create", s.handleCreateSandbox)
	mux.HandleFunc("/sandbox/extend", s.handleExtendSandbox)
	mux.HandleFunc("/sandbox/delete", s.handleDeleteSandbox)
	mux.HandleFunc("/sandbox/purge-preview", s.handlePreviewSandboxPurge)
	mux.HandleFunc("/sandbox/detail", s.handleSandboxDetail)
	mux.HandleFunc("/sandbox/suspend", s.handleSuspendSandbox)
	mux.HandleFunc("/sandbox/resume", s.handleResumeSandbox)
	mux.HandleFunc("/sandbox/source-repos", s.handleListSandboxSourceRepos)
	mux.HandleFunc("/sandbox/resolve-ref", s.handleResolveSandboxRef)
	mux.HandleFunc("/sandbox/refresh", s.handleRefreshSandbox)
	mux.HandleFunc("/sandbox/test/run", s.handleRunTestingSandbox)
	mux.HandleFunc("/sandbox/test/start", s.handleStartTestingSandbox)
	mux.HandleFunc("/sandbox/test/runs", s.handleListSandboxTestRuns)
	mux.HandleFunc("/sandbox/test/run/get", s.handleGetSandboxTestRun)
	mux.HandleFunc("/sync/preview", s.handleSyncPreview)
	mux.HandleFunc("/sync/apply", s.handleSyncApply)
	mux.HandleFunc("/service/link-info", s.handleGetLinkedServiceInfo)
	mux.HandleFunc("/service/promote", s.handlePromoteLinkedService)
	mux.HandleFunc("/service/unlink", s.handleUnlinkService)
	mux.HandleFunc("/service/link", s.handleLinkToSharedRoot)
	mux.HandleFunc("/service/link-preview", s.handlePreviewLinkToSharedRoot)
	mux.HandleFunc("/service/shareable-roots", s.handleListShareableRoots)
	mux.HandleFunc("/service/share-targets", s.handleListShareTargets)
	mux.HandleFunc("/volume/clone-preview", s.handlePreviewCloneVolume)
	mux.HandleFunc("/volume/clone", s.handleCloneVolumeData)
	mux.HandleFunc("/rollback", s.handleRollback)
	mux.HandleFunc("/deployments/rollback-eligible", s.handleRollbackEligible)
	mux.HandleFunc("/node/delete-preview", s.handlePreviewDeleteService)
	mux.HandleFunc("/node/config-status", s.handleNodeConfigStatus)
	mux.HandleFunc("/node/staleness", s.handleNodeStaleness)
	mux.HandleFunc("/node/stage-settings", s.handleStageNodeSettings)
	mux.HandleFunc("/node/stage-env", s.handleStageEnvVarChanges)
	mux.HandleFunc("/node/discard-staged", s.handleDiscardStagedChanges)
	mux.HandleFunc("/node/discard-staged-partial", s.handleDiscardStagedChangesPartial)
	mux.HandleFunc("/node/preview-staged", s.handlePreviewStagedChanges)
	mux.HandleFunc("/config/import-preview", s.handleConfigImportPreview)
	mux.HandleFunc("/config/import-as-project", s.handleConfigImportAsProject)
	mux.HandleFunc("/config/import-into-project", s.handleConfigImportIntoProject)
	mux.HandleFunc("/config/export", s.handleConfigExport)
	mux.HandleFunc("/config/export-project", s.handleConfigExportProject)
	mux.HandleFunc("/config/export-to-path", s.handleConfigExportToPath)
	mux.HandleFunc("/draftpack/export-service", s.handleDraftPackExportService)
	mux.HandleFunc("/draftpack/export-environment", s.handleDraftPackExportEnvironment)
	mux.HandleFunc("/draftpack/export-project", s.handleDraftPackExportProject)
	mux.HandleFunc("/draftpack/export-to-path", s.handleDraftPackExportToPath)
	mux.HandleFunc("/draftpack/import-preview", s.handleDraftPackImportPreview)
	mux.HandleFunc("/draftpack/import", s.handleDraftPackImport)
	mux.HandleFunc("/draftpack/import-preview-json", s.handleDraftPackImportPreviewJSON)
	mux.HandleFunc("/draftpack/import-json", s.handleDraftPackImportJSON)
	mux.HandleFunc("/stop", s.handleStop)
	mux.HandleFunc("/restart", s.handleRestart)
	mux.HandleFunc("/logs/start", s.handleStartLogStream)
	mux.HandleFunc("/logs/stop", s.handleStopLogStream)
	mux.HandleFunc("/logs/history", s.handleLogHistory)
	mux.HandleFunc("/deployments", s.handleDeployments)
	mux.HandleFunc("/active-deployment", s.handleActiveDeployment)
	mux.HandleFunc("/build-log", s.handleBuildLog)
	mux.HandleFunc("/metrics", s.handleMetrics)
	mux.HandleFunc("/node/health", s.handleNodeHealth)
	mux.HandleFunc("/docker", s.handleDocker)
	mux.HandleFunc("/local-domain", s.handleLocalDomain)
	mux.HandleFunc("/local-domain/enable", s.handleEnableLocalDomain)
	mux.HandleFunc("/local-domain/disable", s.handleDisableLocalDomain)
	mux.HandleFunc("/local-https/enable", s.handleEnableLocalHTTPS)
	mux.HandleFunc("/local-https/disable", s.handleDisableLocalHTTPS)
	mux.HandleFunc("/env", s.handleGetEnv)
	mux.HandleFunc("/env/set", s.handleSetEnv)
	mux.HandleFunc("/env/delete", s.handleDeleteEnv)
	mux.HandleFunc("/env/scope", s.handleSetEnvScope)
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
	mux.HandleFunc("/docker/df", s.handleDockerDF)
	mux.HandleFunc("/docker/containers", s.handleListContainers)
	mux.HandleFunc("/docker/containers/start", s.handleStartContainer)
	mux.HandleFunc("/docker/containers/stop", s.handleStopContainer)
	mux.HandleFunc("/docker/containers/restart", s.handleRestartContainer)
	mux.HandleFunc("/docker/containers/remove", s.handleRemoveContainer)
	mux.HandleFunc("/docker/images", s.handleListImages)
	mux.HandleFunc("/docker/images/remove", s.handleRemoveImage)
	mux.HandleFunc("/docker/networks", s.handleListNetworks)
	mux.HandleFunc("/docker/networks/remove", s.handleRemoveNetwork)
	mux.HandleFunc("/docker/volumes/all", s.handleListAllVolumes)
	mux.HandleFunc("/docker/volumes/remove", s.handleRemoveVolume)
	mux.HandleFunc("/docker/prune", s.handleDockerPrune)
	mux.HandleFunc("/hooks/recheck", s.handleGitRecheck)
	mux.HandleFunc("/exec/attach", s.handleExecAttach)
	mux.HandleFunc("/exec/ticket", s.handleExecTicket)
	mux.HandleFunc("/exec/run", s.handleExecRun)
	mux.HandleFunc("/project/update", s.handleUpdateProject)
	mux.HandleFunc("/project/delete", s.handleDeleteProject)
	mux.HandleFunc("/project/env", s.handleListProjectEnvVars)
	mux.HandleFunc("/project/env/set", s.handleSetProjectEnvVar)
	mux.HandleFunc("/project/env/delete", s.handleDeleteProjectEnvVar)
	mux.HandleFunc("/project/env/usages", s.handleListProjectEnvVarUsages)
	mux.HandleFunc("/secrets", s.handleListAppSecrets)
	mux.HandleFunc("/secrets/set", s.handleSetAppSecret)
	mux.HandleFunc("/secrets/delete", s.handleDeleteAppSecret)
	mux.HandleFunc("/secrets/usages", s.handleListAppSecretUsages)
	mux.HandleFunc("/routes", s.handleListRoutes)

	// Discovery / CRUD used by MCP and headless clients (POST-JSON).
	mux.HandleFunc("/projects/list", s.handleListProjects)
	mux.HandleFunc("/project/create", s.handleCreateProject)
	mux.HandleFunc("/projects/services-summary", s.handleProjectServicesSummary)
	mux.HandleFunc("/environments/list", s.handleListEnvironments)
	mux.HandleFunc("/environment/create", s.handleCreateEnvironment)
	mux.HandleFunc("/environment/rename", s.handleRenameEnvironment)
	mux.HandleFunc("/environment/set-default", s.handleSetDefaultEnvironment)
	mux.HandleFunc("/nodes/list", s.handleListNodes)
	mux.HandleFunc("/node/create", s.handleCreateNode)
	mux.HandleFunc("/node/get", s.handleGetNode)
	mux.HandleFunc("/node/settings", s.handleGetNodeSettings)
	mux.HandleFunc("/templates/list", s.handleListTemplates)
	mux.HandleFunc("/templates/get", s.handleGetTemplate)
	mux.HandleFunc("/sandboxes/list", s.handleListSandboxes)
	mux.HandleFunc("/sandbox/profiles/list", s.handleListSandboxProfiles)
	mux.HandleFunc("/sandbox/profiles/save", s.handleSaveSandboxProfile)
	mux.HandleFunc("/sandbox/profiles/delete", s.handleDeleteSandboxProfile)
	mux.HandleFunc("/sandbox/project-settings/get", s.handleGetSandboxProjectSettings)
	mux.HandleFunc("/sandbox/project-settings/save", s.handleSaveSandboxProjectSettings)
	mux.HandleFunc("/app-settings/get", s.handleGetAppSettings)
	mux.HandleFunc("/app-settings/set", s.handleSetAppSettings)
	return s.auth(mux)
}

func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}
		// Interactive shell WebSocket: browsers cannot set custom headers, so
		// /exec/attach accepts a short-lived single-use ?ticket= minted over
		// header-authenticated HTTP. The long-lived daemon token is never
		// accepted in the query string.
		if r.URL.Path == "/exec/attach" {
			if r.Header.Get(tokenHeader) == s.state.Token {
				next.ServeHTTP(w, r)
				return
			}
			ticket := r.URL.Query().Get("ticket")
			nodeID := r.URL.Query().Get("nodeId")
			if s.ensureShellTickets().consume(ticket, nodeID) {
				next.ServeHTTP(w, r)
				return
			}
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Header.Get(tokenHeader) != s.state.Token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) ensureShellTickets() *shellTicketStore {
	if s.shellTickets == nil {
		s.shellTickets = newShellTicketStore()
	}
	return s.shellTickets
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

func (s *Server) handleDeleteEnvironment(w http.ResponseWriter, r *http.Request) {
	var req struct {
		EnvironmentID uint `json:"environmentId"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	writeError(w, s.engine.DeleteEnvironment(context.Background(), req.EnvironmentID))
}

func (s *Server) handleEnvironmentStack(w http.ResponseWriter, r *http.Request) {
	var req struct {
		EnvironmentID uint   `json:"environmentId"`
		Action        string `json:"action"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	result, err := s.engine.RunEnvironmentStack(context.Background(), req.EnvironmentID, deploy.EnvironmentStackAction(req.Action))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleSyncPreview(w http.ResponseWriter, r *http.Request) {
	var req deploy.SyncRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	out, err := s.engine.PreviewSync(req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, out)
}

func (s *Server) handleSyncApply(w http.ResponseWriter, r *http.Request) {
	var req struct {
		deploy.SyncRequest
		Mode string `json:"mode"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	out, err := s.engine.ApplySync(context.Background(), req.SyncRequest, req.Mode)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, out)
}

func (s *Server) handleDuplicateEnvironment(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SourceEnvironmentID uint                          `json:"sourceEnvironmentId"`
		NewName             string                        `json:"newName"`
		Choices             []deploy.ServiceDataChoice    `json:"choices"`
		StartAfter          bool                          `json:"startAfter"`
		Repositories        []deploy.SandboxRepositoryRef `json:"repositories,omitempty"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	// Detach from the request context so clone-data choices can finish after a client timeout.
	out, err := s.engine.DuplicateEnvironmentWithChoices(context.Background(), req.SourceEnvironmentID, req.NewName, req.Choices, req.StartAfter, req.Repositories)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, out)
}

func (s *Server) handlePreviewEnvironmentDuplicate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SourceEnvironmentID uint `json:"sourceEnvironmentId"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	out, err := s.engine.PreviewEnvironmentDuplicate(req.SourceEnvironmentID)
	if err != nil {
		writeError(w, err)
		return
	}
	if out == nil {
		out = []deploy.StatefulServiceSummary{}
	}
	writeJSON(w, out)
}

func (s *Server) handlePreviewSandbox(w http.ResponseWriter, r *http.Request) {
	var req deploy.SandboxCreateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	out, err := s.engine.PreviewSandbox(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, out)
}

func (s *Server) handleCreateSandbox(w http.ResponseWriter, r *http.Request) {
	var req deploy.SandboxCreateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	out, err := s.engine.CreateSandbox(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, out)
}

func (s *Server) handleListSandboxSourceRepos(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SourceEnvironmentID uint `json:"sourceEnvironmentId"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	out, err := s.engine.ListSandboxSourceRepos(r.Context(), req.SourceEnvironmentID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, out)
}

func (s *Server) handleResolveSandboxRef(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RepoRoot  string `json:"repoRoot"`
		Ref       string `json:"ref"`
		CommitSHA string `json:"commitSha"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	out, err := s.engine.ResolveSandboxRef(r.Context(), req.RepoRoot, req.Ref, req.CommitSHA)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, out)
}

func (s *Server) handleRefreshSandbox(w http.ResponseWriter, r *http.Request) {
	var req deploy.SandboxRefreshRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	out, err := s.engine.RefreshSandbox(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, out)
}

func (s *Server) handleExtendSandbox(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SandboxID uint       `json:"sandboxId"`
		TTLHours  int        `json:"ttlHours"`
		ExpiresAt *time.Time `json:"expiresAt"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	var (
		out *store.Sandbox
		err error
	)
	if req.ExpiresAt != nil {
		out, err = s.engine.ExtendSandboxUntil(req.SandboxID, *req.ExpiresAt)
	} else {
		out, err = s.engine.ExtendSandbox(req.SandboxID, req.TTLHours)
	}
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, out)
}

func (s *Server) handleDeleteSandbox(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SandboxID uint `json:"sandboxId"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	writeError(w, s.engine.DeleteSandbox(r.Context(), req.SandboxID))
}

func (s *Server) handlePreviewSandboxPurge(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SandboxID uint `json:"sandboxId"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	out, err := s.engine.PreviewSandboxPurge(r.Context(), req.SandboxID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, out)
}

func (s *Server) handleSandboxDetail(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SandboxID uint `json:"sandboxId"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	out, err := s.engine.GetSandboxDetail(req.SandboxID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, out)
}

func (s *Server) handleSuspendSandbox(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SandboxID uint `json:"sandboxId"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	out, err := s.engine.SuspendSandbox(r.Context(), req.SandboxID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, out)
}

func (s *Server) handleResumeSandbox(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SandboxID uint `json:"sandboxId"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	out, err := s.engine.ResumeSandbox(r.Context(), req.SandboxID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, out)
}

func (s *Server) handleRunTestingSandbox(w http.ResponseWriter, r *http.Request) {
	var req deploy.SandboxTestRunRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	out, err := s.engine.RunTestingSandbox(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, out)
}

func (s *Server) handleStartTestingSandbox(w http.ResponseWriter, r *http.Request) {
	var req deploy.SandboxTestRunRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	out, err := s.engine.StartTestingSandbox(r.Context(), req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, out)
}

func (s *Server) handleListSandboxTestRuns(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProjectID uint `json:"projectId"`
		Limit     int  `json:"limit"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	out, err := s.engine.ListSandboxTestRuns(req.ProjectID, req.Limit)
	if err != nil {
		writeError(w, err)
		return
	}
	if out == nil {
		out = []store.SandboxTestRun{}
	}
	writeJSON(w, out)
}

func (s *Server) handleGetSandboxTestRun(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RunID uint `json:"runId"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	out, err := s.engine.GetSandboxTestRun(req.RunID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, out)
}

func (s *Server) handleGetLinkedServiceInfo(w http.ResponseWriter, r *http.Request) {
	var req struct {
		NodeID string `json:"nodeId"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	out, err := s.engine.GetLinkedServiceInfo(req.NodeID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, out)
}

func (s *Server) handlePromoteLinkedService(w http.ResponseWriter, r *http.Request) {
	var req struct {
		NodeID      string                  `json:"nodeId"`
		Seed        string                  `json:"seed"`
		Consistency deploy.CloneConsistency `json:"consistency"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	// Detach from the request context so a client disconnect/timeout cannot leave
	// promote mid-clone (link already cleared, volumes half-copied).
	writeError(w, s.engine.PromoteLinkedService(context.Background(), req.NodeID, req.Seed, req.Consistency))
}

func (s *Server) handleUnlinkService(w http.ResponseWriter, r *http.Request) {
	var req struct {
		NodeID string `json:"nodeId"`
		Become string `json:"become"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	writeError(w, s.engine.UnlinkService(r.Context(), req.NodeID, req.Become))
}

func (s *Server) handlePreviewLinkToSharedRoot(w http.ResponseWriter, r *http.Request) {
	var req struct {
		NodeID     string `json:"nodeId"`
		RootNodeID string `json:"rootNodeId"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	out, err := s.engine.PreviewLinkToSharedRoot(req.NodeID, req.RootNodeID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, out)
}

func (s *Server) handleLinkToSharedRoot(w http.ResponseWriter, r *http.Request) {
	var req struct {
		NodeID     string                   `json:"nodeId"`
		RootNodeID string                   `json:"rootNodeId"`
		Volumes    deploy.VolumeDisposition `json:"volumes"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	// Stop + optional volume delete can outlive a short client timeout.
	writeError(w, s.engine.LinkToSharedRoot(context.Background(), req.NodeID, req.RootNodeID, req.Volumes))
}

func (s *Server) handleListShareableRoots(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProjectID            uint `json:"projectId"`
		ExcludeEnvironmentID uint `json:"excludeEnvironmentId"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	out, err := s.engine.ListShareableRoots(req.ProjectID, req.ExcludeEnvironmentID)
	if err != nil {
		writeError(w, err)
		return
	}
	if out == nil {
		out = []deploy.RootServiceSummary{}
	}
	writeJSON(w, out)
}

func (s *Server) handleListShareTargets(w http.ResponseWriter, r *http.Request) {
	var req struct {
		NodeID string `json:"nodeId"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	out, err := s.engine.ListShareTargets(req.NodeID)
	if err != nil {
		writeError(w, err)
		return
	}
	if out == nil {
		out = []deploy.ShareTargetEnvironment{}
	}
	writeJSON(w, out)
}

func (s *Server) handlePreviewCloneVolume(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TargetNodeID  string `json:"targetNodeId"`
		SourceNodeID  string `json:"sourceNodeId"`
		ContainerPath string `json:"containerPath"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	out, err := s.engine.PreviewCloneVolume(r.Context(), req.TargetNodeID, req.SourceNodeID, req.ContainerPath)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, out)
}

func (s *Server) handleCloneVolumeData(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TargetNodeID  string                  `json:"targetNodeId"`
		SourceNodeID  string                  `json:"sourceNodeId"`
		ContainerPath string                  `json:"containerPath"`
		Consistency   deploy.CloneConsistency `json:"consistency"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	// Detach from the request context so disconnects cannot abort mid-copy.
	out, err := s.engine.CloneVolumeData(context.Background(), req.TargetNodeID, req.SourceNodeID, req.ContainerPath, req.Consistency)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, out)
}

func (s *Server) handleConfigImportPreview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	preview, err := s.engine.ImportConfigPreview(req.Path)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, preview)
}

func (s *Server) handleConfigImportAsProject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path        string `json:"path"`
		ProjectName string `json:"projectName"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	result, err := s.engine.ImportConfigAsProject(req.Path, req.ProjectName)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleConfigImportIntoProject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProjectID     uint    `json:"projectId"`
		EnvironmentID uint    `json:"environmentId"`
		Path          string  `json:"path"`
		X             float64 `json:"x"`
		Y             float64 `json:"y"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	result, err := s.engine.ImportConfigIntoProject(req.ProjectID, req.EnvironmentID, req.Path, req.X, req.Y)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleConfigExport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		NodeID string `json:"nodeId"`
		Format string `json:"format"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	result, err := s.engine.ExportConfig(req.NodeID, req.Format)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleConfigExportProject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProjectID uint   `json:"projectId"`
		Format    string `json:"format"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	result, err := s.engine.ExportProjectConfig(req.ProjectID, req.Format)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleConfigExportToPath(w http.ResponseWriter, r *http.Request) {
	var req struct {
		NodeID  string `json:"nodeId"`
		Format  string `json:"format"`
		DestDir string `json:"destDir"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	result, err := s.engine.ExportConfigToPath(req.NodeID, req.Format, req.DestDir)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleDraftPackExportService(w http.ResponseWriter, r *http.Request) {
	var req struct {
		NodeID  string                        `json:"nodeId"`
		Options deploy.DraftPackExportOptions `json:"options"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	result, err := s.engine.ExportDraftPackService(req.NodeID, req.Options)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleDraftPackExportEnvironment(w http.ResponseWriter, r *http.Request) {
	var req struct {
		EnvironmentID uint                          `json:"environmentId"`
		Options       deploy.DraftPackExportOptions `json:"options"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	result, err := s.engine.ExportDraftPackEnvironment(req.EnvironmentID, req.Options)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleDraftPackExportProject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProjectID uint                          `json:"projectId"`
		Options   deploy.DraftPackExportOptions `json:"options"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	result, err := s.engine.ExportDraftPackProject(req.ProjectID, req.Options)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleDraftPackExportToPath(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Scope         string                        `json:"scope"`
		NodeID        string                        `json:"nodeId"`
		ProjectID     uint                          `json:"projectId"`
		EnvironmentID uint                          `json:"environmentId"`
		Options       deploy.DraftPackExportOptions `json:"options"`
		DestPath      string                        `json:"destPath"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	result, err := s.engine.ExportDraftPackToPath(req.Scope, req.NodeID, req.ProjectID, req.EnvironmentID, req.Options, req.DestPath)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleDraftPackImportPreview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path    string                         `json:"path"`
		Options deploy.DraftPackPreviewOptions `json:"options"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	result, err := s.engine.PreviewDraftPackImport(req.Path, req.Options)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleDraftPackImport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path    string                        `json:"path"`
		Options deploy.DraftPackImportOptions `json:"options"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	result, err := s.engine.ImportDraftPack(req.Path, req.Options)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleDraftPackImportPreviewJSON(w http.ResponseWriter, r *http.Request) {
	var req struct {
		JSON    string                         `json:"json"`
		Options deploy.DraftPackPreviewOptions `json:"options"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	result, err := s.engine.PreviewDraftPackJSON([]byte(req.JSON), req.Options)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleDraftPackImportJSON(w http.ResponseWriter, r *http.Request) {
	var req struct {
		JSON    string                        `json:"json"`
		Options deploy.DraftPackImportOptions `json:"options"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	result, err := s.engine.ImportDraftPackJSON([]byte(req.JSON), req.Options)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, result)
}

func (s *Server) handleRollback(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeploymentID uint `json:"deploymentId"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.engine.RollbackDeployment(r.Context(), req.DeploymentID); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) handleRollbackEligible(w http.ResponseWriter, r *http.Request) {
	var req nodeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	items, err := s.engine.RollbackEligibility(r.Context(), req.NodeID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, items)
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

func (s *Server) handleNodeStaleness(w http.ResponseWriter, r *http.Request) {
	var req nodeRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	status, err := s.engine.GetServiceStaleness(req.NodeID)
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
	Upserts    []store.EnvVarStageUpsert `json:"upserts"`
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

type discardStagedPartialRequest struct {
	NodeID       string   `json:"nodeId"`
	SettingKeys  []string `json:"settingKeys"`
	EnvKeys      []string `json:"envKeys"`
}

func (s *Server) handleDiscardStagedChangesPartial(w http.ResponseWriter, r *http.Request) {
	var req discardStagedPartialRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	writeError(w, s.store.DiscardStagedChangesPartial(req.NodeID, req.SettingKeys, req.EnvKeys))
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

type prepareUpdateRequest struct {
	CancelActive bool `json:"cancelActive"`
}

type UpdateReadiness struct {
	Ready       bool     `json:"ready"`
	ActiveNodes []string `json:"activeNodes,omitempty"`
}

// handlePrepareUpdate is the only supported way for the desktop process to
// stop its daemon for an app replacement. It never stops Docker containers.
// A caller must explicitly opt into cancelling in-flight builds.
func (s *Server) handlePrepareUpdate(w http.ResponseWriter, r *http.Request) {
	var req prepareUpdateRequest
	if r.ContentLength > 0 && !decodeJSON(w, r, &req) {
		return
	}
	active := s.engine.ActiveBuilds()
	if len(active) > 0 && !req.CancelActive {
		writeJSON(w, UpdateReadiness{Ready: false, ActiveNodes: active})
		return
	}
	if req.CancelActive {
		s.engine.CancelActiveBuilds()
		deadline := time.Now().Add(30 * time.Second)
		for len(s.engine.ActiveBuilds()) > 0 && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}
		active = s.engine.ActiveBuilds()
		if len(active) > 0 {
			writeJSON(w, UpdateReadiness{Ready: false, ActiveNodes: active})
			return
		}
	}
	s.engine.StopAllLogStreams()
	writeJSON(w, UpdateReadiness{Ready: true})
	// Let the response reach the desktop before closing the server that carries
	// it. Run() then flushes HTTP connections and releases router/database state.
	go func() {
		time.Sleep(75 * time.Millisecond)
		s.shutdownMu.Lock()
		shutdown := s.shutdown
		s.shutdownMu.Unlock()
		if shutdown != nil {
			shutdown()
		}
	}()
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

func (s *Server) handleLogHistory(w http.ResponseWriter, r *http.Request) {
	nodeID := r.URL.Query().Get("nodeId")
	tail, err := strconv.Atoi(r.URL.Query().Get("tail"))
	if err != nil {
		http.Error(w, "tail must be a number", http.StatusBadRequest)
		return
	}
	history, err := s.engine.GetContainerLogHistory(r.Context(), nodeID, tail)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, history)
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
	if r.URL.Query().Get("refresh") == "1" {
		writeJSON(w, s.router.RefreshLocalDomainStatus())
		return
	}
	writeJSON(w, s.router.LocalDomainStatus())
}

func (s *Server) handleEnableLocalDomain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.router == nil {
		http.Error(w, "local router unavailable", http.StatusServiceUnavailable)
		return
	}
	status, err := s.router.EnableLocalDraftDomain()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, status)
}

func (s *Server) handleDisableLocalDomain(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.router == nil {
		http.Error(w, "local router unavailable", http.StatusServiceUnavailable)
		return
	}
	status, err := s.router.DisableLocalDraftDomain()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, status)
}

func (s *Server) handleEnableLocalHTTPS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.router == nil {
		http.Error(w, "local router unavailable", http.StatusServiceUnavailable)
		return
	}
	status, err := s.router.EnableLocalHTTPS()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, status)
}

func (s *Server) handleDisableLocalHTTPS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.router == nil {
		http.Error(w, "local router unavailable", http.StatusServiceUnavailable)
		return
	}
	status, err := s.router.DisableLocalHTTPS()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, status)
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
			if string(ev.Raw.Type) == "container" {
				s.engine.HandleDockerContainerEvent(string(ev.Raw.Action), ev.Raw.Actor.Attributes)
			}
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
		NodeID string `json:"nodeId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	result, err := s.exportEnvFile(req.NodeID)
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
	environmentID, err := strconv.ParseUint(r.URL.Query().Get("environmentId"), 10, 64)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	conns, err := s.engine.GetEnvironmentConnections(uint(environmentID))
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

// handleDockerDF returns the daemon-wide disk usage breakdown (images,
// containers, volumes, build cache) that backs the Docker tab's summary bar.
func (s *Server) handleDockerDF(w http.ResponseWriter, r *http.Request) {
	du, err := s.engine.SystemDF(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, du)
}

// handleListContainers returns every container on the daemon, Draft-managed
// or not. Backs the Docker tab's Containers section.
func (s *Server) handleListContainers(w http.ResponseWriter, r *http.Request) {
	containers, err := s.engine.ListContainers(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, containers)
}

type containerActionRequest struct {
	ID    string `json:"id"`
	Force bool   `json:"force"`
}

func decodeContainerActionRequest(w http.ResponseWriter, r *http.Request) (containerActionRequest, bool) {
	var req containerActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return req, false
	}
	if req.ID == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return req, false
	}
	return req, true
}

func (s *Server) handleStartContainer(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeContainerActionRequest(w, r)
	if !ok {
		return
	}
	if err := s.engine.StartContainer(r.Context(), req.ID); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleStopContainer(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeContainerActionRequest(w, r)
	if !ok {
		return
	}
	if err := s.engine.StopContainer(r.Context(), req.ID); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleRestartContainer(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeContainerActionRequest(w, r)
	if !ok {
		return
	}
	if err := s.engine.RestartContainer(r.Context(), req.ID); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleRemoveContainer(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeContainerActionRequest(w, r)
	if !ok {
		return
	}
	if err := s.engine.RemoveContainer(r.Context(), req.ID, req.Force); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleListImages returns every image on the daemon. Backs the Docker tab's
// Images section.
func (s *Server) handleListImages(w http.ResponseWriter, r *http.Request) {
	images, err := s.engine.ListImages(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, images)
}

func (s *Server) handleRemoveImage(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeContainerActionRequest(w, r)
	if !ok {
		return
	}
	if err := s.engine.RemoveImage(r.Context(), req.ID, req.Force); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleListNetworks returns every network on the daemon. Backs the Docker
// tab's Networks section.
func (s *Server) handleListNetworks(w http.ResponseWriter, r *http.Request) {
	networks, err := s.engine.ListNetworks(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, networks)
}

func (s *Server) handleRemoveNetwork(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if req.ID == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}
	if err := s.engine.RemoveNetwork(r.Context(), req.ID); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleListAllVolumes returns every volume on the daemon, Draft-managed or
// not — the unrestricted counterpart to handleVolumesOverview. Backs the
// Docker tab's Volumes section (the standalone Volumes nav tab keeps using
// handleVolumesOverview for its Draft-only, orphan-aware view).
func (s *Server) handleListAllVolumes(w http.ResponseWriter, r *http.Request) {
	vols, err := s.engine.ListAllVolumes(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, vols)
}

// handleRemoveVolume removes any Docker volume by name, unlike
// handleDeleteVolume which only allows removal of Draft-managed volumes.
func (s *Server) handleRemoveVolume(w http.ResponseWriter, r *http.Request) {
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
	if err := s.engine.RemoveVolume(r.Context(), req.Name, req.Force); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleDockerPrune runs a scoped or unscoped prune for one resource type.
// draftOnly restricts removal to Draft-managed/Draft-built resources where
// that distinction is meaningful (see docker_admin.go's per-resource prune
// methods for how each resource type is scoped).
func (s *Server) handleDockerPrune(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Resource  string `json:"resource"`
		DraftOnly bool   `json:"draftOnly"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	var (
		report deploy.PruneReport
		err    error
	)
	switch req.Resource {
	case "containers":
		report, err = s.engine.PruneContainers(r.Context(), req.DraftOnly)
	case "images":
		report, err = s.engine.PruneImages(r.Context(), req.DraftOnly)
	case "networks":
		report, err = s.engine.PruneNetworks(r.Context(), req.DraftOnly)
	case "volumes":
		report, err = s.engine.PruneVolumes(r.Context(), req.DraftOnly)
	case "buildcache":
		report, err = s.engine.PruneBuildCache(r.Context(), req.DraftOnly)
	default:
		http.Error(w, "unknown resource: "+req.Resource, http.StatusBadRequest)
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, report)
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
	root := store.ResolveUnderProject(project.Path, settings["service_root"])
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

func (s *Server) exportEnvFile(nodeID string) (store.EnvFileSyncResult, error) {
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
	count, err := envfile.Write(path, vars)
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
		return store.ResolveUnderProject(project.Path, envPath), nil
	}
	root := store.ResolveUnderProject(project.Path, settings["service_root"])
	return filepath.Join(root, ".env"), nil
}
