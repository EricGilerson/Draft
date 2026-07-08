package deploy

import (
	"fmt"
	"regexp"
	"strings"

	"Draft/internal/store"
)

var secretExprPattern = regexp.MustCompile(`\{\{secret\.([A-Za-z_][A-Za-z0-9_]*)\}\}`)

// secretExprToken returns the literal {{secret.KEY}} token for a key.
func secretExprToken(key string) string {
	return "{{secret." + key + "}}"
}

// containsSecretExpr reports whether raw contains any {{secret.*}} token.
func containsSecretExpr(raw string) bool {
	return secretExprPattern.MatchString(raw)
}

// resolveSecretExprs substitutes every {{secret.KEY}} token in raw with the
// value from app_secrets. When preserveSecretExprs is true, tokens are left
// literal (used during .env export).
func (e *Engine) resolveSecretExprs(raw string, preserveSecretExprs bool) (string, error) {
	if !containsSecretExpr(raw) {
		return raw, nil
	}
	if preserveSecretExprs {
		return raw, nil
	}
	matches := secretExprPattern.FindAllStringSubmatchIndex(raw, -1)
	if matches == nil {
		return raw, nil
	}
	var out strings.Builder
	last := 0
	for _, m := range matches {
		out.WriteString(raw[last:m[0]])
		key := raw[m[2]:m[3]]
		secret, err := e.store.GetAppSecret(key)
		if err != nil {
			return "", fmt.Errorf("{{secret.%s}}: secret not found", key)
		}
		out.WriteString(secret.Value)
		last = m[1]
	}
	out.WriteString(raw[last:])
	return out.String(), nil
}

// listMissingSecretExprs returns issues for {{secret.KEY}} tokens whose key is
// not defined in app_secrets.
func listMissingSecretExprs(s *store.Store, varKey, raw string) []ReferenceIssue {
	var issues []ReferenceIssue
	for _, m := range secretExprPattern.FindAllStringSubmatch(raw, -1) {
		key := m[1]
		token := m[0]
		exists, err := s.AppSecretExists(key)
		if err != nil || !exists {
			reason := "secret not found"
			if err != nil {
				reason = err.Error()
			}
			issues = append(issues, ReferenceIssue{
				VarKey: varKey,
				Token:  token,
				Reason: reason,
			})
		}
	}
	return issues
}
