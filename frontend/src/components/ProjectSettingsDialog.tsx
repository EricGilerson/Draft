import {useEffect, useState} from 'react';
import {AlertTriangle, Eye, EyeOff, Pencil, Plus, Trash2} from 'lucide-react';
import {
    ListProjectEnvVars, SetProjectEnvVar, DeleteProjectEnvVar,
    ListProjectEnvVarUsages, DeployService,
    UpdateProject, DeleteProject, DeleteSandboxProfile, GetSandboxProjectSettings,
    ListEnvironments, ListNodes, ListSandboxProfiles, SaveSandboxProfile, SaveSandboxProjectSettings,
} from '../../wailsjs/go/main/App';
import {deploy, store} from '../../wailsjs/go/models';
import SandboxHoursInput from './SandboxHoursInput';
import {useAppDialog} from './AppDialogProvider';
import Dialog from './Dialog';
import ScopedValueUsages from './ScopedValueUsages';
import {Skeleton, SkeletonListCards} from './Skeleton';
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
    const {confirm} = useAppDialog();

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
        if (!await confirm({
            title: 'Remove project value?',
            message: `Remove ${key}?`,
            detail: `Services referencing {{project.${key}}} will fail until updated.`,
            confirmLabel: 'Remove',
            danger: true,
        })) return;
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

                <ProjectSandboxSettings projectId={project.id}/>

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
                        <SkeletonListCards count={3} withActions />
                    ) : vars.length === 0 ? (
                        <div className="variables-empty">No shared project values yet.</div>
                    ) : (
                        <div className="secrets-list">
                            {vars.map((v) => (
                                <div key={v.key} className="secrets-row">
                                    <div className="secrets-row-main">
                                        <span className="secrets-key">{v.key}</span>
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

function ProjectSandboxSettings({projectId}: {projectId: number}) {
    const [settings, setSettings] = useState<store.SandboxProjectSettings | null>(null);
    const [profiles, setProfiles] = useState<store.SandboxProfile[]>([]);
    const [environments, setEnvironments] = useState<store.Environment[]>([]);
    const [name, setName] = useState('');
    const [scope, setScope] = useState(0);
    const [ttl, setTTL] = useState(168);
    const [warning, setWarning] = useState(24);
    const [grace, setGrace] = useState(72);
    const [isDefault, setIsDefault] = useState(false);
    const [editing, setEditing] = useState<store.SandboxProfile | null>(null);
    const [profileEditorOpen, setProfileEditorOpen] = useState(false);
    const [profileNodes, setProfileNodes] = useState<store.CanvasNode[]>([]);
    const [profileRules, setProfileRules] = useState<Record<string, deploy.SandboxServiceRule>>({});
    const [saving, setSaving] = useState(false);
    const [loading, setLoading] = useState(true);

    const refresh = () => {
        setLoading(true);
        GetSandboxProjectSettings(projectId).then(setSettings).catch(() => setSettings(null)).finally(() => setLoading(false));
        ListSandboxProfiles(projectId).then((rows) => setProfiles(rows ?? [])).catch(() => setProfiles([]));
        ListEnvironments(projectId).then((rows) => setEnvironments(rows ?? [])).catch(() => setEnvironments([]));
    };
    useEffect(refresh, [projectId]);
    const edit = (profile?: store.SandboxProfile) => {
        setProfileEditorOpen(true);
        setEditing(profile ?? null); setName(profile?.name ?? ''); setScope(profile?.sourceEnvironmentId ?? 0); setIsDefault(profile?.isDefault ?? false);
        try { const plan = JSON.parse(profile?.planJson || '{}'); setTTL(plan.ttlHours ?? settings?.defaultTtlHours ?? 168); setWarning(plan.warningHours ?? settings?.warningHours ?? 24); setGrace(plan.graceHours ?? settings?.graceHours ?? 72); setProfileRules(Object.fromEntries((plan.services ?? []).map((rule: deploy.SandboxServiceRule) => [rule.sourceNodeId, deploy.SandboxServiceRule.createFrom(rule)]))); }
        catch { setTTL(168); setWarning(24); setGrace(72); setProfileRules({}); }
    };
    useEffect(() => {
        if (!profileEditorOpen || !scope) { setProfileNodes([]); return; }
        ListNodes(scope).then((nodes) => setProfileNodes(nodes ?? [])).catch(() => setProfileNodes([]));
    }, [scope, profileEditorOpen]);
    const saveDefaults = async () => {
        if (!settings) return; setSaving(true);
        try { await SaveSandboxProjectSettings(store.SandboxProjectSettings.createFrom(settings)); } finally { setSaving(false); }
    };
    const saveProfile = async () => {
        if (!name.trim()) return; setSaving(true);
        try { await SaveSandboxProfile(store.SandboxProfile.createFrom({id: editing?.id, projectId, sourceEnvironmentId: scope, name: name.trim(), description: '', isDefault, planJson: JSON.stringify({ttlHours: ttl, warningHours: warning, graceHours: grace, services: Object.values(profileRules)})})); setProfileEditorOpen(false); setEditing(null); setName(''); refresh(); } finally { setSaving(false); }
    };
    return <section className="project-settings-section">
        <div className="secrets-section-head"><h3 className="project-settings-section-title">Sandbox defaults & profiles</h3><button type="button" className="btn btn-primary" onClick={() => edit()}><Plus size={14}/> New profile</button></div>
        <p className="settings-hint">These defaults apply to new sandboxes in this project. Profiles can be scoped to a source environment and selected during creation.</p>
        {loading ? (
            <div className="sandbox-profile-times">
                {Array.from({length: 3}, (_, i) => (
                    <div key={i} className="form-field">
                        <Skeleton width="60%" height={10} style={{marginBottom: 6}} />
                        <Skeleton height={32} />
                    </div>
                ))}
            </div>
        ) : settings && <div className="sandbox-profile-times"><SandboxHoursInput label="Default lifetime (hours)" value={settings.defaultTtlHours} min={1} onChange={(defaultTtlHours) => setSettings(store.SandboxProjectSettings.createFrom({...settings, defaultTtlHours}))}/><SandboxHoursInput label="Warning (hours)" value={settings.warningHours} min={0} onChange={(warningHours) => setSettings(store.SandboxProjectSettings.createFrom({...settings, warningHours}))}/><SandboxHoursInput label="Grace (hours)" value={settings.graceHours} min={0} onChange={(graceHours) => setSettings(store.SandboxProjectSettings.createFrom({...settings, graceHours}))}/><SandboxHoursInput label="Idle suspend (hours)" value={settings.suspendIdleHours} min={0} onChange={(suspendIdleHours) => setSettings(store.SandboxProjectSettings.createFrom({...settings, suspendIdleHours}))}/><p className="settings-hint" style={{gridColumn: '1 / -1', margin: 0}}>Idle suspend stops a sandbox after this many hours with no proxy traffic (0 = off). TTL expiry still applies.</p><button className="btn btn-ghost" disabled={saving} onClick={() => void saveDefaults()}>Save defaults</button></div>}
        {profiles.map((profile) => <div key={profile.id} className="sandbox-profile-card"><div><strong>{profile.name}</strong><span>{profile.sourceEnvironmentId ? environments.find((env) => env.id === profile.sourceEnvironmentId)?.name ?? 'Source environment' : 'Project-wide'}{profile.isDefault ? ' · default' : ''}</span></div><div><button className="btn btn-ghost" onClick={() => edit(profile)}>Edit</button><button className="btn btn-ghost" onClick={() => void DeleteSandboxProfile(profile.id).then(refresh)}>Delete</button></div></div>)}
        {profileEditorOpen ? <div className="sandbox-plan-review"><div className="form-field"><label className="form-label">Profile name</label><input className="input" value={name} onChange={(e) => setName(e.target.value)}/></div><div className="form-field"><label className="form-label">Source environment</label><select className="input settings-select" value={scope} onChange={(e) => { setScope(Number(e.target.value)); setProfileRules({}); }}><option value={0}>Any environment</option>{environments.map((env) => <option key={env.id} value={env.id}>{env.name}</option>)}</select></div><div className="sandbox-profile-times"><SandboxHoursInput label="Lifetime" value={ttl} min={1} onChange={setTTL}/><SandboxHoursInput label="Warning" value={warning} min={0} onChange={setWarning}/><SandboxHoursInput label="Grace" value={grace} min={0} onChange={setGrace}/></div>{scope ? <div className="sandbox-plan-editor"><h4 className="project-settings-section-title">Service defaults</h4><p className="settings-hint">These rules apply when this profile is used from the selected source environment.</p>{profileNodes.map((node) => { const rule = profileRules[node.id]; const mode = rule?.mode ?? 'copy'; return <div className="sandbox-plan-row" key={node.id}><strong>{node.label}</strong><select className="input settings-select" value={mode} onChange={(e) => setProfileRules((rules) => ({...rules, [node.id]: deploy.SandboxServiceRule.createFrom({sourceNodeId: node.id, mode: e.target.value, dataMode: rule?.dataMode})}))}><option value="copy">Copy</option><option value="share">Share source service</option><option value="omit">Omit</option></select>{mode === 'copy' && <select className="input settings-select" value={rule?.dataMode ?? ''} onChange={(e) => setProfileRules((rules) => ({...rules, [node.id]: deploy.SandboxServiceRule.createFrom({sourceNodeId: node.id, mode, dataMode: e.target.value || undefined})}))}><option value="">Use automatic data policy</option><option value="clone">Clone data</option><option value="fresh">Fresh data</option></select>}</div>; })}</div> : <p className="settings-hint">Choose a source environment to configure service defaults. Project-wide profiles only set lifecycle defaults because service IDs differ between environments.</p>}<label className="environment-data-mode"><input type="checkbox" checked={isDefault} onChange={(e) => setIsDefault(e.target.checked)}/> Default for this source</label><div className="project-settings-actions"><button className="btn btn-primary" disabled={saving || !name.trim()} onClick={() => void saveProfile()}>{editing ? 'Save profile' : 'Create profile'}</button><button className="btn btn-ghost" onClick={() => { setProfileEditorOpen(false); setEditing(null); setName(''); }}>Cancel</button></div></div> : null}
    </section>;
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
            await SetProjectEnvVar(projectId, trimmedKey, value, '', false);
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
