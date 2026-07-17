package draftmcp

import (
	"sync"
)

// sessionContext holds optional defaults so agents can set a project/environment
// once and omit those IDs on subsequent tools.
type sessionContext struct {
	ProjectID     uint `json:"projectId,omitempty"`
	EnvironmentID uint `json:"environmentId,omitempty"`
}

var (
	sessionMu  sync.RWMutex
	sessionCtx sessionContext
)

func getSessionContext() sessionContext {
	sessionMu.RLock()
	defer sessionMu.RUnlock()
	return sessionCtx
}

func setSessionContext(c sessionContext) {
	sessionMu.Lock()
	defer sessionMu.Unlock()
	sessionCtx = c
}

func clearSessionContext() {
	sessionMu.Lock()
	defer sessionMu.Unlock()
	sessionCtx = sessionContext{}
}

// resolveProjectID uses the argument when present, otherwise the session context.
func resolveProjectID(args map[string]any) (uint, error) {
	if id := optionalUint(args, "projectId"); id != 0 {
		return id, nil
	}
	if id := getSessionContext().ProjectID; id != 0 {
		return id, nil
	}
	return argUint(args, "projectId")
}

func resolveEnvironmentID(args map[string]any) (uint, error) {
	if id := optionalUint(args, "environmentId"); id != 0 {
		return id, nil
	}
	if id := getSessionContext().EnvironmentID; id != 0 {
		return id, nil
	}
	return argUint(args, "environmentId")
}
