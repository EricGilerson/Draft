import {deploy, store} from '../../wailsjs/go/models';

// Mirrors the backend issue scanners in internal/deploy (refs.go, secret_expr.go,
// project_expr.go) so the Variables tab can recompute issues against the live
// in-session draft instead of only the applied/stored env vars. This is what
// makes an invalid @{worker.X} warning clear the moment you fix the token.
const REF_PATTERN = /@\{([^{}]+)\.([A-Za-z0-9_]+)\}/g;
const SECRET_EXPR_PATTERN = /\{\{secret\.([A-Za-z_][A-Za-z0-9_]*)\}\}/g;
const PROJECT_EXPR_PATTERN = /\{\{project\.([A-Za-z_][A-Za-z0-9_]*)\}\}/g;

export function computeReferenceIssues(
    vars: store.EnvVar[],
    linkTargets: deploy.ReferenceTarget[],
    appSecrets: store.AppSecret[],
    projectVars: store.ProjectEnvVar[],
): deploy.ReferenceIssue[] {
    const targetsByLabel = new Map<string, deploy.ReferenceTarget>();
    for (const t of linkTargets) {
        targetsByLabel.set(t.label.trim().toLowerCase(), t);
    }
    const secretKeys = new Set(appSecrets.map((s) => s.key));
    const projectKeys = new Set(projectVars.map((p) => p.key));

    const issues: deploy.ReferenceIssue[] = [];
    for (const v of vars) {
        const value = v.value || '';

        for (const m of value.matchAll(REF_PATTERN)) {
            const token = m[0];
            const label = m[1];
            const attr = m[2];
            const target = targetsByLabel.get(label.trim().toLowerCase());
            if (!target) {
                issues.push(deploy.ReferenceIssue.createFrom({
                    varKey: v.key,
                    token,
                    reason: `no service named "${label}"`,
                }));
                continue;
            }
            if (target.attributes.includes(attr)) continue;
            if (target.customKeys.includes(attr)) continue;
            issues.push(deploy.ReferenceIssue.createFrom({
                varKey: v.key,
                token,
                reason: `"${label}" has no variable named "${attr}"`,
            }));
        }

        for (const m of value.matchAll(SECRET_EXPR_PATTERN)) {
            const token = m[0];
            const key = m[1];
            if (!secretKeys.has(key)) {
                issues.push(deploy.ReferenceIssue.createFrom({
                    varKey: v.key,
                    token,
                    reason: 'secret not found',
                }));
            }
        }

        for (const m of value.matchAll(PROJECT_EXPR_PATTERN)) {
            const token = m[0];
            const key = m[1];
            if (!projectKeys.has(key)) {
                issues.push(deploy.ReferenceIssue.createFrom({
                    varKey: v.key,
                    token,
                    reason: 'project value not found',
                }));
            }
        }
    }
    return issues;
}
