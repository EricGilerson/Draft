import {useEffect, useMemo, useState} from 'react';
import {CheckCircle2, Clock3, FlaskConical, GitBranch, Pause, Play, Plus, RefreshCw, RotateCcw, SlidersHorizontal, Trash2, XCircle} from 'lucide-react';
import {
    CreateSandbox,
    DeleteSandbox,
    DeleteSandboxProfile,
    GetSandboxDetail,
    GetSandboxTestRun,
    ListEnvironments,
    ListNodes,
    ListSandboxProfiles,
    ListSandboxSourceRepos,
    ListSandboxes,
    ListSandboxTestRuns,
    PreviewEnvironmentDuplicate,
    PreviewSandbox,
    RefreshSandbox,
    ResumeSandbox,
    RunTestingSandbox,
    SaveSandboxProfile,
    SuspendSandbox,
} from '../../wailsjs/go/main/App';
import {deploy, store} from '../../wailsjs/go/models';
import SandboxExtendControl from '../components/SandboxExtendControl';
import SandboxHoursInput from '../components/SandboxHoursInput';
import {useAppDialog} from '../components/AppDialogProvider';
import Dialog from '../components/Dialog';
import PageHeader from '../components/PageHeader';
import {Skeleton, SkeletonListCards} from '../components/Skeleton';
import './WorkspaceViews.css';

type Props = {
    projects: store.Project[];
    initialSource?: {projectId: number; environmentId: number} | null;
    onOpenSandbox: (projectId: number, environmentId: number) => void;
    onReturnToSource?: (projectId: number, environmentId: number) => void;
    dialogOnly?: boolean;
};

type StepDraft = {
    name: string;
    serviceLabel: string;
    cmd: string;
    workDir: string;
};

/** Per-repo source picker state for the create dialog. */
type RepoSourceDraft = {
    repoRoot: string;
    mode: 'keep' | 'branch' | 'pr';
    ref: string;
    commitSha: string;
    prNumber: number;
    prTitle: string;
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

function parseCmdLine(line: string): string[] {
    const parts = line.trim().match(/(?:[^\s"]+|"[^"]*")+/g) ?? [];
    return parts.map((p) => (p.startsWith('"') && p.endsWith('"') ? p.slice(1, -1) : p)).filter(Boolean);
}

function stepsFromPlan(plan: any): StepDraft[] {
    const steps = Array.isArray(plan?.steps) ? plan.steps : [];
    return steps.map((step: any) => ({
        name: step.name ?? '',
        serviceLabel: step.serviceLabel ?? '',
        cmd: Array.isArray(step.cmd) ? step.cmd.join(' ') : '',
        workDir: step.workDir ?? '',
    }));
}

function stepsToPlan(steps: StepDraft[]): deploy.SandboxStep[] {
    return steps
        .filter((step) => step.serviceLabel.trim() && step.cmd.trim())
        .map((step) => deploy.SandboxStep.createFrom({
            name: step.name.trim() || undefined,
            serviceLabel: step.serviceLabel.trim(),
            cmd: parseCmdLine(step.cmd),
            workDir: step.workDir.trim() || undefined,
        }));
}

function emptyStep(serviceLabel = ''): StepDraft {
    return {name: '', serviceLabel, cmd: '', workDir: ''};
}

function purposeLabel(purpose?: string): string {
    return purpose === 'test' ? 'test' : 'preview';
}

function shortSha(sha?: string): string {
    const value = (sha ?? '').trim();
    return value ? value.slice(0, 12) : '—';
}

function slugifyRef(value: string): string {
    return value
        .trim()
        .toLowerCase()
        .replace(/^origin\//, '')
        .replace(/[^a-z0-9]+/g, '-')
        .replace(/^-+|-+$/g, '')
        .slice(0, 48);
}

function repoLeaf(path: string): string {
    const cleaned = path.replace(/[\\/]+$/, '');
    const parts = cleaned.split(/[\\/]/);
    return parts[parts.length - 1] || path;
}

function draftsFromSourceRepos(repos: deploy.SandboxSourceRepos | null): Record<string, RepoSourceDraft> {
    const next: Record<string, RepoSourceDraft> = {};
    for (const repo of repos?.repositories ?? []) {
        next[repo.repoRoot] = {
            repoRoot: repo.repoRoot,
            mode: 'keep',
            ref: repo.defaultRef || 'HEAD',
            commitSha: '',
            prNumber: 0,
            prTitle: '',
        };
    }
    return next;
}

export default function SandboxesView({projects, initialSource, onOpenSandbox, onReturnToSource, dialogOnly = false}: Props) {
    const [sandboxes, setSandboxes] = useState<store.Sandbox[]>([]);
    const [testRuns, setTestRuns] = useState<store.SandboxTestRun[]>([]);
    const [projectId, setProjectId] = useState<number>(initialSource?.projectId ?? projects[0]?.id ?? 0);
    const [environments, setEnvironments] = useState<store.Environment[]>([]);
    const [sourceId, setSourceId] = useState<number>(initialSource?.environmentId ?? 0);
    const [name, setName] = useState('');
    const [purpose, setPurpose] = useState<'preview' | 'test'>('preview');
    const [ttlHours, setTtlHours] = useState(168);
    const [onComplete, setOnComplete] = useState<'leave' | 'delete' | 'suspend'>('leave');
    const [steps, setSteps] = useState<StepDraft[]>([emptyStep()]);
    const [links, setLinks] = useState('');
    const [profiles, setProfiles] = useState<store.SandboxProfile[]>([]);
    const [profileId, setProfileId] = useState(0);
    const [sourceNodes, setSourceNodes] = useState<store.CanvasNode[]>([]);
    /** Node IDs in the source env that have managed volume mounts (clone-capable). */
    const [nodesWithVolumes, setNodesWithVolumes] = useState<Record<string, boolean>>({});
    const [sourceRepos, setSourceRepos] = useState<deploy.SandboxSourceRepos | null>(null);
    const [repoDrafts, setRepoDrafts] = useState<Record<string, RepoSourceDraft>>({});
    const [sourceReposLoading, setSourceReposLoading] = useState(false);
    const [rules, setRules] = useState<Record<string, deploy.SandboxServiceRule>>({});
    const [preview, setPreview] = useState<deploy.SandboxPreview | null>(null);
    const [detail, setDetail] = useState<deploy.SandboxDetail | null>(null);
    const [runResult, setRunResult] = useState<deploy.SandboxTestRunResult | null>(null);
    const [profilesOpen, setProfilesOpen] = useState(false);
    const [editingProfile, setEditingProfile] = useState<store.SandboxProfile | null>(null);
    const [profileName, setProfileName] = useState('');
    const [profileScope, setProfileScope] = useState(0);
    const [profileTTL, setProfileTTL] = useState(168);
    const [profileWarning, setProfileWarning] = useState(24);
    const [profileGrace, setProfileGrace] = useState(72);
    const [profileDefault, setProfileDefault] = useState(false);
    const [profilePurpose, setProfilePurpose] = useState<'preview' | 'test'>('preview');
    const [profileOnComplete, setProfileOnComplete] = useState<'leave' | 'delete' | 'suspend'>('leave');
    const [profileSteps, setProfileSteps] = useState<StepDraft[]>([emptyStep()]);
    const [open, setOpen] = useState(false);
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState('');
    const [loading, setLoading] = useState(true);
    const {confirm, alert} = useAppDialog();

    const refresh = async () => {
        try {
            const rows = await Promise.all(projects.map((p) => ListSandboxes(p.id).catch(() => [])));
            setSandboxes(rows.flat());
            const runs = await Promise.all(projects.map((p) => ListSandboxTestRuns(p.id, 30).catch(() => [])));
            setTestRuns(runs.flat().sort((a, b) => new Date(b.startedAt).valueOf() - new Date(a.startedAt).valueOf()));
        } finally {
            setLoading(false);
        }
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
        ListNodes(sourceId).then((rows) => {
            const next = rows ?? [];
            setSourceNodes(next);
            setSteps((current) => {
                if (current.length === 1 && !current[0].serviceLabel && next[0]) {
                    return [emptyStep(next[0].label)];
                }
                return current;
            });
        }).catch(() => setSourceNodes([]));
        // Same volume detection as env-duplicate wizard: only managed volume
        // mounts get a clone/fresh data dropdown.
        PreviewEnvironmentDuplicate(sourceId)
            .then((rows) => {
                const next: Record<string, boolean> = {};
                for (const svc of rows ?? []) {
                    next[svc.nodeId] = (svc.volumes?.length ?? 0) > 0;
                }
                setNodesWithVolumes(next);
            })
            .catch(() => setNodesWithVolumes({}));
        setRules({}); setPreview(null);
    }, [sourceId]);
    useEffect(() => {
        if (!sourceId || (!open && !dialogOnly)) {
            return;
        }
        let cancelled = false;
        setSourceReposLoading(true);
        ListSandboxSourceRepos(sourceId)
            .then((repos) => {
                if (cancelled) return;
                setSourceRepos(repos);
                setRepoDrafts(draftsFromSourceRepos(repos));
            })
            .catch(() => {
                if (cancelled) return;
                setSourceRepos(null);
                setRepoDrafts({});
            })
            .finally(() => {
                if (!cancelled) setSourceReposLoading(false);
            });
        return () => { cancelled = true; };
    }, [sourceId, open, dialogOnly]);
    useEffect(() => {
        if (!initialSource) return;
        setProjectId(initialSource.projectId);
        setSourceId(initialSource.environmentId);
        setOpen(true);
    }, [initialSource]);

    const selectedProject = projects.find((p) => p.id === projectId);
    const source = environments.find((env) => env.id === sourceId);
    const defaultServiceLabel = sourceNodes[0]?.label ?? '';
    const repositoryPlan = useMemo(() => {
        return Object.values(repoDrafts)
            .filter((draft) => draft.mode !== 'keep' && (draft.ref.trim() || draft.commitSha.trim() || draft.prNumber > 0))
            .map((draft) => deploy.SandboxRepositoryRef.createFrom({
                repoRoot: draft.repoRoot,
                // Prefer the PR head branch name; backend rewrites to a deployable
                // ref (origin/<branch> or origin/pr/<n> after fetch) when needed.
                ref: draft.ref.trim() || (draft.prNumber > 0 ? `pr-${draft.prNumber}` : draft.commitSha.trim()),
                commitSha: draft.commitSha.trim() || undefined,
                prNumber: draft.mode === 'pr' && draft.prNumber > 0 ? draft.prNumber : undefined,
            }));
    }, [repoDrafts]);

    const testingProfiles = useMemo(
        () => profiles.filter((profile) => {
            try {
                const plan = JSON.parse(profile.planJson || '{}');
                return plan.purpose === 'test';
            } catch {
                return false;
            }
        }),
        [profiles],
    );

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
            setProfileTTL(plan.ttlHours ?? (plan.purpose === 'test' ? 4 : 168));
            setProfileWarning(plan.warningHours ?? (plan.purpose === 'test' ? 1 : 24));
            setProfileGrace(plan.graceHours ?? (plan.purpose === 'test' ? 2 : 72));
            setProfilePurpose(plan.purpose === 'test' ? 'test' : 'preview');
            setProfileOnComplete(plan.onComplete === 'delete' || plan.onComplete === 'suspend' ? plan.onComplete : 'leave');
            const nextSteps = stepsFromPlan(plan);
            setProfileSteps(nextSteps.length ? nextSteps : [emptyStep(defaultServiceLabel)]);
        } catch {
            setProfileTTL(168); setProfileWarning(24); setProfileGrace(72);
            setProfilePurpose('preview'); setProfileOnComplete('leave');
            setProfileSteps([emptyStep(defaultServiceLabel)]);
        }
    };
    const saveProfile = async () => {
        if (!projectId || !profileName.trim()) return;
        setBusy(true); setError('');
        try {
            const plan: Record<string, unknown> = {
                ttlHours: profileTTL,
                warningHours: profileWarning,
                graceHours: profileGrace,
                purpose: profilePurpose,
            };
            if (profilePurpose === 'test') {
                plan.onComplete = profileOnComplete;
                plan.steps = stepsToPlan(profileSteps);
            }
            await SaveSandboxProfile(store.SandboxProfile.createFrom({
                id: editingProfile?.id,
                projectId,
                sourceEnvironmentId: profileScope,
                name: profileName.trim(),
                description: profilePurpose === 'test' ? 'Testing recipe' : '',
                isDefault: profileDefault,
                planJson: JSON.stringify(plan),
            }));
            await loadProfiles(); setEditingProfile(null); setProfileName('');
        } catch (e) { setError(String(e)); } finally { setBusy(false); }
    };

    const buildLinks = () => {
        const parsed = links.split(',').map((item) => item.trim()).filter(Boolean).map((value) => {
            const [kind, ...rest] = value.split(':');
            return store.SandboxLink.createFrom({
                kind: rest.length ? kind.trim() : 'reference',
                value: (rest.length ? rest.join(':') : kind).trim(),
            });
        });
        // Ensure PR selections appear as structured links even if the freeform field is empty.
        for (const draft of Object.values(repoDrafts)) {
            if (draft.mode === 'pr' && draft.prNumber > 0) {
                const value = String(draft.prNumber);
                if (!parsed.some((link) => link.kind === 'pr' && link.value === value)) {
                    parsed.push(store.SandboxLink.createFrom({
                        kind: 'pr',
                        value,
                        label: draft.prTitle || undefined,
                    }));
                }
            }
        }
        return parsed;
    };

    const buildPlan = () => {
        const plan = deploy.SandboxPlan.createFrom({
            ttlHours,
            purpose,
            services: Object.values(rules),
            repositories: repositoryPlan.length ? repositoryPlan : undefined,
        });
        if (purpose === 'test') {
            plan.onComplete = onComplete;
            plan.steps = stepsToPlan(steps);
        }
        return plan;
    };

    const buildRequest = (startOnCreate = purpose === 'preview') => deploy.SandboxCreateRequest.createFrom({
        name: name.trim() || (purpose === 'test' ? 'test-run' : 'sandbox-preview'),
        sourceEnvironmentId: sourceId,
        profileId: profileId || undefined,
        plan: buildPlan(),
        links: buildLinks(),
        startOnCreate: startOnCreate && purpose === 'preview',
    });

    const updateRepoDraft = (repoRoot: string, patch: Partial<RepoSourceDraft>) => {
        setRepoDrafts((current) => {
            const base = current[repoRoot] ?? {
                repoRoot,
                mode: 'keep' as const,
                ref: '',
                commitSha: '',
                prNumber: 0,
                prTitle: '',
            };
            return {...current, [repoRoot]: {...base, ...patch, repoRoot}};
        });
        setPreview(null);
    };

    const applyPrToDraft = (repo: deploy.SandboxSourceRepo, prNumber: number) => {
        const pr = (repo.pullRequests ?? []).find((item) => item.number === prNumber);
        if (!pr) {
            updateRepoDraft(repo.repoRoot, {mode: 'pr', prNumber: 0, prTitle: '', ref: '', commitSha: ''});
            return;
        }
        updateRepoDraft(repo.repoRoot, {
            mode: 'pr',
            prNumber: pr.number,
            prTitle: pr.title,
            ref: pr.headRef,
            commitSha: pr.headSha || '',
        });
        if (!name.trim()) {
            setName(`pr-${pr.number}`);
        }
        const prToken = `pr:${pr.number}`;
        setLinks((current) => {
            const parts = current.split(',').map((p) => p.trim()).filter(Boolean);
            if (parts.some((p) => p === prToken || p.startsWith(`pr:${pr.number}`))) return current;
            return [...parts, prToken].join(', ');
        });
    };

    const suggestNameFromBranch = (ref: string) => {
        if (name.trim()) return;
        const slug = slugifyRef(ref);
        if (slug) setName(slug);
    };

    const review = async () => {
        if (!sourceId) return;
        setBusy(true); setError('');
        try { setPreview(await PreviewSandbox(buildRequest(false))); } catch (e) { setError(String(e)); } finally { setBusy(false); }
    };

    const create = async () => {
        if (!sourceId || !name.trim() || busy) return;
        setBusy(true); setError('');
        try {
            if (purpose === 'test') {
                const result = await RunTestingSandbox(deploy.SandboxTestRunRequest.createFrom({
                    name: name.trim(),
                    sourceEnvironmentId: sourceId,
                    profileId: profileId || undefined,
                    plan: buildPlan(),
                    links: buildLinks(),
                    mode: 'fresh',
                }));
                setOpen(false);
                setName('');
                setLinks('');
                setRunResult(result);
                await refresh();
                if (result.sandbox) {
                    onOpenSandbox(result.sandbox.projectId, result.sandbox.environmentId);
                    onReturnToSource?.(result.sandbox.projectId, result.sandbox.environmentId);
                }
            } else {
                const result = await CreateSandbox(buildRequest(true));
                setOpen(false); setName(''); setLinks(''); await refresh();
                if (result.startError) {
                    void alert({
                        title: 'Sandbox created, start incomplete',
                        message: result.startError,
                        detail: 'The sandbox environment exists. Open it and deploy individual services if needed.',
                    });
                }
                if (result.sandbox) {
                    onOpenSandbox(result.sandbox.projectId, result.sandbox.environmentId);
                    onReturnToSource?.(result.sandbox.projectId, result.sandbox.environmentId);
                }
            }
        } catch (e) { setError(String(e)); } finally { setBusy(false); }
    };

    const refreshSandboxSource = async (sandbox: store.Sandbox, mode: 'tip' | 'same') => {
        setBusy(true);
        try {
            const result = await RefreshSandbox(sandbox.id, mode);
            await refresh();
            if (detail?.sandbox.id === sandbox.id && result.sandbox) {
                const next = await GetSandboxDetail(sandbox.id);
                setDetail(next);
            }
        } catch (e) {
            void alert({
                title: mode === 'tip' ? 'Could not refresh to branch tip' : 'Could not redeploy at pinned SHA',
                message: String(e),
            });
        } finally {
            setBusy(false);
        }
    };

    const runFreshFromProfile = async (profile: store.SandboxProfile) => {
        if (!sourceId) {
            void alert({title: 'Pick a source environment', message: 'Select a project and source environment before running a testing profile.'});
            return;
        }
        setBusy(true); setError('');
        try {
            let plan: any = {};
            try { plan = JSON.parse(profile.planJson || '{}'); } catch { plan = {}; }
            const result = await RunTestingSandbox(deploy.SandboxTestRunRequest.createFrom({
                name: profile.name,
                sourceEnvironmentId: sourceId,
                profileId: profile.id,
                plan: deploy.SandboxPlan.createFrom({...plan, purpose: 'test'}),
                mode: 'fresh',
            }));
            setRunResult(result);
            await refresh();
            if (result.sandbox) {
                onOpenSandbox(result.sandbox.projectId, result.sandbox.environmentId);
            }
        } catch (e) {
            void alert({title: 'Test run failed', message: String(e)});
        } finally {
            setBusy(false);
        }
    };

    const rerunSandbox = async (sandbox: store.Sandbox, mode: 'fresh' | 'steps') => {
        setBusy(true);
        try {
            const result = await RunTestingSandbox(deploy.SandboxTestRunRequest.createFrom({
                name: sandbox.name,
                sourceEnvironmentId: sandbox.sourceEnvironmentId,
                profileId: sandbox.profileId || undefined,
                sandboxId: sandbox.id,
                plan: deploy.SandboxPlan.createFrom({purpose: 'test'}),
                mode,
            }));
            setRunResult(result);
            await refresh();
            if (result.sandbox && mode === 'fresh') {
                onOpenSandbox(result.sandbox.projectId, result.sandbox.environmentId);
            }
        } catch (e) {
            void alert({title: mode === 'fresh' ? 'Could not rebuild test sandbox' : 'Could not re-run steps', message: String(e)});
        } finally {
            setBusy(false);
        }
    };

    const openRun = async (run: store.SandboxTestRun) => {
        try {
            setRunResult(await GetSandboxTestRun(run.id));
        } catch (e) {
            void alert({title: 'Could not load test run', message: String(e)});
        }
    };

    const updateRule = (nodeId: string, patch: Partial<deploy.SandboxServiceRule>) => setRules((current) => ({
        ...current,
        [nodeId]: deploy.SandboxServiceRule.createFrom({
            ...current[nodeId],
            sourceNodeId: nodeId,
            mode: current[nodeId]?.mode || 'copy',
            ...patch,
        }),
    }));

    const remove = async (sandbox: store.Sandbox) => {
        if (!await confirm({
            title: 'Delete sandbox?',
            message: `Delete "${sandbox.name}" and all of its Draft-managed data?`,
            detail: 'Containers, routes, the sandbox network, and Draft-managed volumes are permanently removed. Test run history is kept.',
            confirmLabel: 'Delete sandbox',
            danger: true,
        })) return;
        try { await DeleteSandbox(sandbox.id); await refresh(); } catch (e) { void alert({title: 'Could not delete sandbox', message: String(e)}); }
    };

    const setPurposeAndDefaults = (next: 'preview' | 'test') => {
        setPurpose(next);
        setPreview(null);
        if (next === 'test') {
            setTtlHours((current) => (current === 168 ? 4 : current));
            setSteps((current) => (current.length ? current : [emptyStep(defaultServiceLabel)]));
        } else {
            setTtlHours((current) => (current === 4 ? 168 : current));
        }
    };

    const renderStepEditor = (
        value: StepDraft[],
        onChange: (next: StepDraft[]) => void,
        serviceOptions: store.CanvasNode[],
    ) => (
        <section className="sandbox-plan-editor">
            <h3 className="project-settings-section-title">Test steps</h3>
            <p className="environment-source-hint">
                Commands run in the sandbox service container after the stack is healthy. Use shell-style quoting for args with spaces.
            </p>
            {value.map((step, index) => (
                <div className="sandbox-step-row" key={index}>
                    <input
                        className="input"
                        placeholder="Name (optional)"
                        value={step.name}
                        onChange={(e) => {
                            const next = [...value];
                            next[index] = {...step, name: e.target.value};
                            onChange(next);
                        }}
                    />
                    <select
                        className="input settings-select"
                        value={step.serviceLabel}
                        onChange={(e) => {
                            const next = [...value];
                            next[index] = {...step, serviceLabel: e.target.value};
                            onChange(next);
                        }}
                    >
                        <option value="">Service</option>
                        {serviceOptions.map((node) => (
                            <option key={node.id} value={node.label}>{node.label}</option>
                        ))}
                    </select>
                    <input
                        className="input"
                        placeholder="pytest -q tests/integration"
                        value={step.cmd}
                        onChange={(e) => {
                            const next = [...value];
                            next[index] = {...step, cmd: e.target.value};
                            onChange(next);
                        }}
                    />
                    <input
                        className="input"
                        placeholder="Workdir (optional)"
                        value={step.workDir}
                        onChange={(e) => {
                            const next = [...value];
                            next[index] = {...step, workDir: e.target.value};
                            onChange(next);
                        }}
                    />
                    <button
                        className="icon-button"
                        type="button"
                        aria-label="Remove step"
                        onClick={() => onChange(value.filter((_, i) => i !== index))}
                        disabled={value.length <= 1}
                    >
                        <Trash2 size={14}/>
                    </button>
                </div>
            ))}
            <button
                className="btn btn-ghost"
                type="button"
                onClick={() => onChange([...value, emptyStep(serviceOptions[0]?.label ?? '')])}
            >
                <Plus size={14}/> Add step
            </button>
        </section>
    );

    return <>
        {!dialogOnly ? (<div className="workspace-view">
            <PageHeader
                title="Sandboxes"
                description="Isolated, short-lived environment copies. Preview sandboxes for PR work; testing sandboxes rebuild a recipe and run commands."
                action={
                    <div className="sandbox-page-actions">
                        <button className="btn btn-ghost" onClick={() => { setProfilesOpen(true); openProfileEditor(); }}>
                            <SlidersHorizontal size={15}/> Profiles
                        </button>
                        <button className="btn btn-primary" onClick={() => { setPurpose('preview'); setOpen(true); }}>
                            <FlaskConical size={15}/> New sandbox
                        </button>
                    </div>
                }
            />
            <div className="workspace-body workspace-narrow">
                {testingProfiles.length > 0 && (
                    <section className="sandbox-recipes">
                        <h3 className="project-settings-section-title">Testing recipes</h3>
                        <p className="environment-source-hint">Saved testing profiles. Run rebuilds a short-lived sandbox from the recipe and executes steps.</p>
                        <div className="stack-list">
                            {testingProfiles.map((profile) => (
                                <article key={profile.id} className="sandbox-card sandbox-card--recipe">
                                    <div className="sandbox-card-main">
                                        <div className="sandbox-branch-row">
                                            <FlaskConical size={14}/>
                                            <span className="sandbox-branch">{profile.name}</span>
                                            <span className="tag-pill">test recipe</span>
                                            {profile.isDefault ? <span className="tag-pill">default</span> : null}
                                        </div>
                                        <div className="sandbox-meta-row">
                                            <span>{profile.sourceEnvironmentId
                                                ? environments.find((env) => env.id === profile.sourceEnvironmentId)?.name ?? 'Scoped source'
                                                : 'Project-wide'}</span>
                                        </div>
                                    </div>
                                    <div className="sandbox-actions">
                                        <button className="btn btn-ghost" onClick={() => { setProfilesOpen(true); openProfileEditor(profile); }}>Edit</button>
                                        <button className="btn btn-primary" disabled={busy || !sourceId} onClick={() => void runFreshFromProfile(profile)}>
                                            <Play size={14}/> Run
                                        </button>
                                    </div>
                                </article>
                            ))}
                        </div>
                    </section>
                )}

                <section className="sandbox-live-section">
                    <h3 className="project-settings-section-title">Live sandboxes</h3>
                    {loading ? (
                        <SkeletonListCards count={3} withActions />
                    ) : sandboxes.length === 0 ? (
                        <div className="panel panel-empty">
                            <FlaskConical size={18}/> No sandboxes yet. Create a preview copy, or a testing sandbox with steps to rerun.
                        </div>
                    ) : (
                        <div className="stack-list">
                            {sandboxes.map((sandbox) => {
                                const project = projects.find((p) => p.id === sandbox.projectId);
                                const isTest = sandbox.purpose === 'test';
                                return (
                                    <article key={sandbox.id} className="sandbox-card">
                                        <div className="sandbox-card-main">
                                            <div className="sandbox-branch-row">
                                                <span className="status-dot"/>
                                                <GitBranch size={14}/>
                                                <span className="sandbox-branch">{sandbox.name}</span>
                                                <span className="tag-pill">{purposeLabel(sandbox.purpose)}</span>
                                                <span className="tag-pill">{sandbox.status}</span>
                                            </div>
                                            <div className="sandbox-meta-row">
                                                <span>{project?.name ?? 'Unknown project'}</span>
                                                <span className="sandbox-meta-divider">·</span>
                                                <Clock3 size={12}/>
                                                <span>{sandboxLifecycleLabel(sandbox)}</span>
                                            </div>
                                        </div>
                                        <div className="sandbox-actions">
                                            <button className="btn btn-ghost" onClick={() => void GetSandboxDetail(sandbox.id).then(setDetail)}>Details</button>
                                            <button className="btn btn-ghost" onClick={() => onOpenSandbox(sandbox.projectId, sandbox.environmentId)}>Open</button>
                                            {!isTest && (
                                                <>
                                                    <button className="btn btn-ghost" disabled={busy} title="Re-resolve branch/PR refs and redeploy" onClick={() => void refreshSandboxSource(sandbox, 'tip')}>
                                                        <RefreshCw size={14}/> Refresh tip
                                                    </button>
                                                    <button className="btn btn-ghost" disabled={busy} title="Redeploy at the frozen commit SHAs" onClick={() => void refreshSandboxSource(sandbox, 'same')}>
                                                        <RotateCcw size={14}/> Same SHA
                                                    </button>
                                                </>
                                            )}
                                            {isTest && (
                                                <>
                                                    <button className="btn btn-ghost" disabled={busy} title="Re-run steps on this stack" onClick={() => void rerunSandbox(sandbox, 'steps')}>
                                                        <Play size={14}/> Re-run steps
                                                    </button>
                                                    <button className="btn btn-primary" disabled={busy} title="Rebuild from recipe and run" onClick={() => void rerunSandbox(sandbox, 'fresh')}>
                                                        <RotateCcw size={14}/> Rerun fresh
                                                    </button>
                                                </>
                                            )}
                                            <SandboxExtendControl
                                                sandboxId={sandbox.id}
                                                currentExpiresAt={sandbox.expiresAt}
                                                onExtended={() => void refresh()}
                                                onError={(message) => void alert({title: 'Could not extend sandbox', message})}
                                            />
                                            <button className="icon-button" onClick={() => void remove(sandbox)} aria-label="Delete sandbox">
                                                <Trash2 size={14}/>
                                            </button>
                                        </div>
                                    </article>
                                );
                            })}
                        </div>
                    )}
                </section>

                {testRuns.length > 0 && (
                    <section className="sandbox-run-history">
                        <h3 className="project-settings-section-title">Recent test runs</h3>
                        <div className="stack-list">
                            {testRuns.slice(0, 20).map((run) => {
                                const project = projects.find((p) => p.id === run.projectId);
                                const StatusIcon = run.status === 'passed' ? CheckCircle2 : run.status === 'failed' ? XCircle : Clock3;
                                return (
                                    <article key={run.id} className="sandbox-card sandbox-card--run">
                                        <div className="sandbox-card-main">
                                            <div className="sandbox-branch-row">
                                                <StatusIcon size={14}/>
                                                <span className="sandbox-branch">{run.name}</span>
                                                <span className="tag-pill">{run.status}</span>
                                                <span className="tag-pill">{run.mode}</span>
                                            </div>
                                            <div className="sandbox-meta-row">
                                                <span>{project?.name ?? 'Unknown project'}</span>
                                                <span className="sandbox-meta-divider">·</span>
                                                <span>{dateLabel(run.startedAt)}</span>
                                                {run.error ? <><span className="sandbox-meta-divider">·</span><span className="sandbox-run-error">{run.error}</span></> : null}
                                            </div>
                                        </div>
                                        <div className="sandbox-actions">
                                            <button className="btn btn-ghost" onClick={() => void openRun(run)}>View</button>
                                        </div>
                                    </article>
                                );
                            })}
                        </div>
                    </section>
                )}
            </div>
        </div>) : null}

        {open && (
            <Dialog
                title={purpose === 'test' ? 'New testing sandbox' : 'New sandbox'}
                wide
                onClose={() => { if (!busy) closeCreate(); }}
                footer={
                    <>
                        <button className="btn btn-ghost" disabled={busy} onClick={closeCreate}>Cancel</button>
                        <button className="btn btn-ghost" disabled={!sourceId || busy} onClick={() => void review()}>
                            <SlidersHorizontal size={14}/> Review plan
                        </button>
                        <button className="btn btn-primary" disabled={!sourceId || !name.trim() || busy} onClick={() => void create()}>
                            {busy
                                ? (purpose === 'test' ? 'Running…' : 'Creating…')
                                : (purpose === 'test' ? 'Create & run' : 'Create & start')}
                        </button>
                    </>
                }
            >
                {error && <p className="environment-error">{error}</p>}
                <div className="form-field">
                    <label className="form-label">Purpose</label>
                    <select className="input settings-select" value={purpose} onChange={(e) => setPurposeAndDefaults(e.target.value as 'preview' | 'test')}>
                        <option value="preview">Preview — short-lived PR / feature copy</option>
                        <option value="test">Testing — rebuildable recipe + commands</option>
                    </select>
                </div>
                <div className="form-field">
                    <label className="form-label">Project</label>
                    <select className="input settings-select" value={projectId} onChange={(e) => setProjectId(Number(e.target.value))}>
                        {projects.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
                    </select>
                </div>
                <div className="form-field">
                    <label className="form-label">Source environment</label>
                    <select className="input settings-select" value={sourceId} onChange={(e) => setSourceId(Number(e.target.value))}>
                        {environments.map((env) => <option key={env.id} value={env.id}>{env.name}{env.isDefault ? ' (default)' : ''}</option>)}
                    </select>
                </div>
                <div className="form-field">
                    <label className="form-label">Profile</label>
                    <select className="input settings-select" value={profileId} onChange={(e) => { setProfileId(Number(e.target.value)); setPreview(null); }}>
                        <option value={0}>Project/source defaults</option>
                        {profiles.map((profile) => <option key={profile.id} value={profile.id}>{profile.name}{profile.isDefault ? ' (default)' : ''}</option>)}
                    </select>
                </div>
                <div className="form-field">
                    <label className="form-label">Sandbox name</label>
                    <input className="input" autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder={purpose === 'test' ? 'api-integration' : 'pr-412-checkout'}/>
                </div>
                <SandboxHoursInput label="Lifetime (hours)" value={ttlHours} min={1} onChange={setTtlHours} variant="field"/>
                {purpose === 'test' && (
                    <div className="form-field">
                        <label className="form-label">After suite completes</label>
                        <select className="input settings-select" value={onComplete} onChange={(e) => setOnComplete(e.target.value as 'leave' | 'delete' | 'suspend')}>
                            <option value="leave">Leave running (short TTL)</option>
                            <option value="suspend">Suspend services</option>
                            <option value="delete">Delete immediately</option>
                        </select>
                    </div>
                )}
                <div className="form-field">
                    <label className="form-label">Links (optional)</label>
                    <input className="input" value={links} onChange={(e) => setLinks(e.target.value)} placeholder="pr:412, ticket:ENG-933"/>
                    <p className="environment-source-hint">
                        Metadata only (PR/ticket/URL). Choosing a GitHub PR below adds <code>pr:N</code> automatically.
                    </p>
                </div>

                <section className="sandbox-plan-editor">
                    <h3 className="project-settings-section-title">Source code</h3>
                    <p className="environment-source-hint">
                        Pin sandbox <strong>copies</strong> to a branch, ref, or PR without changing the durable environment.
                        Shared services keep the source environment&apos;s code. Only committed git objects are used (not the dirty working tree).
                    </p>
                    {sourceReposLoading ? (
                        <div className="sandbox-plan-editor" style={{gap: 10, display: 'flex', flexDirection: 'column'}}>
                            {Array.from({length: 2}, (_, i) => (
                                <div key={i} className="sandbox-source-repo">
                                    <div className="sandbox-source-repo-head">
                                        <GitBranch size={14}/>
                                        <Skeleton width={i === 0 ? '30%' : '24%'} height={13} />
                                    </div>
                                    <Skeleton width="55%" height={10} style={{marginTop: 6}} />
                                    <Skeleton width="70%" height={30} style={{marginTop: 10}} />
                                </div>
                            ))}
                        </div>
                    ) : !(sourceRepos?.repositories?.length) ? (
                        <p className="settings-hint">
                            No git repositories found for services in this environment. Image-only services skip this section.
                        </p>
                    ) : (
                        (sourceRepos.repositories ?? []).map((repo) => {
                            const draft = repoDrafts[repo.repoRoot] ?? {
                                repoRoot: repo.repoRoot,
                                mode: 'keep' as const,
                                ref: repo.defaultRef || 'HEAD',
                                commitSha: '',
                                prNumber: 0,
                                prTitle: '',
                            };
                            const branches = repo.branches ?? [];
                            const prs = repo.pullRequests ?? [];
                            return (
                                <div className="sandbox-source-repo" key={repo.repoRoot}>
                                    <div className="sandbox-source-repo-head">
                                        <GitBranch size={14}/>
                                        <strong title={repo.repoRoot}>{repoLeaf(repo.repoRoot)}</strong>
                                        <span className="sandbox-source-repo-path" title={repo.repoRoot}>{repo.repoRoot}</span>
                                    </div>
                                    <p className="environment-source-hint">
                                        Services: {(repo.serviceLabels ?? []).join(', ') || '—'}
                                        {repo.defaultRef ? ` · source pin ${repo.defaultRef}` : ''}
                                    </p>
                                    <div className="sandbox-source-modes">
                                        <label className="sandbox-source-mode">
                                            <input
                                                type="radio"
                                                name={`src-mode-${repo.repoRoot}`}
                                                checked={draft.mode === 'keep'}
                                                onChange={() => updateRepoDraft(repo.repoRoot, {
                                                    mode: 'keep',
                                                    ref: repo.defaultRef || 'HEAD',
                                                    commitSha: '',
                                                    prNumber: 0,
                                                    prTitle: '',
                                                })}
                                            />
                                            Keep source pins
                                        </label>
                                        <label className="sandbox-source-mode">
                                            <input
                                                type="radio"
                                                name={`src-mode-${repo.repoRoot}`}
                                                checked={draft.mode === 'branch'}
                                                onChange={() => updateRepoDraft(repo.repoRoot, {
                                                    mode: 'branch',
                                                    prNumber: 0,
                                                    prTitle: '',
                                                    ref: draft.ref || repo.defaultRef || 'HEAD',
                                                })}
                                            />
                                            Branch / ref
                                        </label>
                                        {repo.pullRequestsAvailable ? (
                                            <label className="sandbox-source-mode">
                                                <input
                                                    type="radio"
                                                    name={`src-mode-${repo.repoRoot}`}
                                                    checked={draft.mode === 'pr'}
                                                    onChange={() => updateRepoDraft(repo.repoRoot, {mode: 'pr'})}
                                                />
                                                Pull request
                                            </label>
                                        ) : (
                                            <span className="sandbox-source-mode sandbox-source-mode--disabled" title={repo.pullRequestsError || 'GitHub CLI unavailable'}>
                                                PRs unavailable
                                            </span>
                                        )}
                                    </div>
                                    {draft.mode === 'branch' && (
                                        <div className="sandbox-source-controls">
                                            <select
                                                className="input settings-select"
                                                value={branches.includes(draft.ref) ? draft.ref : ''}
                                                onChange={(e) => {
                                                    const ref = e.target.value;
                                                    updateRepoDraft(repo.repoRoot, {ref, commitSha: ''});
                                                    suggestNameFromBranch(ref);
                                                }}
                                            >
                                                <option value="">Select branch…</option>
                                                {branches.map((branch) => (
                                                    <option key={branch} value={branch}>{branch}</option>
                                                ))}
                                            </select>
                                            <input
                                                className="input"
                                                value={draft.ref}
                                                onChange={(e) => updateRepoDraft(repo.repoRoot, {ref: e.target.value, commitSha: ''})}
                                                onBlur={(e) => suggestNameFromBranch(e.target.value)}
                                                placeholder="branch, tag, or SHA"
                                            />
                                        </div>
                                    )}
                                    {draft.mode === 'pr' && repo.pullRequestsAvailable && (
                                        <div className="sandbox-source-controls">
                                            <select
                                                className="input settings-select"
                                                value={draft.prNumber || ''}
                                                onChange={(e) => applyPrToDraft(repo, Number(e.target.value) || 0)}
                                            >
                                                <option value="">Select open PR…</option>
                                                {prs.map((pr) => (
                                                    <option key={pr.number} value={pr.number}>
                                                        #{pr.number} {pr.title}{pr.headRef ? ` (${pr.headRef})` : ''}
                                                    </option>
                                                ))}
                                            </select>
                                            {draft.prNumber > 0 && (
                                                <p className="environment-source-hint">
                                                    Head {draft.ref || '—'}
                                                    {draft.commitSha ? ` · ${shortSha(draft.commitSha)}` : ''}
                                                    {draft.prTitle ? ` · ${draft.prTitle}` : ''}
                                                </p>
                                            )}
                                            {!prs.length && (
                                                <p className="settings-hint">No open pull requests found for this repository.</p>
                                            )}
                                        </div>
                                    )}
                                    {!repo.pullRequestsAvailable && repo.pullRequestsError && (
                                        <p className="settings-hint">PRs: {repo.pullRequestsError}</p>
                                    )}
                                </div>
                            );
                        })
                    )}
                </section>

                <section className="sandbox-plan-editor">
                    <h3 className="project-settings-section-title">Service plan</h3>
                    <p className="environment-source-hint">
                        Copy is the isolated default{purpose === 'test' ? ' (testing defaults data to fresh)' : ''}.
                        Share bridges directly to the source root; omit removes the service from this sandbox.
                    </p>
                    {sourceNodes.map((node) => {
                        const rule = rules[node.id];
                        const mode = rule?.mode ?? 'copy';
                        const hasVolumes = !!nodesWithVolumes[node.id];
                        return (
                            <div className={`sandbox-plan-row${hasVolumes ? '' : ' sandbox-plan-row--no-data'}`} key={node.id}>
                                <strong>{node.label}</strong>
                                <select
                                    className="input settings-select"
                                    value={mode}
                                    onChange={(e) => {
                                        const nextMode = e.target.value;
                                        // Drop clone/fresh when switching away from copy, or when
                                        // the service has no managed volumes to choose over.
                                        if (nextMode !== 'copy' || !hasVolumes) {
                                            updateRule(node.id, {mode: nextMode, dataMode: undefined, consistency: undefined});
                                        } else {
                                            updateRule(node.id, {mode: nextMode});
                                        }
                                    }}
                                >
                                    <option value="copy">Copy</option>
                                    <option value="share">Share source service</option>
                                    <option value="omit">Omit</option>
                                </select>
                                {mode === 'copy' && hasVolumes && (
                                    <select className="input settings-select" value={rule?.dataMode ?? ''} onChange={(e) => updateRule(node.id, {dataMode: e.target.value || undefined})}>
                                        <option value="">Profile/default data plan</option>
                                        <option value="clone">Clone data</option>
                                        <option value="fresh">Fresh data</option>
                                    </select>
                                )}
                            </div>
                        );
                    })}
                </section>
                {purpose === 'test' && renderStepEditor(steps, setSteps, sourceNodes)}
                {preview && (
                    <section className="sandbox-plan-review">
                        <h3 className="project-settings-section-title">Resolved plan</h3>
                        <p>
                            Purpose {purposeLabel(preview.plan.purpose)} · expires {dateLabel(preview.expiresAt)}; auto-deletes {dateLabel(preview.graceEndsAt)} (after grace).{' '}
                            {preview.services.filter((rule) => rule.mode === 'copy').length} copied,{' '}
                            {preview.services.filter((rule) => rule.mode === 'share').length} shared,{' '}
                            {preview.services.filter((rule) => rule.mode === 'omit').length} omitted.
                        </p>
                        {(preview.plan.steps?.length ?? 0) > 0 && (
                            <ul>
                                {preview.plan.steps!.map((step, i) => (
                                    <li key={i}>{step.serviceLabel}: {(step.cmd ?? []).join(' ')}</li>
                                ))}
                            </ul>
                        )}
                        {(preview.repositories?.length ?? 0) > 0 && (
                            <>
                                <h3 className="project-settings-section-title">Repositories</h3>
                                <ul>
                                    {preview.repositories.map((repo) => (
                                        <li key={repo.repoRoot}>
                                            {repoLeaf(repo.repoRoot)} · {repo.ref || 'HEAD'} · {shortSha(repo.commitSha)}
                                        </li>
                                    ))}
                                </ul>
                            </>
                        )}
                    </section>
                )}
                {selectedProject && source && (
                    <p className="environment-source-hint">
                        {purpose === 'test'
                            ? `Creates a testing sandbox from ${selectedProject.name} / ${source.name}, starts services, and runs steps. Durable env settings are not changed.`
                            : `Creates an isolated sandbox from ${selectedProject.name} / ${source.name}, pins selected sources, and starts services. Durable env settings are not changed.`}
                    </p>
                )}
            </Dialog>
        )}

        {detail && (
            <Dialog
                title={`Sandbox · ${detail.sandbox.name}`}
                onClose={() => setDetail(null)}
                footer={
                    <>
                        <button className="btn btn-ghost" onClick={() => setDetail(null)}>Close</button>
                        {detail.sandbox.purpose === 'test' ? (
                            <>
                                <button className="btn btn-ghost" disabled={busy} onClick={() => void rerunSandbox(detail.sandbox, 'steps')}>Re-run steps</button>
                                <button className="btn btn-primary" disabled={busy} onClick={() => void rerunSandbox(detail.sandbox, 'fresh')}>Rerun fresh</button>
                            </>
                        ) : (
                            <>
                                <button className="btn btn-ghost" disabled={busy} onClick={() => void refreshSandboxSource(detail.sandbox, 'tip')}>
                                    <RefreshCw size={14}/> Refresh tip
                                </button>
                                <button className="btn btn-ghost" disabled={busy} onClick={() => void refreshSandboxSource(detail.sandbox, 'same')}>
                                    <RotateCcw size={14}/> Same SHA
                                </button>
                            </>
                        )}
                        {detail.sandbox.status === 'suspended' ? (
                            <button className="btn btn-primary" onClick={() => void ResumeSandbox(detail.sandbox.id).then((sandbox) => {
                                setDetail(deploy.SandboxDetail.createFrom({...detail, sandbox}));
                                void refresh();
                            })}>
                                <Play size={14}/> Resume
                            </button>
                        ) : (
                            <button className="btn btn-ghost" onClick={() => void SuspendSandbox(detail.sandbox.id).then((sandbox) => {
                                setDetail(deploy.SandboxDetail.createFrom({...detail, sandbox}));
                                void refresh();
                            })}>
                                <Pause size={14}/> Suspend
                            </button>
                        )}
                    </>
                }
            >
                <div className="sandbox-detail">
                    <p>
                        Source: <strong>{detail.source.name}</strong> · {purposeLabel(detail.sandbox.purpose)} · {sandboxLifecycleLabel(detail.sandbox)}
                    </p>
                    <h3 className="project-settings-section-title">Services</h3>
                    <ul>
                        {(detail.plan.services ?? []).map((rule) => (
                            <li key={rule.sourceNodeId}>{rule.sourceNodeId}: {rule.mode}{rule.dataMode ? ` · ${rule.dataMode}` : ''}</li>
                        ))}
                    </ul>
                    {(detail.plan.steps?.length ?? 0) > 0 && (
                        <>
                            <h3 className="project-settings-section-title">Test steps</h3>
                            <ul>
                                {detail.plan.steps!.map((step, i) => (
                                    <li key={i}>{step.name || step.serviceLabel}: {(step.cmd ?? []).join(' ')}</li>
                                ))}
                            </ul>
                        </>
                    )}
                    {detail.latestRun && (
                        <>
                            <h3 className="project-settings-section-title">Latest run</h3>
                            <p>
                                {detail.latestRun.status} · {detail.latestRun.mode} · {dateLabel(detail.latestRun.startedAt)}
                                {detail.latestRun.error ? ` · ${detail.latestRun.error}` : ''}
                            </p>
                            <button className="btn btn-ghost" onClick={() => void openRun(detail.latestRun!)}>View run transcript</button>
                        </>
                    )}
                    <h3 className="project-settings-section-title">Repositories</h3>
                    {(detail.repositories?.length ?? 0) === 0 ? (
                        <p className="settings-hint">No repository pins (image-only or keep-source create).</p>
                    ) : (
                        <ul>
                            {detail.repositories.map((repo) => (
                                <li key={repo.repoRoot}>
                                    <strong>{repoLeaf(repo.repoRoot)}</strong>
                                    {' · '}
                                    <span title={repo.ref}>{repo.ref || 'HEAD'}</span>
                                    {' · '}
                                    <code title={repo.commitSha}>{shortSha(repo.commitSha)}</code>
                                </li>
                            ))}
                        </ul>
                    )}
                    <h3 className="project-settings-section-title">Links</h3>
                    {detail.links.length ? (
                        <ul>{detail.links.map((link) => (
                            <li key={link.id}>
                                {link.kind}: {link.value}{link.label ? ` · ${link.label}` : ''}
                            </li>
                        ))}</ul>
                    ) : (
                        <p className="settings-hint">No links attached.</p>
                    )}
                </div>
            </Dialog>
        )}

        {runResult && (
            <Dialog
                title={`Test run · ${runResult.run.name}`}
                wide
                onClose={() => setRunResult(null)}
                footer={
                    <>
                        <button className="btn btn-ghost" onClick={() => setRunResult(null)}>Close</button>
                        {runResult.sandbox && (
                            <button className="btn btn-primary" onClick={() => {
                                onOpenSandbox(runResult.sandbox!.projectId, runResult.sandbox!.environmentId);
                                setRunResult(null);
                            }}>
                                Open sandbox
                            </button>
                        )}
                    </>
                }
            >
                <div className="sandbox-detail">
                    <p>
                        Status: <strong>{runResult.run.status}</strong> · mode {runResult.run.mode}
                        {runResult.run.error ? ` · ${runResult.run.error}` : ''}
                    </p>
                    <p>Started {dateLabel(runResult.run.startedAt)}{runResult.run.finishedAt ? ` · finished ${dateLabel(runResult.run.finishedAt)}` : ''}</p>
                    <h3 className="project-settings-section-title">Steps</h3>
                    {(runResult.steps ?? []).length === 0 ? (
                        <p className="settings-hint">No step results recorded.</p>
                    ) : (
                        <div className="sandbox-run-steps">
                            {runResult.steps.map((step, i) => (
                                <article key={i} className="sandbox-run-step">
                                    <div className="sandbox-branch-row">
                                        {step.exitCode === 0 && !step.error ? <CheckCircle2 size={14}/> : <XCircle size={14}/>}
                                        <strong>{step.name || step.serviceLabel}</strong>
                                        <span className="tag-pill">{step.serviceLabel}</span>
                                        <span className="tag-pill">exit {step.exitCode}</span>
                                        <span className="tag-pill">{step.durationMs}ms</span>
                                    </div>
                                    {step.error && <p className="environment-error">{step.error}</p>}
                                    {step.output && <pre className="sandbox-run-output">{step.output}</pre>}
                                </article>
                            ))}
                        </div>
                    )}
                </div>
            </Dialog>
        )}

        {profilesOpen && (
            <Dialog
                title="Sandbox profiles"
                wide
                onClose={() => { setProfilesOpen(false); setEditingProfile(null); }}
                footer={<button className="btn btn-ghost" onClick={() => { setProfilesOpen(false); setEditingProfile(null); }}>Close</button>}
            >
                <div className="sandbox-profiles">
                    <p className="environment-source-hint">
                        Profiles are reusable recipes. Mark purpose as Testing to save steps and rebuild with Run.
                    </p>
                    <div className="sandbox-profile-layout">
                        <div>
                            {profiles.map((profile) => {
                                let profilePurposeTag = 'preview';
                                try {
                                    const plan = JSON.parse(profile.planJson || '{}');
                                    if (plan.purpose === 'test') profilePurposeTag = 'test';
                                } catch { /* ignore */ }
                                return (
                                    <div key={profile.id} className="sandbox-profile-card">
                                        <div>
                                            <strong>{profile.name}</strong>
                                            <span>
                                                {profile.sourceEnvironmentId
                                                    ? environments.find((env) => env.id === profile.sourceEnvironmentId)?.name ?? 'source environment'
                                                    : 'Project-wide'}
                                                {profile.isDefault ? ' · default' : ''}
                                                {' · '}{profilePurposeTag}
                                            </span>
                                        </div>
                                        <div>
                                            <button className="btn btn-ghost" onClick={() => openProfileEditor(profile)}>Edit</button>
                                            {profilePurposeTag === 'test' && (
                                                <button className="btn btn-ghost" disabled={busy} onClick={() => void runFreshFromProfile(profile)}>Run</button>
                                            )}
                                            <button className="btn btn-ghost" onClick={() => void DeleteSandboxProfile(profile.id).then(loadProfiles)}>Delete</button>
                                        </div>
                                    </div>
                                );
                            })}
                        </div>
                        <div className="sandbox-plan-review">
                            <h3 className="project-settings-section-title">{editingProfile ? 'Edit profile' : 'New profile'}</h3>
                            {error && <p className="environment-error">{error}</p>}
                            <div className="form-field">
                                <label className="form-label">Name</label>
                                <input className="input" value={profileName} onChange={(e) => setProfileName(e.target.value)} placeholder="API integration"/>
                            </div>
                            <div className="form-field">
                                <label className="form-label">Purpose</label>
                                <select
                                    className="input settings-select"
                                    value={profilePurpose}
                                    onChange={(e) => {
                                        const next = e.target.value as 'preview' | 'test';
                                        setProfilePurpose(next);
                                        if (next === 'test') {
                                            setProfileTTL((v) => (v === 168 ? 4 : v));
                                            setProfileWarning((v) => (v === 24 ? 1 : v));
                                            setProfileGrace((v) => (v === 72 ? 2 : v));
                                        }
                                    }}
                                >
                                    <option value="preview">Preview</option>
                                    <option value="test">Testing</option>
                                </select>
                            </div>
                            <div className="form-field">
                                <label className="form-label">Applies when branching from</label>
                                <select className="input settings-select" value={profileScope} onChange={(e) => setProfileScope(Number(e.target.value))}>
                                    <option value={0}>Any environment in this project</option>
                                    {environments.map((env) => <option key={env.id} value={env.id}>{env.name}</option>)}
                                </select>
                            </div>
                            <div className="sandbox-profile-times">
                                <SandboxHoursInput label="Lifetime" value={profileTTL} min={1} onChange={setProfileTTL}/>
                                <SandboxHoursInput label="Warning" value={profileWarning} min={0} onChange={setProfileWarning}/>
                                <SandboxHoursInput label="Grace" value={profileGrace} min={0} onChange={setProfileGrace}/>
                            </div>
                            {profilePurpose === 'test' && (
                                <>
                                    <div className="form-field">
                                        <label className="form-label">After suite completes</label>
                                        <select className="input settings-select" value={profileOnComplete} onChange={(e) => setProfileOnComplete(e.target.value as 'leave' | 'delete' | 'suspend')}>
                                            <option value="leave">Leave running</option>
                                            <option value="suspend">Suspend</option>
                                            <option value="delete">Delete</option>
                                        </select>
                                    </div>
                                    {renderStepEditor(profileSteps, setProfileSteps, sourceNodes)}
                                </>
                            )}
                            <label className="environment-data-mode">
                                <input type="checkbox" checked={profileDefault} onChange={(e) => setProfileDefault(e.target.checked)}/>
                                Default profile for this scope
                            </label>
                            <button className="btn btn-primary" disabled={busy || !profileName.trim()} onClick={() => void saveProfile()}>
                                {editingProfile ? 'Save profile' : 'Create profile'}
                            </button>
                        </div>
                    </div>
                </div>
            </Dialog>
        )}
    </>;
}
