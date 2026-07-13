import {useEffect, useState} from 'react';
import {Clock3, FlaskConical, GitBranch, Pause, Play, SlidersHorizontal, Trash2} from 'lucide-react';
import {CreateSandbox, DeleteSandbox, DeleteSandboxProfile, GetSandboxDetail, ListEnvironments, ListNodes, ListSandboxProfiles, ListSandboxes, PreviewSandbox, ResumeSandbox, SaveSandboxProfile, SuspendSandbox} from '../../wailsjs/go/main/App';
import {deploy, store} from '../../wailsjs/go/models';
import SandboxExtendControl from '../components/SandboxExtendControl';
import SandboxHoursInput from '../components/SandboxHoursInput';
import {useAppDialog} from '../components/AppDialogProvider';
import Dialog from '../components/Dialog';
import PageHeader from '../components/PageHeader';
import './WorkspaceViews.css';

type Props = {
    projects: store.Project[];
    initialSource?: {projectId: number; environmentId: number} | null;
    onOpenSandbox: (projectId: number, environmentId: number) => void;
    onReturnToSource?: (projectId: number, environmentId: number) => void;
    dialogOnly?: boolean;
};

function dateLabel(value: any): string {
    const date = new Date(value);
    return Number.isNaN(date.valueOf()) ? 'Unknown expiry' : date.toLocaleString();
}

/** Lifecycle copy: expiry is not deletion — resources are purged after grace. */
function sandboxLifecycleLabel(sandbox: store.Sandbox): string {
    const expires = dateLabel(sandbox.expiresAt);
    const deletes = dateLabel(sandbox.graceEndsAt);
    if (sandbox.status === 'expired' || sandbox.status === 'cleanup_failed') {
        return `Deletes ${deletes}`;
    }
    if (sandbox.status === 'warning') {
        return `Warning · expires ${expires} · deletes ${deletes}`;
    }
    return `Expires ${expires} · deletes ${deletes}`;
}

export default function SandboxesView({projects, initialSource, onOpenSandbox, onReturnToSource, dialogOnly = false}: Props) {
    const [sandboxes, setSandboxes] = useState<store.Sandbox[]>([]);
    const [projectId, setProjectId] = useState<number>(initialSource?.projectId ?? projects[0]?.id ?? 0);
    const [environments, setEnvironments] = useState<store.Environment[]>([]);
    const [sourceId, setSourceId] = useState<number>(initialSource?.environmentId ?? 0);
    const [name, setName] = useState('');
    const [ttlHours, setTtlHours] = useState(168);
    const [links, setLinks] = useState('');
    const [profiles, setProfiles] = useState<store.SandboxProfile[]>([]);
    const [profileId, setProfileId] = useState(0);
    const [sourceNodes, setSourceNodes] = useState<store.CanvasNode[]>([]);
    const [rules, setRules] = useState<Record<string, deploy.SandboxServiceRule>>({});
    const [preview, setPreview] = useState<deploy.SandboxPreview | null>(null);
    const [detail, setDetail] = useState<deploy.SandboxDetail | null>(null);
    const [profilesOpen, setProfilesOpen] = useState(false);
    const [editingProfile, setEditingProfile] = useState<store.SandboxProfile | null>(null);
    const [profileName, setProfileName] = useState('');
    const [profileScope, setProfileScope] = useState(0);
    const [profileTTL, setProfileTTL] = useState(168);
    const [profileWarning, setProfileWarning] = useState(24);
    const [profileGrace, setProfileGrace] = useState(72);
    const [profileDefault, setProfileDefault] = useState(false);
    const [open, setOpen] = useState(false);
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState('');
    const {confirm, alert} = useAppDialog();

    const refresh = async () => {
        const rows = await Promise.all(projects.map((p) => ListSandboxes(p.id).catch(() => [])));
        setSandboxes(rows.flat());
    };
    useEffect(() => { void refresh(); }, [projects]);
    useEffect(() => {
        if (!projectId) return;
        ListEnvironments(projectId).then((rows) => {
            const next = rows ?? [];
            setEnvironments(next);
            setSourceId((current) => next.some((env) => env.id === current) ? current : (next[0]?.id ?? 0));
        }).catch(() => setEnvironments([]));
    }, [projectId]);
    useEffect(() => {
        if (!projectId) return;
        ListSandboxProfiles(projectId).then((rows) => {
            const next = rows ?? [];
            setProfiles(next);
            setProfileId(next.find((profile) => profile.isDefault)?.id ?? 0);
        }).catch(() => setProfiles([]));
    }, [projectId]);
    useEffect(() => {
        if (!sourceId) return;
        ListNodes(sourceId).then((rows) => setSourceNodes(rows ?? [])).catch(() => setSourceNodes([]));
        setRules({}); setPreview(null);
    }, [sourceId]);
    useEffect(() => {
        if (!initialSource) return;
        setProjectId(initialSource.projectId);
        setSourceId(initialSource.environmentId);
        setOpen(true);
    }, [initialSource]);

    const selectedProject = projects.find((p) => p.id === projectId);
    const source = environments.find((env) => env.id === sourceId);
    const closeCreate = () => {
        setOpen(false);
        if (initialSource) onReturnToSource?.(initialSource.projectId, initialSource.environmentId);
    };
    const loadProfiles = async () => {
        if (!projectId) return;
        const next = await ListSandboxProfiles(projectId).catch(() => []);
        setProfiles(next ?? []);
    };
    const openProfileEditor = (profile?: store.SandboxProfile) => {
        setEditingProfile(profile ?? null);
        setProfileName(profile?.name ?? '');
        setProfileScope(profile?.sourceEnvironmentId ?? 0);
        setProfileDefault(profile?.isDefault ?? false);
        try {
            const plan = JSON.parse(profile?.planJson || '{}');
            setProfileTTL(plan.ttlHours ?? 168); setProfileWarning(plan.warningHours ?? 24); setProfileGrace(plan.graceHours ?? 72);
        } catch { setProfileTTL(168); setProfileWarning(24); setProfileGrace(72); }
    };
    const saveProfile = async () => {
        if (!projectId || !profileName.trim()) return;
        setBusy(true); setError('');
        try {
            await SaveSandboxProfile(store.SandboxProfile.createFrom({id: editingProfile?.id, projectId, sourceEnvironmentId: profileScope, name: profileName.trim(), description: '', isDefault: profileDefault, planJson: JSON.stringify({ttlHours: profileTTL, warningHours: profileWarning, graceHours: profileGrace})}));
            await loadProfiles(); setEditingProfile(null); setProfileName('');
        } catch (e) { setError(String(e)); } finally { setBusy(false); }
    };
    const buildRequest = () => deploy.SandboxCreateRequest.createFrom({
        name: name.trim() || 'sandbox-preview', sourceEnvironmentId: sourceId, profileId: profileId || undefined,
        plan: deploy.SandboxPlan.createFrom({ttlHours, services: Object.values(rules)}),
        links: links.split(',').map((item) => item.trim()).filter(Boolean).map((value) => {
            const [kind, ...rest] = value.split(':');
            return store.SandboxLink.createFrom({kind: rest.length ? kind.trim() : 'reference', value: (rest.length ? rest.join(':') : kind).trim()});
        }),
    });
    const review = async () => {
        if (!sourceId) return;
        setBusy(true); setError('');
        try { setPreview(await PreviewSandbox(buildRequest())); } catch (e) { setError(String(e)); } finally { setBusy(false); }
    };
    const create = async () => {
        if (!sourceId || !name.trim() || busy) return;
        setBusy(true); setError('');
        try {
            const sandbox = await CreateSandbox(buildRequest());
            setOpen(false); setName(''); setLinks(''); await refresh();
            if (sandbox) {
                onOpenSandbox(sandbox.projectId, sandbox.environmentId);
                onReturnToSource?.(sandbox.projectId, sandbox.environmentId);
            }
        } catch (e) { setError(String(e)); } finally { setBusy(false); }
    };
    const updateRule = (nodeId: string, patch: Partial<deploy.SandboxServiceRule>) => setRules((current) => ({...current, [nodeId]: deploy.SandboxServiceRule.createFrom({...current[nodeId], sourceNodeId: nodeId, mode: current[nodeId]?.mode || 'copy', ...patch})}));
    const remove = async (sandbox: store.Sandbox) => {
        if (!await confirm({title: 'Delete sandbox?', message: `Delete "${sandbox.name}" and all of its Draft-managed data?`, detail: 'Containers, routes, the sandbox network, and Draft-managed volumes are permanently removed.', confirmLabel: 'Delete sandbox', danger: true})) return;
        try { await DeleteSandbox(sandbox.id); await refresh(); } catch (e) { void alert({title: 'Could not delete sandbox', message: String(e)}); }
    };

    return <>
        {!dialogOnly ? (<div className="workspace-view">
        <PageHeader title="Sandboxes" description="Isolated, short-lived copies of project environments. Dependencies can be shared only when you choose it." action={<div className="sandbox-page-actions"><button className="btn btn-ghost" onClick={() => { setProfilesOpen(true); openProfileEditor(); }}><SlidersHorizontal size={15}/> Profiles</button><button className="btn btn-primary" onClick={() => setOpen(true)}><FlaskConical size={15}/> New sandbox</button></div>}/>
        <div className="workspace-body workspace-narrow">
            {sandboxes.length === 0 ? <div className="panel panel-empty"><FlaskConical size={18}/> No sandboxes yet. Create a full isolated copy, then selectively share dependencies when needed.</div> : <div className="stack-list">
                {sandboxes.map((sandbox) => {
                    const project = projects.find((p) => p.id === sandbox.projectId);
                    return <article key={sandbox.id} className="sandbox-card">
                        <div className="sandbox-card-main">
                            <div className="sandbox-branch-row"><span className="status-dot"/><GitBranch size={14}/><span className="sandbox-branch">{sandbox.name}</span><span className="tag-pill">{sandbox.status}</span></div>
                            <div className="sandbox-meta-row"><span>{project?.name ?? 'Unknown project'}</span><span className="sandbox-meta-divider">·</span><Clock3 size={12}/><span>{sandboxLifecycleLabel(sandbox)}</span></div>
                        </div>
                        <div className="sandbox-actions"><button className="btn btn-ghost" onClick={() => void GetSandboxDetail(sandbox.id).then(setDetail)}>Details</button><button className="btn btn-ghost" onClick={() => onOpenSandbox(sandbox.projectId, sandbox.environmentId)}>Open</button><SandboxExtendControl sandboxId={sandbox.id} currentExpiresAt={sandbox.expiresAt} onExtended={() => void refresh()} onError={(message) => void alert({title: 'Could not extend sandbox', message})}/><button className="icon-button" onClick={() => void remove(sandbox)} aria-label="Delete sandbox"><Trash2 size={14}/></button></div>
                    </article>;
                })}
            </div>}
        </div>
    </div>) : null}
        {open && <Dialog title="New sandbox" wide onClose={() => { if (!busy) closeCreate(); }} footer={<><button className="btn btn-ghost" disabled={busy} onClick={closeCreate}>Cancel</button><button className="btn btn-ghost" disabled={!sourceId || busy} onClick={() => void review()}><SlidersHorizontal size={14}/> Review plan</button><button className="btn btn-primary" disabled={!sourceId || !name.trim() || busy} onClick={() => void create()}>{busy ? 'Creating…' : 'Create sandbox'}</button></>}>
            {error && <p className="environment-error">{error}</p>}
            <div className="form-field"><label className="form-label">Project</label><select className="input settings-select" value={projectId} onChange={(e) => setProjectId(Number(e.target.value))}>{projects.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}</select></div>
            <div className="form-field"><label className="form-label">Source environment</label><select className="input settings-select" value={sourceId} onChange={(e) => setSourceId(Number(e.target.value))}>{environments.map((env) => <option key={env.id} value={env.id}>{env.name}{env.isDefault ? ' (default)' : ''}</option>)}</select></div>
            <div className="form-field"><label className="form-label">Profile</label><select className="input settings-select" value={profileId} onChange={(e) => { setProfileId(Number(e.target.value)); setPreview(null); }}><option value={0}>Project/source defaults</option>{profiles.map((profile) => <option key={profile.id} value={profile.id}>{profile.name}{profile.isDefault ? ' (default)' : ''}</option>)}</select></div>
            <div className="form-field"><label className="form-label">Sandbox name</label><input className="input" autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="checkout-validation"/></div>
            <SandboxHoursInput label="Lifetime (hours)" value={ttlHours} min={1} onChange={setTtlHours} variant="field"/>
            <div className="form-field"><label className="form-label">Links (optional)</label><input className="input" value={links} onChange={(e) => setLinks(e.target.value)} placeholder="pr:412, ticket:ENG-933"/><p className="environment-source-hint">Links are manual metadata only.</p></div>
            <section className="sandbox-plan-editor"><h3 className="project-settings-section-title">Service plan</h3><p className="environment-source-hint">Copy is the isolated default. Share bridges directly to the source root; omit removes the service from this sandbox.</p>{sourceNodes.map((node) => { const rule = rules[node.id]; return <div className="sandbox-plan-row" key={node.id}><strong>{node.label}</strong><select className="input settings-select" value={rule?.mode ?? 'copy'} onChange={(e) => updateRule(node.id, {mode: e.target.value})}><option value="copy">Copy</option><option value="share">Share source service</option><option value="omit">Omit</option></select>{(rule?.mode ?? 'copy') === 'copy' && <select className="input settings-select" value={rule?.dataMode ?? ''} onChange={(e) => updateRule(node.id, {dataMode: e.target.value || undefined})}><option value="">Profile/default data plan</option><option value="clone">Clone data</option><option value="fresh">Fresh data</option></select>}</div>; })}</section>
            {preview && <section className="sandbox-plan-review"><h3 className="project-settings-section-title">Resolved plan</h3><p>Expires {dateLabel(preview.expiresAt)}; auto-deletes {dateLabel(preview.graceEndsAt)} (after grace). {preview.services.filter((rule) => rule.mode === 'copy').length} copied, {preview.services.filter((rule) => rule.mode === 'share').length} shared, {preview.services.filter((rule) => rule.mode === 'omit').length} omitted.</p>{preview.repositories.length > 0 && <ul>{preview.repositories.map((repo) => <li key={repo.repoRoot}>{repo.repoRoot} · {repo.commitSha.slice(0, 12)}</li>)}</ul>}</section>}
            {selectedProject && source && <p className="environment-source-hint">Creates an isolated sandbox from {selectedProject.name} / {source.name}. Draft will use the project’s sandbox defaults and profile rules.</p>}
        </Dialog>}
        {detail && <Dialog title={`Sandbox · ${detail.sandbox.name}`} onClose={() => setDetail(null)} footer={<><button className="btn btn-ghost" onClick={() => setDetail(null)}>Close</button>{detail.sandbox.status === 'suspended' ? <button className="btn btn-primary" onClick={() => void ResumeSandbox(detail.sandbox.id).then((sandbox) => { setDetail(deploy.SandboxDetail.createFrom({...detail, sandbox})); void refresh(); })}><Play size={14}/> Resume</button> : <button className="btn btn-ghost" onClick={() => void SuspendSandbox(detail.sandbox.id).then((sandbox) => { setDetail(deploy.SandboxDetail.createFrom({...detail, sandbox})); void refresh(); })}><Pause size={14}/> Suspend</button>}</>}><div className="sandbox-detail"><p>Source: <strong>{detail.source.name}</strong> · {sandboxLifecycleLabel(detail.sandbox)}</p><h3 className="project-settings-section-title">Services</h3><ul>{(detail.plan.services ?? []).map((rule) => <li key={rule.sourceNodeId}>{rule.sourceNodeId}: {rule.mode}{rule.dataMode ? ` · ${rule.dataMode}` : ''}</li>)}</ul><h3 className="project-settings-section-title">Repositories</h3><ul>{detail.repositories.map((repo) => <li key={repo.repoRoot}>{repo.repoRoot} · {repo.ref} · {repo.commitSha.slice(0, 12)}</li>)}</ul><h3 className="project-settings-section-title">Manual links</h3>{detail.links.length ? <ul>{detail.links.map((link) => <li key={link.id}>{link.kind}: {link.value}</li>)}</ul> : <p className="settings-hint">No links attached.</p>}</div></Dialog>}
        {profilesOpen && <Dialog title="Sandbox profiles" wide onClose={() => { setProfilesOpen(false); setEditingProfile(null); }} footer={<button className="btn btn-ghost" onClick={() => { setProfilesOpen(false); setEditingProfile(null); }}>Close</button>}><div className="sandbox-profiles"><p className="environment-source-hint">Profiles provide reusable lifecycle defaults. Service copy/share/omit rules remain editable in each sandbox’s review plan.</p><div className="sandbox-profile-layout"><div>{profiles.map((profile) => <div key={profile.id} className="sandbox-profile-card"><div><strong>{profile.name}</strong><span>{profile.sourceEnvironmentId ? environments.find((env) => env.id === profile.sourceEnvironmentId)?.name ?? 'source environment' : 'Project-wide'}{profile.isDefault ? ' · default' : ''}</span></div><div><button className="btn btn-ghost" onClick={() => openProfileEditor(profile)}>Edit</button><button className="btn btn-ghost" onClick={() => void DeleteSandboxProfile(profile.id).then(loadProfiles)}>Delete</button></div></div>)}</div><div className="sandbox-plan-review"><h3 className="project-settings-section-title">{editingProfile ? 'Edit profile' : 'New profile'}</h3><div className="form-field"><label className="form-label">Name</label><input className="input" value={profileName} onChange={(e) => setProfileName(e.target.value)} placeholder="PR preview"/></div><div className="form-field"><label className="form-label">Applies when branching from</label><select className="input settings-select" value={profileScope} onChange={(e) => setProfileScope(Number(e.target.value))}><option value={0}>Any environment in this project</option>{environments.map((env) => <option key={env.id} value={env.id}>{env.name}</option>)}</select></div><div className="sandbox-profile-times"><SandboxHoursInput label="Lifetime" value={profileTTL} min={1} onChange={setProfileTTL}/><SandboxHoursInput label="Warning" value={profileWarning} min={0} onChange={setProfileWarning}/><SandboxHoursInput label="Grace" value={profileGrace} min={0} onChange={setProfileGrace}/></div><label className="environment-data-mode"><input type="checkbox" checked={profileDefault} onChange={(e) => setProfileDefault(e.target.checked)}/> Default for this scope</label>{error && <p className="environment-error">{error}</p>}<button className="btn btn-primary" disabled={busy || !profileName.trim()} onClick={() => void saveProfile()}>{busy ? 'Saving…' : editingProfile ? 'Save profile' : 'Create profile'}</button></div></div></div></Dialog>}
    </>;
}
