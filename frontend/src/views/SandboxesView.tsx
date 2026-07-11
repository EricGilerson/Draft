import {useEffect, useState} from 'react';
import {Clock3, FlaskConical, GitBranch, Plus, Trash2} from 'lucide-react';
import {CreateSandbox, DeleteSandbox, ExtendSandbox, ListEnvironments, ListSandboxes} from '../../wailsjs/go/main/App';
import {deploy, store} from '../../wailsjs/go/models';
import {useAppDialog} from '../components/AppDialogProvider';
import Dialog from '../components/Dialog';
import PageHeader from '../components/PageHeader';
import './WorkspaceViews.css';

type Props = { projects: store.Project[]; initialSource?: {projectId: number; environmentId: number} | null; onOpenSandbox: (projectId: number, environmentId: number) => void; };

function dateLabel(value: any): string {
    const date = new Date(value);
    return Number.isNaN(date.valueOf()) ? 'Unknown expiry' : date.toLocaleString();
}

export default function SandboxesView({projects, initialSource, onOpenSandbox}: Props) {
    const [sandboxes, setSandboxes] = useState<store.Sandbox[]>([]);
    const [projectId, setProjectId] = useState<number>(initialSource?.projectId ?? projects[0]?.id ?? 0);
    const [environments, setEnvironments] = useState<store.Environment[]>([]);
    const [sourceId, setSourceId] = useState<number>(initialSource?.environmentId ?? 0);
    const [name, setName] = useState('');
    const [ttlHours, setTtlHours] = useState(168);
    const [links, setLinks] = useState('');
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
        if (!initialSource) return;
        setProjectId(initialSource.projectId);
        setSourceId(initialSource.environmentId);
        setOpen(true);
    }, [initialSource]);

    const selectedProject = projects.find((p) => p.id === projectId);
    const source = environments.find((env) => env.id === sourceId);
    const create = async () => {
        if (!sourceId || !name.trim() || busy) return;
        setBusy(true); setError('');
        const parsedLinks = links.split(',').map((item) => item.trim()).filter(Boolean).map((value) => {
            const [kind, ...rest] = value.split(':');
            return store.SandboxLink.createFrom({kind: rest.length ? kind.trim() : 'reference', value: (rest.length ? rest.join(':') : kind).trim()});
        });
        try {
            await CreateSandbox(deploy.SandboxCreateRequest.createFrom({name: name.trim(), sourceEnvironmentId: sourceId, plan: deploy.SandboxPlan.createFrom({ttlHours}), links: parsedLinks}));
            setOpen(false); setName(''); setLinks(''); await refresh();
        } catch (e) { setError(String(e)); } finally { setBusy(false); }
    };
    const remove = async (sandbox: store.Sandbox) => {
        if (!await confirm({title: 'Delete sandbox?', message: `Delete "${sandbox.name}" and all of its Draft-managed data?`, detail: 'Containers, routes, the sandbox network, and Draft-managed volumes are permanently removed.', confirmLabel: 'Delete sandbox', danger: true})) return;
        try { await DeleteSandbox(sandbox.id); await refresh(); } catch (e) { void alert({title: 'Could not delete sandbox', message: String(e)}); }
    };

    return <div className="workspace-view">
        <PageHeader title="Sandboxes" description="Isolated, short-lived copies of project environments. Dependencies can be shared only when you choose it." action={<button className="btn btn-primary" onClick={() => setOpen(true)}><FlaskConical size={15}/> New sandbox</button>}/>
        <div className="workspace-body workspace-narrow">
            {sandboxes.length === 0 ? <div className="panel panel-empty"><FlaskConical size={18}/> No sandboxes yet. Create a full isolated copy, then selectively share dependencies when needed.</div> : <div className="stack-list">
                {sandboxes.map((sandbox) => {
                    const project = projects.find((p) => p.id === sandbox.projectId);
                    return <article key={sandbox.id} className="sandbox-card">
                        <div className="sandbox-card-main">
                            <div className="sandbox-branch-row"><span className="status-dot"/><GitBranch size={14}/><span className="sandbox-branch">{sandbox.name}</span><span className="tag-pill">{sandbox.status}</span></div>
                            <div className="sandbox-meta-row"><span>{project?.name ?? 'Unknown project'}</span><span className="sandbox-meta-divider">·</span><Clock3 size={12}/><span>Expires {dateLabel(sandbox.expiresAt)}</span></div>
                        </div>
                        <div className="sandbox-actions"><button className="btn btn-ghost" onClick={() => onOpenSandbox(sandbox.projectId, sandbox.environmentId)}>Open</button><button className="btn btn-ghost" onClick={() => void ExtendSandbox(sandbox.id, 168).then(refresh)}>Extend 7d</button><button className="icon-button" onClick={() => void remove(sandbox)} aria-label="Delete sandbox"><Trash2 size={14}/></button></div>
                    </article>;
                })}
            </div>}
        </div>
        {open && <Dialog title="New sandbox" onClose={() => !busy && setOpen(false)} footer={<><button className="btn btn-ghost" disabled={busy} onClick={() => setOpen(false)}>Cancel</button><button className="btn btn-primary" disabled={!sourceId || !name.trim() || busy} onClick={() => void create()}>{busy ? 'Creating…' : 'Create sandbox'}</button></>}>
            {error && <p className="environment-error">{error}</p>}
            <div className="form-field"><label className="form-label">Project</label><select className="input settings-select" value={projectId} onChange={(e) => setProjectId(Number(e.target.value))}>{projects.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}</select></div>
            <div className="form-field"><label className="form-label">Source environment</label><select className="input settings-select" value={sourceId} onChange={(e) => setSourceId(Number(e.target.value))}>{environments.map((env) => <option key={env.id} value={env.id}>{env.name}{env.isDefault ? ' (default)' : ''}</option>)}</select></div>
            <div className="form-field"><label className="form-label">Sandbox name</label><input className="input" autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="checkout-validation"/></div>
            <div className="form-field"><label className="form-label">Lifetime (hours)</label><input className="input" type="number" min="1" value={ttlHours} onChange={(e) => setTtlHours(Number(e.target.value))}/></div>
            <div className="form-field"><label className="form-label">Links (optional)</label><input className="input" value={links} onChange={(e) => setLinks(e.target.value)} placeholder="pr:412, ticket:ENG-933"/><p className="environment-source-hint">Links are manual metadata only. The default plan copies all services; profile and dependency controls can refine it next.</p></div>
            {selectedProject && source && <p className="environment-source-hint">Creates an isolated sandbox from {selectedProject.name} / {source.name}. Draft will use the project’s sandbox defaults and profile rules.</p>}
        </Dialog>}
    </div>;
}
