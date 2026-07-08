package deploy

import (
	"fmt"
	"regexp"
	"strings"

	"Draft/internal/store"
)

var projectExprPattern = regexp.MustCompile(`\{\{project\.([A-Za-z_][A-Za-z0-9_]*)\}\}`)

func projectExprToken(key string) string {
	return "{{project." + key + "}}"
}

func containsProjectExpr(raw string) bool {
	return projectExprPattern.MatchString(raw)
}

// resolveProjectExprs substitutes {{project.KEY}} tokens from project_env_vars.
// When preserveExprs is true, tokens are left literal (used during .env export).
func (e *Engine) resolveProjectExprs(selfNodeID string, projectID uint, raw string, visited map[string]bool, preserveExprs bool) (string, error) {
	if !containsProjectExpr(raw) {
		return raw, nil
	}
	if preserveExprs {
		return raw, nil
	}
	return e.replaceProjectExprMatches(raw, projectID, selfNodeID, visited)
}

func (e *Engine) replaceProjectExprMatches(raw string, projectID uint, selfNodeID string, visited map[string]bool) (string, error) {
	matches := projectExprPattern.FindAllStringSubmatchIndex(raw, -1)
	if matches == nil {
		return raw, nil
	}
	var out strings.Builder
	last := 0
	for _, m := range matches {
		out.WriteString(raw[last:m[0]])
		key := raw[m[2]:m[3]]
		value, err := e.resolveProjectRef(projectID, key, selfNodeID, visited)
		if err != nil {
			token := raw[m[0]:m[1]]
			return "", fmt.Errorf("%s: %w", token, err)
		}
		out.WriteString(value)
		last = m[1]
	}
	out.WriteString(raw[last:])
	return out.String(), nil
}

func (e *Engine) resolveProjectRef(projectID uint, key string, selfNodeID string, visited map[string]bool) (string, error) {
	pv, err := e.store.GetProjectEnvVar(projectID, key)
	if err != nil {
		return "", fmt.Errorf("project value not found")
	}
	return e.resolveValue(selfNodeID, projectID, pv.Value, visited)
}

func listMissingProjectExprs(s *store.Store, projectID uint, varKey, raw string) []ReferenceIssue {
	var issues []ReferenceIssue
	for _, m := range projectExprPattern.FindAllStringSubmatch(raw, -1) {
		key := m[1]
		token := m[0]
		if _, err := s.GetProjectEnvVar(projectID, key); err != nil {
			issues = append(issues, ReferenceIssue{
				VarKey: varKey,
				Token:  token,
				Reason: "project value not found",
			})
		}
	}
	return issues
}
