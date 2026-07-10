import {useEffect, useMemo, useState} from 'react';
import {ArrowRight, GitCompare, Loader2} from 'lucide-react';
import {
    ApplySync,
    ListEnvironments,
    ListNodes,
    PreviewSync,
} from '../../wailsjs/go/main/App';
import {deploy, store} from '../../wailsjs/go/models';
import Dialog from './Dialog';
import './SyncConfigDialog.css';

type SyncConfigDialogProps = {
    projectId: number;
    /** Pre-selected target environment (usually the open env). */
    targetEnvironmentId: number;
    /** When set, opens in service scope for this node. */
    targetNodeId?: string;
    targetNodeLabel?: string;
    onClose: () => void;
    onApplied?: () => void;
};

type Scope = 'service' | 'environment';

function maskSecret(value: string, secret: boolean): string {
    if (!secret || !value) return value || '—';
    if (value.length <= 4) return '••••';
    return '••••' + value.slice(-2);
}

function actionClass(action: string): string {
    switch (action) {
        case 'set':
        case 'restamp':
            return 'sync-action sync-action-set';
        case 'skip':
            return 'sync-action sync-action-skip';
        default:
            return 'sync-action';
    }
}

export default function SyncConfigDialog({
    projectId,
    targetEnvironmentId,
    targetNodeId,
    targetNodeLabel,
    onClose,
    onApplied,
}: SyncConfigDialogProps) {
    const initialScope: Scope = targetNodeId ? 'service' : 'environment';
    const [scope, setScope] = useState<Scope>(initialScope);
    const [environments, setEnvironments] = useState<store.Environment[]>([]);
    const [sourceEnvId, setSourceEnvId] = useState<number | ''>('');
    const [targetEnvId, setTargetEnvId] = useState<number>(targetEnvironmentId);
    const [sourceNodes, setSourceNodes] = useState<store.CanvasNode[]>([]);
    const [targetNodes, setTargetNodes] = useState<store.CanvasNode[]>([]);
    const [sourceNodeId, setSourceNodeId] = useState('');
    const [targetNode, setTargetNode] = useState(targetNodeId || '');
    const [includeSettings, setIncludeSettings] = useState(true);
    const [includeEnv, setIncludeEnv] = useState(true);
    const [preview, setPreview] = useState<deploy.SyncPreview | null>(null);
    const [loadingPreview, setLoadingPreview] = useState(false);
    const [applying, setApplying] = useState(false);
    const [error, setError] = useState('');
    const [applyMessage, setApplyMessage] = useState('');

    useEffect(() => {
        ListEnvironments(projectId)
            .then((envs) => {
                const list = envs ?? [];
                setEnvironments(list);
                const other = list.find((e) => e.id !== targetEnvironmentId);
                if (other) setSourceEnvId(other.id);
                else if (list[0]) setSourceEnvId(list[0].id);
            })
            .catch(() => setEnvironments([]));
    }, [projectId, targetEnvironmentId]);

    useEffect(() => {
        if (!sourceEnvId) {
            setSourceNodes([]);
            return;
        }
        ListNodes(Number(sourceEnvId))
            .then((nodes) => setSourceNodes(nodes ?? []))
            .catch(() => setSourceNodes([]));
    }, [sourceEnvId]);

    useEffect(() => {
        ListNodes(targetEnvId)
            .then((nodes) => {
                const list = nodes ?? [];
                setTargetNodes(list);
                if (targetNodeId) {
                    setTargetNode(targetNodeId);
                    return;
                }
                if (!list.find((n) => n.id === targetNode)) {
                    setTargetNode(list[0]?.id || '');
                }
            })
            .catch(() => setTargetNodes([]));
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [targetEnvId, targetNodeId]);

    // Auto-pick source service with same label when possible.
    useEffect(() => {
        if (scope !== 'service' || !targetNode) return;
        const tgt = targetNodes.find((n) => n.id === targetNode);
        if (!tgt) return;
        const match = sourceNodes.find(
            (n) => n.label.trim().toLowerCase() === tgt.label.trim().toLowerCase(),
        );
        if (match) setSourceNodeId(match.id);
        else if (sourceNodes[0] && !sourceNodeId) setSourceNodeId(sourceNodes[0].id);
    }, [scope, targetNode, targetNodes, sourceNodes, sourceNodeId]);

    const canPreview = useMemo(() => {
        if (!includeSettings && !includeEnv) return false;
        if (!sourceEnvId || !targetEnvId) return false;
        if (Number(sourceEnvId) === Number(targetEnvId) && scope === 'environment') return false;
        if (scope === 'service') {
            return Boolean(sourceNodeId && targetNode && sourceNodeId !== targetNode);
        }
        return true;
    }, [includeSettings, includeEnv, sourceEnvId, targetEnvId, scope, sourceNodeId, targetNode]);

    const buildRequest = (): deploy.SyncRequest =>
        deploy.SyncRequest.createFrom({
            scope,
            sourceEnvironmentId: Number(sourceEnvId),
            targetEnvironmentId: Number(targetEnvId),
            sourceNodeId: scope === 'service' ? sourceNodeId : '',
            targetNodeId: scope === 'service' ? targetNode : '',
            includeSettings,
            includeEnv,
        });

    const runPreview = () => {
        if (!canPreview) return;
        setLoadingPreview(true);
        setError('');
        setApplyMessage('');
        setPreview(null);
        PreviewSync(buildRequest())
            .then((p) => setPreview(p))
            .catch((e) => setError(String(e)))
            .finally(() => setLoadingPreview(false));
    };

    // Clear preview when inputs change.
    useEffect(() => {
        setPreview(null);
        setApplyMessage('');
    }, [scope, sourceEnvId, targetEnvId, sourceNodeId, targetNode, includeSettings, includeEnv]);

    const runApply = async (mode: 'stage' | 'stageAndRedeploy') => {
        if (!preview || preview.actionableCount === 0 || applying) return;
        setApplying(true);
        setError('');
        setApplyMessage('');
        try {
            const result = await ApplySync(buildRequest(), mode);
            const failed = (result.results ?? []).filter((r) => r.error);
            const staged = (result.results ?? []).filter((r) => r.staged).length;
            if (failed.length > 0) {
                setError(
                    failed.map((r) => `${r.label || r.nodeId}: ${r.error}`).join('\n'),
                );
            } else {
                setApplyMessage(
                    mode === 'stageAndRedeploy'
                        ? `Staged and redeployed ${staged} service${staged === 1 ? '' : 's'}.`
                        : `Staged ${staged} service${staged === 1 ? '' : 's'}. Changes apply on next deploy.`,
                );
            }
            onApplied?.();
            // Refresh preview so user sees post-apply state.
            const next = await PreviewSync(buildRequest()).catch(() => null);
            if (next) setPreview(next);
        } catch (e) {
            setError(String(e));
        } finally {
            setApplying(false);
        }
    };

    const actionable = preview?.actionableCount ?? 0;

    return (
        <Dialog
            title="Sync configuration"
            onClose={onClose}
            wide
            footer={
                <>
                    <button className="btn btn-ghost" onClick={onClose} disabled={applying}>
                        Close
                    </button>
                    <button
                        className="btn btn-ghost"
                        onClick={runPreview}
                        disabled={!canPreview || loadingPreview || applying}
                    >
                        {loadingPreview ? <Loader2 size={14} className="spin"/> : <GitCompare size={14}/>}
                        Preview
                    </button>
                    <button
                        className="btn btn-ghost"
                        disabled={!preview || actionable === 0 || applying}
                        onClick={() => void runApply('stage')}
                    >
                        {applying ? 'Working…' : 'Sync'}
                    </button>
                    <button
                        className="btn btn-primary"
                        disabled={!preview || actionable === 0 || applying}
                        onClick={() => void runApply('stageAndRedeploy')}
                    >
                        Sync & redeploy
                    </button>
                </>
            }
        >
            <div className="sync-dialog">
                <p className="sync-dialog-lead">
                    Copy configuration from a source environment into a target. Changes are staged
                    (like the draft bar) unless you choose Sync &amp; redeploy. Template-generated
                    secrets are restamped for the target identity, not pasted.
                </p>

                <div className="sync-dialog-grid">
                    <div className="form-field">
                        <label className="form-label">Scope</label>
                        <div className="sync-toggle-row">
                            <label className="sync-radio">
                                <input
                                    type="radio"
                                    checked={scope === 'environment'}
                                    onChange={() => setScope('environment')}
                                />
                                Environment
                            </label>
                            <label className="sync-radio">
                                <input
                                    type="radio"
                                    checked={scope === 'service'}
                                    onChange={() => setScope('service')}
                                />
                                Service
                            </label>
                        </div>
                    </div>

                    <div className="form-field">
                        <label className="form-label">Modes</label>
                        <div className="sync-toggle-row">
                            <label className="sync-check">
                                <input
                                    type="checkbox"
                                    checked={includeSettings}
                                    onChange={(e) => setIncludeSettings(e.target.checked)}
                                />
                                Settings
                            </label>
                            <label className="sync-check">
                                <input
                                    type="checkbox"
                                    checked={includeEnv}
                                    onChange={(e) => setIncludeEnv(e.target.checked)}
                                />
                                Environment variables
                            </label>
                        </div>
                    </div>
                </div>

                <div className="sync-dialog-grid sync-env-pickers">
                    <div className="form-field">
                        <label className="form-label">Source environment</label>
                        <select
                            className="input settings-select"
                            value={sourceEnvId === '' ? '' : String(sourceEnvId)}
                            onChange={(e) => setSourceEnvId(e.target.value ? Number(e.target.value) : '')}
                        >
                            <option value="">Select…</option>
                            {environments.map((env) => (
                                <option key={env.id} value={env.id}>{env.name}</option>
                            ))}
                        </select>
                    </div>
                    <div className="sync-arrow" aria-hidden>
                        <ArrowRight size={16}/>
                    </div>
                    <div className="form-field">
                        <label className="form-label">Target environment</label>
                        <select
                            className="input settings-select"
                            value={String(targetEnvId)}
                            onChange={(e) => setTargetEnvId(Number(e.target.value))}
                        >
                            {environments.map((env) => (
                                <option key={env.id} value={env.id}>{env.name}</option>
                            ))}
                        </select>
                    </div>
                </div>

                {scope === 'service' && (
                    <div className="sync-dialog-grid sync-env-pickers">
                        <div className="form-field">
                            <label className="form-label">Source service</label>
                            <select
                                className="input settings-select"
                                value={sourceNodeId}
                                onChange={(e) => setSourceNodeId(e.target.value)}
                            >
                                <option value="">Select…</option>
                                {sourceNodes.map((n) => (
                                    <option key={n.id} value={n.id}>{n.label}</option>
                                ))}
                            </select>
                        </div>
                        <div className="sync-arrow" aria-hidden>
                            <ArrowRight size={16}/>
                        </div>
                        <div className="form-field">
                            <label className="form-label">Target service</label>
                            <select
                                className="input settings-select"
                                value={targetNode}
                                onChange={(e) => setTargetNode(e.target.value)}
                                disabled={Boolean(targetNodeId)}
                            >
                                <option value="">Select…</option>
                                {targetNodes.map((n) => (
                                    <option key={n.id} value={n.id}>{n.label}</option>
                                ))}
                            </select>
                            {targetNodeLabel && targetNodeId && (
                                <p className="environment-source-hint">Syncing into {targetNodeLabel}</p>
                            )}
                        </div>
                    </div>
                )}

                {error && <p className="sync-error">{error}</p>}
                {applyMessage && <p className="sync-success">{applyMessage}</p>}

                {loadingPreview && (
                    <div className="sync-preview-empty">
                        <Loader2 size={16} className="spin"/> Building preview…
                    </div>
                )}

                {preview && !loadingPreview && (
                    <div className="sync-preview">
                        <div className="sync-preview-summary">
                            <strong>
                                {preview.sourceEnvName} → {preview.targetEnvName}
                            </strong>
                            <span>
                                {actionable === 0
                                    ? 'No changes for the selected modes'
                                    : `${actionable} change${actionable === 1 ? '' : 's'} ready to sync`}
                            </span>
                        </div>

                        {(preview.unmatchedSource?.length > 0 || preview.unmatchedTarget?.length > 0) && (
                            <div className="sync-unmatched">
                                {preview.unmatchedSource?.length > 0 && (
                                    <p>
                                        Only in source:{' '}
                                        {preview.unmatchedSource.join(', ')}
                                    </p>
                                )}
                                {preview.unmatchedTarget?.length > 0 && (
                                    <p>
                                        Only in target:{' '}
                                        {preview.unmatchedTarget.join(', ')}
                                    </p>
                                )}
                            </div>
                        )}

                        {(preview.services ?? []).map((svc) => (
                            <div key={svc.targetNodeId} className="sync-service-card">
                                <div className="sync-service-head">
                                    <strong>{svc.label}</strong>
                                    {svc.skipped ? (
                                        <span className="sync-badge sync-badge-skip">{svc.skipReason}</span>
                                    ) : (
                                        <span className="sync-badge">
                                            {svc.actionableCount} change{svc.actionableCount === 1 ? '' : 's'}
                                        </span>
                                    )}
                                </div>

                                {svc.warnings?.length > 0 && (
                                    <ul className="sync-warnings">
                                        {svc.warnings.map((w, i) => (
                                            <li key={i}>{w.message}</li>
                                        ))}
                                    </ul>
                                )}

                                {!svc.skipped && includeSettings && (svc.settings?.length ?? 0) > 0 && (
                                    <div className="sync-section">
                                        <div className="sync-section-title">Settings</div>
                                        <table className="sync-table">
                                            <thead>
                                                <tr>
                                                    <th>Key</th>
                                                    <th>Source</th>
                                                    <th>Target</th>
                                                    <th>Action</th>
                                                </tr>
                                            </thead>
                                            <tbody>
                                                {svc.settings.map((d) => (
                                                    <tr key={d.key}>
                                                        <td className="sync-mono">{d.key}</td>
                                                        <td className="sync-mono sync-cell">{d.sourceValue || '—'}</td>
                                                        <td className="sync-mono sync-cell">{d.targetEffective || '—'}</td>
                                                        <td>
                                                            <span className={actionClass(d.action)}>{d.action}</span>
                                                            {d.reason && <div className="sync-reason">{d.reason}</div>}
                                                            {d.warnings?.map((w, i) => (
                                                                <div key={i} className="sync-reason">{w}</div>
                                                            ))}
                                                        </td>
                                                    </tr>
                                                ))}
                                            </tbody>
                                        </table>
                                    </div>
                                )}

                                {!svc.skipped && includeEnv && (svc.env?.length ?? 0) > 0 && (
                                    <div className="sync-section">
                                        <div className="sync-section-title">Environment variables</div>
                                        <table className="sync-table">
                                            <thead>
                                                <tr>
                                                    <th>Key</th>
                                                    <th>Source</th>
                                                    <th>Target</th>
                                                    <th>Action</th>
                                                </tr>
                                            </thead>
                                            <tbody>
                                                {svc.env.map((d) => (
                                                    <tr key={d.key}>
                                                        <td className="sync-mono">
                                                            {d.key}
                                                            {d.sourceSecret || d.targetSecret ? (
                                                                <span className="sync-secret-tag">secret</span>
                                                            ) : null}
                                                        </td>
                                                        <td className="sync-mono sync-cell">
                                                            {maskSecret(d.sourceValue, d.sourceSecret)}
                                                            {d.sourceSource && (
                                                                <div className="sync-meta">{d.sourceSource}</div>
                                                            )}
                                                        </td>
                                                        <td className="sync-mono sync-cell">
                                                            {maskSecret(d.targetValue, d.targetSecret)}
                                                            {d.targetSource && (
                                                                <div className="sync-meta">{d.targetSource}</div>
                                                            )}
                                                        </td>
                                                        <td>
                                                            <span className={actionClass(d.action)}>{d.action}</span>
                                                            {d.reason && <div className="sync-reason">{d.reason}</div>}
                                                            {d.action === 'restamp' && d.proposedValue && (
                                                                <div className="sync-meta">
                                                                    → {maskSecret(d.proposedValue, true)}
                                                                </div>
                                                            )}
                                                        </td>
                                                    </tr>
                                                ))}
                                            </tbody>
                                        </table>
                                    </div>
                                )}

                                {!svc.skipped && svc.actionableCount === 0 && (svc.settings?.length ?? 0) === 0 && (svc.env?.length ?? 0) === 0 && (
                                    <p className="sync-no-diff">No differences for this service.</p>
                                )}
                            </div>
                        ))}

                        {(preview.services ?? []).length === 0 && (
                            <div className="sync-preview-empty">No matching services to compare.</div>
                        )}
                    </div>
                )}
            </div>
        </Dialog>
    );
}
