import {useEffect, useState} from 'react';
import {AlertTriangle, Eye, EyeOff, Pencil, Plus, Trash2} from 'lucide-react';
import {
    ListProjectEnvVars, SetProjectEnvVar, DeleteProjectEnvVar,
    ListProjectEnvVarUsages, DeployService,
    UpdateProject, DeleteProject,
} from '../../wailsjs/go/main/App';
import {deploy, store} from '../../wailsjs/go/models';
import Dialog from './Dialog';
import ScopedValueUsages from './ScopedValueUsages';
import './VariablesTab.css';
import './ProjectSettingsDialog.css';
import '../views/SecretsView.css';

type ProjectSettingsDialogProps = {
    project: store.Project;
    onClose: () => void;
    onProjectUpdated?: () => void;
    onProjectDeleted?: (projectId: number) => void;
    onOpenSecrets?: () => void;
};

type EditorMode =
    | {kind: 'closed'}
    | {kind: 'add'}
    | {kind: 'edit'; entry: store.ProjectEnvVar};

const SCOPES = ['runtime', 'build', 'both'];

export default function ProjectSettingsDialog({project, onClose, onProjectUpdated, onProjectDeleted, onOpenSecrets}: ProjectSettingsDialogProps) {
    const [name, setName] = useState(project.name);
    const [description, setDescription] = useState(project.description || '');
    const [savingIdentity, setSavingIdentity] = useState(false);
    const [identityError, setIdentityError] = useState<string | null>(null);
    const [identitySaved, setIdentitySaved] = useState(false);

    const [vars, setVars] = useState<store.ProjectEnvVar[]>([]);
    const [loadingVars, setLoadingVars] = useState(true);
    const [varError, setVarError] = useState<string | null>(null);
    const [editor, setEditor] = useState<EditorMode>({kind: 'closed'});

    const [deleting, setDeleting] = useState(false);
    const [deleteError, setDeleteError] = useState<string | null>(null);
    const [confirmingDelete, setConfirmingDelete] = useState(false);

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

    const removeVar = async (key: string) => {
        if (!window.confirm(`Remove ${key}? Services referencing {{project.${key}}} will fail until updated.`)) return;
        setVarError(null);
        try {
            await DeleteProjectEnvVar(project.id, key);
            loadVars();
        } catch (e: any) {
            setVarError(typeof e === 'string' ? e : e?.message || 'could not delete');
        }
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
                    <div className="secrets-section-head">
                        <h3 className="project-settings-section-title">Shared values</h3>
                        <button type="button" className="btn btn-primary" onClick={() => setEditor({kind: 'add'})}>
                            <Plus size={14}/> Add
                        </button>
                    </div>
                    <p className="settings-hint">
                        Project-scoped values referenced from services in this project as <code>{'{{project.KEY}}'}</code>.
                        {' '}App-wide secrets use <code>{'{{secret.KEY}}'}</code> from the{' '}
                        {onOpenSecrets ? (
                            <button type="button" className="project-settings-link" onClick={onOpenSecrets}>Secrets tab</button>
                        ) : (
                            <>Secrets tab</>
                        )}.
                    </p>
                    {varError && <p className="form-error">{varError}</p>}
                    {loadingVars ? (
                        <div className="variables-empty">Loading…</div>
                    ) : vars.length === 0 ? (
                        <div className="variables-empty">No shared project values yet.</div>
                    ) : (
                        <div className="secrets-list">
                            {vars.map((v) => (
                                <div key={v.key} className="secrets-row">
                                    <div className="secrets-row-main">
                                        <span className="secrets-key">{v.key}</span>
                                        <span className="secrets-scope">{v.scope || 'runtime'}</span>
                                    </div>
                                    <div className="secrets-row-actions">
                                        <button type="button" className="btn btn-ghost" onClick={() => setEditor({kind: 'edit', entry: v})} title="Edit">
                                            <Pencil size={14}/>
                                        </button>
                                        <button type="button" className="btn btn-ghost secrets-delete" onClick={() => removeVar(v.key)} title="Delete">
                                            <Trash2 size={14}/>
                                        </button>
                                    </div>
                                </div>
                            ))}
                        </div>
                    )}
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

            {editor.kind !== 'closed' && (
                <ProjectValueEditorDialog
                    projectId={project.id}
                    mode={editor}
                    onClose={() => setEditor({kind: 'closed'})}
                    onSaved={loadVars}
                />
            )}
        </Dialog>
    );
}

function ProjectValueEditorDialog({projectId, mode, onClose, onSaved}: {
    projectId: number;
    mode: Exclude<EditorMode, {kind: 'closed'}>;
    onClose: () => void;
    onSaved: () => void;
}) {
    const isAdd = mode.kind === 'add';
    const [key, setKey] = useState(mode.kind === 'edit' ? mode.entry.key : '');
    const [value, setValue] = useState(mode.kind === 'edit' ? mode.entry.value : '');
    const [scope, setScope] = useState(mode.kind === 'edit' ? (mode.entry.scope || 'runtime') : 'runtime');
    const [revealed, setRevealed] = useState(false);
    const [saving, setSaving] = useState(false);
    const [error, setError] = useState('');
    const [usages, setUsages] = useState<deploy.SecretUsage[]>([]);
    const [loadingUsages, setLoadingUsages] = useState(false);
    const [redeploying, setRedeploying] = useState<string | null>(null);

    const title = isAdd ? 'Add project value' : `Edit ${key}`;

    const loadUsages = () => {
        if (isAdd || !key.trim()) return;
        setLoadingUsages(true);
        ListProjectEnvVarUsages(projectId, key)
            .then((list) => setUsages(list ?? []))
            .catch(() => setUsages([]))
            .finally(() => setLoadingUsages(false));
    };

    useEffect(() => { loadUsages(); }, [projectId, key, isAdd]);

    const save = async () => {
        const trimmedKey = key.trim();
        if (!trimmedKey) return;
        setSaving(true);
        setError('');
        try {
            await SetProjectEnvVar(projectId, trimmedKey, value, scope, false);
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
        for (const u of usages.filter((usage) => usage.isRunning)) {
            await redeploy(u.nodeId);
        }
    };

    return (
        <Dialog title={title} onClose={onClose}>
            <div className="secret-editor">
                {error && <p className="form-error">{error}</p>}

                <div className="form-field">
                    <label className="form-label">Key</label>
                    <input className="input" value={key} onChange={(e) => setKey(e.target.value)} disabled={!isAdd} placeholder="LOG_LEVEL" />
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

                <div className="form-field">
                    <label className="form-label">Scope</label>
                    <select className="input select-styled" value={scope} onChange={(e) => setScope(e.target.value)}>
                        {SCOPES.map((s) => <option key={s} value={s}>{s}</option>)}
                    </select>
                </div>

                <div className="secret-editor-actions">
                    <button type="button" className="btn btn-primary" onClick={save} disabled={saving || !key.trim()}>
                        {saving ? 'Saving…' : 'Save'}
                    </button>
                    <button type="button" className="btn btn-ghost" onClick={onClose}>Cancel</button>
                </div>

                {!isAdd && (
                    <ScopedValueUsages
                        usages={usages}
                        loading={loadingUsages}
                        redeploying={redeploying}
                        onRedeploy={redeploy}
                        onRedeployAllRunning={redeployAllRunning}
                    />
                )}
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
