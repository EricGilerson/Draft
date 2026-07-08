import {useEffect, useMemo, useState} from 'react';
import {AlertTriangle, Eye, EyeOff, Plus, Trash2} from 'lucide-react';
import {
    ListProjectEnvVars, SetProjectEnvVar, DeleteProjectEnvVar,
    UpdateProject, DeleteProject,
} from '../../wailsjs/go/main/App';
import {store} from '../../wailsjs/go/models';
import Dialog from './Dialog';
import './VariablesTab.css';
import './ProjectSettingsDialog.css';

type ProjectSettingsDialogProps = {
    project: store.Project;
    onClose: () => void;
    onProjectUpdated?: () => void;
    onProjectDeleted?: (projectId: number) => void;
    onOpenSecrets?: () => void;
};

const SCOPES = ['runtime', 'build', 'both'];

// ProjectSettingsDialog exposes the project-level controls that don't belong
// on any single service: identity (name/description), shared non-secret env
// vars injected into every service at deploy time, and a danger zone to delete
// the project. Project secrets are managed in the Secrets tab.
export default function ProjectSettingsDialog({project, onClose, onProjectUpdated, onProjectDeleted, onOpenSecrets}: ProjectSettingsDialogProps) {
    const [name, setName] = useState(project.name);
    const [description, setDescription] = useState(project.description || '');
    const [savingIdentity, setSavingIdentity] = useState(false);
    const [identityError, setIdentityError] = useState<string | null>(null);
    const [identitySaved, setIdentitySaved] = useState(false);

    const [vars, setVars] = useState<store.ProjectEnvVar[]>([]);
    const [loadingVars, setLoadingVars] = useState(true);
    const [newKey, setNewKey] = useState('');
    const [newValue, setNewValue] = useState('');
    const [newScope, setNewScope] = useState('runtime');
    const [varError, setVarError] = useState<string | null>(null);
    const [revealed, setRevealed] = useState<Record<string, boolean>>({});

    const [deleting, setDeleting] = useState(false);
    const [deleteError, setDeleteError] = useState<string | null>(null);
    const [confirmingDelete, setConfirmingDelete] = useState(false);

    const sharedVars = useMemo(() => vars.filter((v) => !v.secret), [vars]);

    const loadVars = () => {
        ListProjectEnvVars(project.id)
            .then((list) => setVars(list ?? []))
            .catch((e: any) => setVarError(typeof e === 'string' ? e : e?.message || 'could not load env vars'))
            .finally(() => setLoadingVars(false));
    };

    useEffect(() => { loadVars(); }, [project.id]);

    const saveIdentity = () => {
        setSavingIdentity(true);
        setIdentityError(null);
        setIdentitySaved(false);
        UpdateProject(project.id, name, description)
            .then(() => {
                setSavingIdentity(false);
                setIdentitySaved(true);
                onProjectUpdated?.();
                setTimeout(() => setIdentitySaved(false), 2500);
            })
            .catch((e: any) => {
                setSavingIdentity(false);
                setIdentityError(typeof e === 'string' ? e : e?.message || 'could not save');
            });
    };

    const addVar = () => {
        const key = newKey.trim();
        if (!key) return;
        setVarError(null);
        SetProjectEnvVar(project.id, key, newValue, newScope, false)
            .then(() => {
                setNewKey('');
                setNewValue('');
                setNewScope('runtime');
                loadVars();
            })
            .catch((e: any) => setVarError(typeof e === 'string' ? e : e?.message || 'could not add var'));
    };

    const updateValue = (key: string, value: string) => {
        const existing = vars.find((v) => v.key === key);
        if (!existing) return;
        SetProjectEnvVar(project.id, key, value, existing.scope || 'runtime', false)
            .then(loadVars)
            .catch((e: any) => setVarError(typeof e === 'string' ? e : e?.message || 'could not save'));
    };

    const updateScope = (key: string, scope: string) => {
        const existing = vars.find((v) => v.key === key);
        if (!existing) return;
        SetProjectEnvVar(project.id, key, existing.value, scope, false)
            .then(loadVars)
            .catch((e: any) => setVarError(typeof e === 'string' ? e : e?.message || 'could not save'));
    };

    const removeVar = (key: string) => {
        if (!window.confirm(`Remove ${key} from the project? It will no longer be injected into services on their next deploy.`)) return;
        DeleteProjectEnvVar(project.id, key)
            .then(loadVars)
            .catch((e: any) => setVarError(typeof e === 'string' ? e : e?.message || 'could not delete'));
    };

    const handleDelete = () => {
        setDeleting(true);
        setDeleteError(null);
        DeleteProject(project.id)
            .then(() => {
                onProjectDeleted?.(project.id);
                onClose();
            })
            .catch((e: any) => {
                setDeleting(false);
                setDeleteError(typeof e === 'string' ? e : e?.message || 'delete failed');
            });
    };

    return (
        <Dialog title={`Project settings · ${project.name}`} onClose={onClose}>
            <div className="project-settings">
                <section className="project-settings-section">
                    <h3 className="project-settings-section-title">Identity</h3>
                    <div className="form-field">
                        <label className="form-label">Name</label>
                        <input className="input" value={name} onChange={(e) => setName(e.target.value)} />
                    </div>
                    <div className="form-field">
                        <label className="form-label">Description</label>
                        <input className="input" value={description} onChange={(e) => setDescription(e.target.value)} placeholder="Optional" />
                    </div>
                    <div className="form-field">
                        <label className="form-label">Path</label>
                        <span className="settings-hint">The project folder is fixed once a project exists. To move it, remove and re-add the project.</span>
                        <input className="input" value={project.path} disabled readOnly />
                    </div>
                    {identityError && <p className="form-error">{identityError}</p>}
                    <div className="project-settings-actions">
                        <button className="btn btn-primary" onClick={saveIdentity} disabled={savingIdentity || !name.trim() || name.trim() === project.name && description === (project.description || '')}>
                            {savingIdentity ? 'Saving…' : 'Save identity'}
                        </button>
                        {identitySaved && <span className="project-settings-saved">Saved</span>}
                    </div>
                </section>

                <section className="project-settings-section">
                    <h3 className="project-settings-section-title">Shared environment</h3>
                    <p className="settings-hint">
                        Non-secret variables injected into every service in this project. Use for shared config like <code>LOG_LEVEL</code> or <code>FEATURE_FLAGS</code>.
                        {' '}Project secrets are managed in the{' '}
                        {onOpenSecrets ? (
                            <button type="button" className="project-settings-link" onClick={onOpenSecrets}>Secrets tab</button>
                        ) : (
                            <>Secrets tab</>
                        )}.
                    </p>
                    {varError && <p className="form-error">{varError}</p>}
                    <div className="var-add project-settings-var-add">
                        <input placeholder="KEY" value={newKey} onChange={(e) => setNewKey(e.target.value)} />
                        <input placeholder="value" value={newValue} onChange={(e) => setNewValue(e.target.value)} />
                        <select className="input select-styled" value={newScope} onChange={(e) => setNewScope(e.target.value)}>
                            {SCOPES.map((s) => <option key={s} value={s}>{s}</option>)}
                        </select>
                        <button className="btn btn-primary" onClick={addVar} disabled={!newKey.trim()}>
                            <Plus size={14}/> Add
                        </button>
                    </div>
                    <div className="variables-list project-settings-vars">
                        {loadingVars && <div className="variables-empty">Loading…</div>}
                        {!loadingVars && sharedVars.length === 0 && <div className="variables-empty">No shared project variables yet.</div>}
                        {sharedVars.map((v) => (
                            <div key={v.key} className="var-row">
                                <div className="var-key-cell">
                                    <div className="var-key" title={v.key}>{v.key}</div>
                                </div>
                                <div className="var-value-col">
                                    <div className="var-value">
                                        <input
                                            className="var-value-mask"
                                            type={revealed[v.key] ? 'text' : 'password'}
                                            value={v.value}
                                            onChange={(e) => updateValue(v.key, e.target.value)}
                                        />
                                        <button type="button" className="var-toggle" onClick={() => setRevealed((r) => ({...r, [v.key]: !r[v.key]}))} title={revealed[v.key] ? 'Hide value' : 'Show value'}>
                                            {revealed[v.key] ? <EyeOff size={14}/> : <Eye size={14}/>}
                                        </button>
                                        <select
                                            className="input select-styled var-scope-select"
                                            value={v.scope || 'runtime'}
                                            onChange={(e) => updateScope(v.key, e.target.value)}
                                            title="Variable scope"
                                        >
                                            {SCOPES.map((s) => <option key={s} value={s}>{s}</option>)}
                                        </select>
                                        <button className="var-toggle var-toggle--danger" onClick={() => removeVar(v.key)} title="Remove from project">
                                            <Trash2 size={14}/>
                                        </button>
                                    </div>
                                </div>
                            </div>
                        ))}
                    </div>
                </section>

                <section className="project-settings-section project-settings-danger">
                    <h3 className="project-settings-section-title">Danger zone</h3>
                    <p className="settings-hint">
                        Deleting a project stops and removes every service in it and clears all of its configuration. Draft-managed Docker volumes are <strong>kept</strong> so data isn&apos;t destroyed — they appear as orphans in the Volumes tab and can be reclaimed or deleted there.
                    </p>
                    {deleteError && <p className="form-error">{deleteError}</p>}
                    {!confirmingDelete ? (
                        <button className="btn btn-danger" onClick={() => setConfirmingDelete(true)} disabled={deleting}>
                            <Trash2 size={14}/> Delete project
                        </button>
                    ) : (
                        <div className="project-settings-confirm">
                            <AlertTriangle size={14} className="project-settings-confirm-icon"/>
                            <span>Type the project name to confirm deletion:</span>
                            <ConfirmDelete name={project.name} onConfirm={handleDelete} onCancel={() => setConfirmingDelete(false)} deleting={deleting} />
                        </div>
                    )}
                </section>
            </div>
        </Dialog>
    );
}

function ConfirmDelete({name, onConfirm, onCancel, deleting}: {name: string; onConfirm: () => void; onCancel: () => void; deleting: boolean}) {
    const [typed, setTyped] = useState('');
    return (
        <div className="project-settings-confirm-row">
            <input className="input" value={typed} onChange={(e) => setTyped(e.target.value)} placeholder={name} />
            <button className="btn btn-danger" onClick={onConfirm} disabled={deleting || typed !== name}>
                {deleting ? 'Deleting…' : 'Confirm delete'}
            </button>
            <button className="btn btn-ghost" onClick={onCancel} disabled={deleting}>Cancel</button>
        </div>
    );
}
