import {useCallback, useEffect, useMemo, useState} from 'react';
import {Eye, EyeOff, KeyRound, Pencil, Plus, Trash2} from 'lucide-react';
import {
    ListAppSecrets, SetAppSecret, DeleteAppSecret, ListAppSecretUsages,
    ListAllProjectSecrets, SetProjectEnvVar, DeleteProjectEnvVar,
    ListProjectSecretUsages, DeployService, ListProjects,
} from '../../wailsjs/go/main/App';
import {deploy, store} from '../../wailsjs/go/models';
import PageHeader from '../components/PageHeader';
import Dialog from '../components/Dialog';
import './SecretsView.css';

const SCOPES = ['runtime', 'build', 'both'];

type EditorMode =
    | {kind: 'closed'}
    | {kind: 'app-add'}
    | {kind: 'app-edit'; secret: store.AppSecret}
    | {kind: 'project-add'}
    | {kind: 'project-edit'; entry: store.ProjectSecretEntry};

type SecretsViewProps = {
    filterProjectId?: number | null;
    onClearProjectFilter?: () => void;
};

export default function SecretsView({filterProjectId, onClearProjectFilter}: SecretsViewProps) {
    const [appSecrets, setAppSecrets] = useState<store.AppSecret[]>([]);
    const [projectSecrets, setProjectSecrets] = useState<store.ProjectSecretEntry[]>([]);
    const [projects, setProjects] = useState<store.Project[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState('');
    const [editor, setEditor] = useState<EditorMode>({kind: 'closed'});

    const refresh = useCallback(() => {
        setLoading(true);
        setError('');
        Promise.all([
            ListAppSecrets(),
            ListAllProjectSecrets(),
            ListProjects(),
        ])
            .then(([app, proj, projs]) => {
                setAppSecrets(app ?? []);
                setProjectSecrets(proj ?? []);
                setProjects(projs ?? []);
            })
            .catch((e) => setError(typeof e === 'string' ? e : e?.message || 'Failed to load secrets'))
            .finally(() => setLoading(false));
    }, []);

    useEffect(() => { refresh(); }, [refresh]);

    const filteredProjectSecrets = useMemo(() => {
        if (!filterProjectId) return projectSecrets;
        return projectSecrets.filter((s) => s.projectId === filterProjectId);
    }, [projectSecrets, filterProjectId]);

    const filterProjectName = filterProjectId
        ? projects.find((p) => p.id === filterProjectId)?.name
        : null;

    return (
        <div className="secrets-view">
            <PageHeader
                title="Secrets"
                description="Manage app-wide credentials and per-project secrets. Reference app secrets from services as {{secret.KEY}}."
            />
            <div className="secrets-layout">
                {error && <p className="secrets-error">{error}</p>}
                {filterProjectName && (
                    <div className="secrets-filter-banner">
                        Showing project secrets for <strong>{filterProjectName}</strong>
                        <button type="button" className="btn btn-ghost" onClick={onClearProjectFilter}>Show all</button>
                    </div>
                )}

                <section className="secrets-section">
                    <div className="secrets-section-head">
                        <h2 className="secrets-section-title">
                            <KeyRound size={14}/> App secrets
                        </h2>
                        <button type="button" className="btn btn-primary" onClick={() => setEditor({kind: 'app-add'})}>
                            <Plus size={14}/> Add
                        </button>
                    </div>
                    <p className="secrets-section-hint">Referenced from any service as <code>{'{{secret.KEY}}'}</code></p>
                    {loading ? (
                        <div className="secrets-empty">Loading…</div>
                    ) : appSecrets.length === 0 ? (
                        <div className="secrets-empty">No app secrets yet.</div>
                    ) : (
                        <div className="secrets-list">
                            {appSecrets.map((s) => (
                                <div key={s.key} className="secrets-row">
                                    <div className="secrets-row-main">
                                        <span className="secrets-key">{s.key}</span>
                                        {s.description && <span className="secrets-desc">{s.description}</span>}
                                    </div>
                                    <div className="secrets-row-actions">
                                        <button type="button" className="btn btn-ghost" onClick={() => setEditor({kind: 'app-edit', secret: s})} title="Edit">
                                            <Pencil size={14}/>
                                        </button>
                                        <button type="button" className="btn btn-ghost secrets-delete" onClick={() => handleDeleteApp(s.key, refresh, setError)} title="Delete">
                                            <Trash2 size={14}/>
                                        </button>
                                    </div>
                                </div>
                            ))}
                        </div>
                    )}
                </section>

                <section className="secrets-section">
                    <div className="secrets-section-head">
                        <h2 className="secrets-section-title">Project secrets</h2>
                        <button type="button" className="btn btn-primary" onClick={() => setEditor({kind: 'project-add'})}>
                            <Plus size={14}/> Add
                        </button>
                    </div>
                    <p className="secrets-section-hint">Injected into every service in the project by key name.</p>
                    {loading ? (
                        <div className="secrets-empty">Loading…</div>
                    ) : filteredProjectSecrets.length === 0 ? (
                        <div className="secrets-empty">No project secrets yet.</div>
                    ) : (
                        <div className="secrets-list">
                            {filteredProjectSecrets.map((s) => (
                                <div key={`${s.projectId}:${s.key}`} className="secrets-row">
                                    <div className="secrets-row-main">
                                        <span className="secrets-project">{s.projectName}</span>
                                        <span className="secrets-key">{s.key}</span>
                                        <span className="secrets-scope">{s.scope || 'runtime'}</span>
                                    </div>
                                    <div className="secrets-row-actions">
                                        <button type="button" className="btn btn-ghost" onClick={() => setEditor({kind: 'project-edit', entry: s})} title="Edit">
                                            <Pencil size={14}/>
                                        </button>
                                        <button type="button" className="btn btn-ghost secrets-delete" onClick={() => handleDeleteProject(s.projectId, s.key, refresh, setError)} title="Delete">
                                            <Trash2 size={14}/>
                                        </button>
                                    </div>
                                </div>
                            ))}
                        </div>
                    )}
                </section>
            </div>

            {editor.kind !== 'closed' && (
                <SecretEditorDialog
                    mode={editor}
                    projects={projects}
                    defaultProjectId={filterProjectId ?? undefined}
                    onClose={() => setEditor({kind: 'closed'})}
                    onSaved={refresh}
                />
            )}
        </div>
    );
}

async function handleDeleteApp(key: string, refresh: () => void, setError: (m: string) => void) {
    if (!window.confirm(`Delete app secret ${key}?`)) return;
    try {
        await DeleteAppSecret(key);
        refresh();
    } catch (e: any) {
        setError(typeof e === 'string' ? e : e?.message || 'Delete failed');
    }
}

async function handleDeleteProject(projectId: number, key: string, refresh: () => void, setError: (m: string) => void) {
    if (!window.confirm(`Remove project secret ${key}? Services will lose this injected value on their next deploy.`)) return;
    try {
        await DeleteProjectEnvVar(projectId, key);
        refresh();
    } catch (e: any) {
        setError(typeof e === 'string' ? e : e?.message || 'Delete failed');
    }
}

function SecretEditorDialog({mode, projects, defaultProjectId, onClose, onSaved}: {
    mode: Exclude<EditorMode, {kind: 'closed'}>;
    projects: store.Project[];
    defaultProjectId?: number;
    onClose: () => void;
    onSaved: () => void;
}) {
    const isApp = mode.kind === 'app-add' || mode.kind === 'app-edit';
    const isAdd = mode.kind === 'app-add' || mode.kind === 'project-add';

    const [key, setKey] = useState(
        mode.kind === 'app-edit' ? mode.secret.key
            : mode.kind === 'project-edit' ? mode.entry.key
                : '',
    );
    const [value, setValue] = useState(
        mode.kind === 'app-edit' ? mode.secret.value
            : mode.kind === 'project-edit' ? mode.entry.value
                : '',
    );
    const [description, setDescription] = useState(
        mode.kind === 'app-edit' ? (mode.secret.description || '') : '',
    );
    const [scope, setScope] = useState(
        mode.kind === 'project-edit' ? (mode.entry.scope || 'runtime')
            : mode.kind === 'project-add' ? 'runtime' : 'runtime',
    );
    const [projectId, setProjectId] = useState(
        mode.kind === 'project-edit' ? mode.entry.projectId
            : defaultProjectId ?? (projects[0]?.id ?? 0),
    );
    const [revealed, setRevealed] = useState(false);
    const [saving, setSaving] = useState(false);
    const [error, setError] = useState('');
    const [usages, setUsages] = useState<deploy.SecretUsage[]>([]);
    const [loadingUsages, setLoadingUsages] = useState(false);
    const [redeploying, setRedeploying] = useState<string | null>(null);

    const title = isApp
        ? (isAdd ? 'Add app secret' : `Edit ${key}`)
        : (isAdd ? 'Add project secret' : `Edit ${key}`);

    const loadUsages = useCallback(() => {
        if (isAdd) return;
        setLoadingUsages(true);
        const promise = isApp
            ? ListAppSecretUsages(key)
            : ListProjectSecretUsages(
                mode.kind === 'project-edit' ? mode.entry.projectId : projectId,
                key,
            );
        promise
            .then((list) => setUsages(list ?? []))
            .catch(() => setUsages([]))
            .finally(() => setLoadingUsages(false));
    }, [isAdd, isApp, key, mode, projectId]);

    useEffect(() => { loadUsages(); }, [loadUsages]);

    const save = async () => {
        const trimmedKey = key.trim();
        if (!trimmedKey) return;
        setSaving(true);
        setError('');
        try {
            if (isApp) {
                await SetAppSecret(trimmedKey, value, description);
            } else {
                const pid = mode.kind === 'project-edit' ? mode.entry.projectId : projectId;
                await SetProjectEnvVar(pid, trimmedKey, value, scope, true);
            }
            onSaved();
            if (!isAdd) {
                loadUsages();
            } else {
                onClose();
            }
        } catch (e: any) {
            setError(typeof e === 'string' ? e : e?.message || 'Save failed');
        } finally {
            setSaving(false);
        }
    };

    const redeploy = async (nodeId: string) => {
        setRedeploying(nodeId);
        try {
            await DeployService(nodeId);
            loadUsages();
        } catch (e: any) {
            setError(typeof e === 'string' ? e : e?.message || 'Redeploy failed');
        } finally {
            setRedeploying(null);
        }
    };

    const redeployAllRunning = async () => {
        const running = usages.filter((u) => u.isRunning);
        for (const u of running) {
            await redeploy(u.nodeId);
        }
    };

    const runningCount = usages.filter((u) => u.isRunning).length;

    return (
        <Dialog title={title} onClose={onClose}>
            <div className="secret-editor">
                {error && <p className="form-error">{error}</p>}

                {!isApp && isAdd && (
                    <div className="form-field">
                        <label className="form-label">Project</label>
                        <select className="input select-styled" value={projectId} onChange={(e) => setProjectId(Number(e.target.value))}>
                            {projects.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
                        </select>
                    </div>
                )}

                <div className="form-field">
                    <label className="form-label">Key</label>
                    <input className="input" value={key} onChange={(e) => setKey(e.target.value)} disabled={!isAdd} placeholder="OPENAI_API_KEY" />
                </div>

                <div className="form-field">
                    <label className="form-label">Value</label>
                    <div className="secret-value-row">
                        <input
                            className="input"
                            type={revealed ? 'text' : 'password'}
                            value={value}
                            onChange={(e) => setValue(e.target.value)}
                            autoComplete="off"
                        />
                        <button type="button" className="btn btn-ghost" onClick={() => setRevealed((r) => !r)}>
                            {revealed ? <EyeOff size={14}/> : <Eye size={14}/>}
                        </button>
                    </div>
                </div>

                {isApp && (
                    <div className="form-field">
                        <label className="form-label">Description</label>
                        <input className="input" value={description} onChange={(e) => setDescription(e.target.value)} placeholder="Optional" />
                    </div>
                )}

                {!isApp && (
                    <div className="form-field">
                        <label className="form-label">Scope</label>
                        <select className="input select-styled" value={scope} onChange={(e) => setScope(e.target.value)}>
                            {SCOPES.map((s) => <option key={s} value={s}>{s}</option>)}
                        </select>
                    </div>
                )}

                <div className="secret-editor-actions">
                    <button type="button" className="btn btn-primary" onClick={save} disabled={saving || !key.trim()}>
                        {saving ? 'Saving…' : 'Save'}
                    </button>
                    <button type="button" className="btn btn-ghost" onClick={onClose}>Cancel</button>
                </div>

                {!isAdd && (
                    <div className="secret-usages">
                        <div className="secret-usages-head">
                            <h3 className="secret-usages-title">Usages</h3>
                            {runningCount > 0 && (
                                <button type="button" className="btn btn-ghost" onClick={redeployAllRunning} disabled={!!redeploying}>
                                    Redeploy all running ({runningCount})
                                </button>
                            )}
                        </div>
                        <p className="settings-hint">Services not redeployed will pick up the new value on their next deploy.</p>
                        {loadingUsages ? (
                            <div className="secrets-empty">Loading usages…</div>
                        ) : usages.length === 0 ? (
                            <div className="secrets-empty">No services reference this secret yet.</div>
                        ) : (
                            <div className="secret-usages-list">
                                {usages.map((u) => (
                                    <div key={`${u.nodeId}:${u.varKey}`} className="secret-usage-row">
                                        <div className="secret-usage-main">
                                            <span className="secret-usage-service">{u.projectName} / {u.nodeLabel}</span>
                                            <span className="secret-usage-var">{u.varKey}</span>
                                            {u.overridden && <span className="secret-usage-badge">overridden</span>}
                                            <span className={`secret-usage-status secret-usage-status--${u.isRunning ? 'running' : 'stopped'}`}>
                                                {u.isRunning ? 'running' : 'stopped'}
                                            </span>
                                        </div>
                                        <button
                                            type="button"
                                            className="btn btn-ghost"
                                            disabled={!u.isRunning || redeploying === u.nodeId}
                                            onClick={() => redeploy(u.nodeId)}
                                        >
                                            {redeploying === u.nodeId ? 'Redeploying…' : 'Redeploy'}
                                        </button>
                                    </div>
                                ))}
                            </div>
                        )}
                    </div>
                )}
            </div>
        </Dialog>
    );
}
