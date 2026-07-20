import {useEffect, useMemo, useRef, useState} from 'react';
import {Clock3, FlaskConical, GitBranch, GitCompare, MoreHorizontal, Package, Play, Plus, Power, RefreshCw, Trash2} from 'lucide-react';
import {
    CreateEnvironment,
    DeleteEnvironment,
    DeleteSandbox,
    DuplicateEnvironment,
    ListEnvironments,
    ListSandboxSourceRepos,
    ListSandboxes,
    PreviewEnvironmentDuplicate,
    PreviewSandboxPurge,
    RedeployEnvironment,
    RefreshSandbox,
    RenameEnvironment,
    ResumeSandbox,
    SetDefaultEnvironment,
    StartEnvironment,
    StopEnvironment,
    SuspendSandbox,
} from '../../wailsjs/go/main/App';
import {deploy, store} from '../../wailsjs/go/models';
import {useAppDialog} from './AppDialogProvider';
import Dialog from './Dialog';
import ExportDraftPackDialog from './ExportDraftPackDialog';
import SandboxExtendControl from './SandboxExtendControl';
import {Skeleton} from './Skeleton';
import SyncConfigDialog from './SyncConfigDialog';
import './EnvironmentSwitcher.css';

/** Per-repo source picker for duplicate create (keep / branch / PR). */
type RepoSourceDraft = {
    repoRoot: string;
    mode: 'keep' | 'branch' | 'pr';
    ref: string;
    commitSha: string;
    prNumber: number;
    prTitle: string;
};

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

function sandboxExpiryLabel(value: unknown): string {
    const date = new Date(value as string | number | Date);
    return Number.isNaN(date.valueOf()) ? 'Unknown expiry' : date.toLocaleString();
}

/** Relative TTL for the sandbox banner (updates via parent refresh / tick). */
function sandboxRelativeLabel(value: unknown, prefix: string): string {
    const date = new Date(value as string | number | Date);
    if (Number.isNaN(date.valueOf())) return `${prefix} unknown`;
    const ms = date.valueOf() - Date.now();
    const abs = Math.abs(ms);
    const minutes = Math.round(abs / 60_000);
    if (minutes < 1) return ms >= 0 ? `${prefix} in under a minute` : `${prefix} just now`;
    if (minutes < 60) return ms >= 0 ? `${prefix} in ${minutes}m` : `${prefix} ${minutes}m ago`;
    const hours = Math.round(minutes / 60);
    if (hours < 48) return ms >= 0 ? `${prefix} in ${hours}h` : `${prefix} ${hours}h ago`;
    const days = Math.round(hours / 24);
    return ms >= 0 ? `${prefix} in ${days}d` : `${prefix} ${days}d ago`;
}

/** Confirm dialog tone tracks lifecycle: strongest while live, softest in grace. */
function sandboxDeleteConfirm(sandbox: store.Sandbox): {
    title: string;
    message: string;
    detail: string;
    confirmLabel: string;
    danger: boolean;
} {
    const name = sandbox.name || 'this sandbox';
    const status = sandbox.status || 'active';

    if (status === 'expired' || status === 'cleanup_failed') {
        const scheduled = sandboxExpiryLabel(sandbox.graceEndsAt);
        if (status === 'cleanup_failed') {
            return {
                title: 'Retry sandbox delete?',
                message: `Previous cleanup of "${name}" failed. Try again?`,
                detail: 'This removes any remaining containers, routes, network, and Draft-managed volumes.',
                confirmLabel: 'Retry delete',
                danger: true,
            };
        }
        return {
            title: 'Remove expired sandbox?',
            message: `"${name}" has already expired and is only waiting out its grace period.`,
            detail: `Auto-delete is scheduled for ${scheduled}. Removing it now just frees Docker resources early.`,
            confirmLabel: 'Remove now',
            danger: false,
        };
    }

    if (status === 'warning') {
        return {
            title: 'Delete sandbox?',
            message: `"${name}" is already in its warning window. Delete it now?`,
            detail: `Draft will auto-delete after grace ends (${sandboxExpiryLabel(sandbox.graceEndsAt)}). Deleting now frees containers, routes, and Draft-managed volumes immediately.`,
            confirmLabel: 'Delete now',
            danger: true,
        };
    }

    // active, suspended, or any other live status — most careful wording
    return {
        title: 'Delete sandbox?',
        message: `Delete "${name}" and all of its Draft-managed data?`,
        detail: 'Containers, routes, the sandbox network, and Draft-managed volumes are permanently removed. This cannot be undone.',
        confirmLabel: 'Delete sandbox',
        danger: true,
    };
}

/** Sentinel for "start empty" in the source dropdown. */
const SOURCE_BLANK = '';

type DataMode = 'fresh' | 'share' | 'clone';

type ChoiceState = {
    mode: DataMode;
    consistency: 'consistent' | 'quick';
};

type EnvironmentSwitcherProps = {
    projectId: number;
    selectedEnvironmentId: number | null;
    onSelect: (environmentId: number) => void;
    onDuplicating?: (duplicating: boolean) => void;
    onEnvironmentsChanged?: () => void;
    onStackActionDone?: () => void;
    onServicesChanged?: () => void;
    onCreateSandbox?: (sourceEnvironmentId: number) => void;
};

export default function EnvironmentSwitcher({
    projectId,
    selectedEnvironmentId,
    onSelect,
    onDuplicating,
    onEnvironmentsChanged,
    onStackActionDone,
    onServicesChanged,
    onCreateSandbox,
}: EnvironmentSwitcherProps) {
    const [environments, setEnvironments] = useState<store.Environment[]>([]);
    const [sandboxes, setSandboxes] = useState<store.Sandbox[]>([]);
    const [dialogOpen, setDialogOpen] = useState(false);
    const [syncOpen, setSyncOpen] = useState(false);
    const [exportPackOpen, setExportPackOpen] = useState(false);
    const [step, setStep] = useState<1 | 2>(1);
    const [name, setName] = useState('');
    /** Empty string = blank env; otherwise the source environment id. */
    const [sourceId, setSourceId] = useState(SOURCE_BLANK);
    const [stateful, setStateful] = useState<deploy.StatefulServiceSummary[]>([]);
    const [choices, setChoices] = useState<Record<string, ChoiceState>>({});
    /** Opt-in start after duplicate (off by default — durable envs can be heavy). */
    const [startAfter, setStartAfter] = useState(false);
    const [sourceRepos, setSourceRepos] = useState<deploy.SandboxSourceRepos | null>(null);
    const [repoDrafts, setRepoDrafts] = useState<Record<string, RepoSourceDraft>>({});
    const [sourceReposLoading, setSourceReposLoading] = useState(false);
    const [submitting, setSubmitting] = useState(false);
    const [error, setError] = useState('');

    const repositoryPins = useMemo(() => {
        return Object.values(repoDrafts)
            .filter((draft) => draft.mode !== 'keep' && (draft.ref.trim() || draft.commitSha.trim() || draft.prNumber > 0))
            .map((draft) => deploy.SandboxRepositoryRef.createFrom({
                repoRoot: draft.repoRoot,
                ref: draft.ref.trim() || (draft.prNumber > 0 ? `pr-${draft.prNumber}` : draft.commitSha.trim()),
                commitSha: draft.commitSha.trim() || undefined,
                prNumber: draft.mode === 'pr' && draft.prNumber > 0 ? draft.prNumber : undefined,
            }));
    }, [repoDrafts]);
    const [menuEnvId, setMenuEnvId] = useState<number | null>(null);
    const [renameEnv, setRenameEnv] = useState<store.Environment | null>(null);
    const [renameValue, setRenameValue] = useState('');
    const [stackBusy, setStackBusy] = useState(false);
    const [sandboxBusy, setSandboxBusy] = useState(false);
    const [nowTick, setNowTick] = useState(() => Date.now());
    const menuRef = useRef<HTMLDivElement | null>(null);
    const {confirm, alert} = useAppDialog();

    const refresh = () => {
        ListEnvironments(projectId).then((envs) => setEnvironments(envs ?? [])).catch(() => setEnvironments([]));
        ListSandboxes(projectId).then((rows) => setSandboxes(rows ?? [])).catch(() => setSandboxes([]));
    };

    useEffect(() => {
        refresh();
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [projectId, selectedEnvironmentId]);

    useEffect(() => {
        const id = window.setInterval(() => setNowTick(Date.now()), 60_000);
        return () => window.clearInterval(id);
    }, []);

    useEffect(() => {
        if (menuEnvId == null) return;
        const onDoc = (e: MouseEvent) => {
            if (menuRef.current && !menuRef.current.contains(e.target as Node)) {
                setMenuEnvId(null);
            }
        };
        document.addEventListener('mousedown', onDoc);
        return () => document.removeEventListener('mousedown', onDoc);
    }, [menuEnvId]);

    // Prefetch step-2 data while the user is still on step 1 so Source code
    // and service choices are ready when they click Next.
    useEffect(() => {
        if (!dialogOpen || sourceId === SOURCE_BLANK) {
            if (sourceId === SOURCE_BLANK) {
                setStateful([]);
                setChoices({});
                setSourceRepos(null);
                setRepoDrafts({});
                setSourceReposLoading(false);
            }
            return;
        }
        const envId = Number(sourceId);
        let cancelled = false;
        setStateful([]);
        setChoices({});
        setSourceRepos(null);
        setRepoDrafts({});
        setSourceReposLoading(true);
        PreviewEnvironmentDuplicate(envId)
            .then((list) => {
                if (cancelled) return;
                const rows = list ?? [];
                setStateful(rows);
                const next: Record<string, ChoiceState> = {};
                for (const row of rows) {
                    next[row.nodeId] = {mode: 'fresh', consistency: 'consistent'};
                }
                setChoices(next);
            })
            .catch(() => {
                if (cancelled) return;
                setStateful([]);
                setChoices({});
            });
        ListSandboxSourceRepos(envId)
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
        return () => {
            cancelled = true;
        };
    }, [dialogOpen, sourceId]);

    const openNewDialog = () => {
        setName('');
        setError('');
        setSubmitting(false);
        setStep(1);
        setStateful([]);
        setChoices({});
        setStartAfter(false);
        setSourceRepos(null);
        setRepoDrafts({});
        setSourceReposLoading(false);
        setSourceId(
            selectedEnvironmentId != null ? String(selectedEnvironmentId) : SOURCE_BLANK,
        );
        setDialogOpen(true);
    };

    const closeDialog = () => {
        setDialogOpen(false);
        setName('');
        setSourceId(SOURCE_BLANK);
        setError('');
        setSubmitting(false);
        setStep(1);
        setStateful([]);
        setChoices({});
        setStartAfter(false);
        setSourceRepos(null);
        setRepoDrafts({});
        setSourceReposLoading(false);
    };

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
    };

    const goNext = () => {
        if (name.trim() === '') return;
        if (sourceId === SOURCE_BLANK) {
            submit();
            return;
        }
        setStep(2);
    };

    const submit = () => {
        if (name.trim() === '' || submitting) return;
        setSubmitting(true);
        setError('');

        const isDuplicate = sourceId !== SOURCE_BLANK;
        const dataChoices: deploy.ServiceDataChoice[] = isDuplicate
            ? Object.entries(choices).map(([sourceNodeId, c]) => {
                const svc = stateful.find((row) => row.nodeId === sourceNodeId);
                const hasVolumes = (svc?.volumes?.length ?? 0) > 0;
                const mode = c.mode === 'clone' && !hasVolumes ? 'fresh' : c.mode;
                return deploy.ServiceDataChoice.createFrom({
                    sourceNodeId,
                    mode,
                    consistency: mode === 'clone' ? c.consistency : undefined,
                });
            })
            : [];

        if (isDuplicate) onDuplicating?.(true);

        if (isDuplicate) {
            DuplicateEnvironment(Number(sourceId), name.trim(), dataChoices, startAfter, repositoryPins)
                .then((result) => {
                    const env = result.environment;
                    closeDialog();
                    onDuplicating?.(false);
                    refresh();
                    onEnvironmentsChanged?.();
                    onStackActionDone?.();
                    if (env) onSelect(env.id);
                    if (result.startError) {
                        void alert({
                            title: 'Environment created, start incomplete',
                            message: result.startError,
                            detail: 'The environment exists. Open it and deploy individual services if needed.',
                        });
                    }
                })
                .catch((e) => {
                    setError(String(e));
                    setSubmitting(false);
                    onDuplicating?.(false);
                });
            return;
        }

        CreateEnvironment(projectId, name.trim())
            .then((env) => {
                closeDialog();
                refresh();
                onEnvironmentsChanged?.();
                onSelect(env.id);
            })
            .catch((e) => {
                setError(String(e));
                setSubmitting(false);
            });
    };

    const removeEnvironment = async (env: store.Environment) => {
        if (env.isDefault) return;
        if (!await confirm({
            title: 'Delete environment?',
            message: `Delete environment "${env.name}"?`,
            detail: 'This stops and removes its services. Shared roots used by other environments cannot be deleted until unlinked.',
            confirmLabel: 'Delete',
            danger: true,
        })) return;
        setMenuEnvId(null);
        DeleteEnvironment(env.id).then(() => {
            refresh();
            onEnvironmentsChanged?.();
            if (selectedEnvironmentId === env.id) {
                const fallback = environments.find((e) => e.isDefault);
                if (fallback) onSelect(fallback.id);
            }
        }).catch((e) => {
            void alert({
                title: 'Could not delete environment',
                message: String(e),
            });
        });
    };

    const removeSandbox = async (sandbox: store.Sandbox) => {
        let detail = sandboxDeleteConfirm(sandbox).detail;
        if (sandbox.status === 'cleanup_failed') {
            try {
                const inv = await PreviewSandboxPurge(sandbox.id);
                const pending = (inv?.items ?? []).filter((i) => i.status === 'pending' || i.status === 'failed');
                if (pending.length > 0) {
                    detail = `Still present: ${pending.map((i) => `${i.kind} ${i.label || i.id}`).join(', ')}.${
                        sandbox.cleanupError ? ` Last error: ${sandbox.cleanupError}` : ''
                    }`;
                } else if (sandbox.cleanupError) {
                    detail = `Last error: ${sandbox.cleanupError}`;
                }
            } catch {
                if (sandbox.cleanupError) detail = `Last error: ${sandbox.cleanupError}`;
            }
        }
        const options = {...sandboxDeleteConfirm(sandbox), detail};
        if (!await confirm(options)) return;
        try {
            await DeleteSandbox(sandbox.id);
            refresh();
            onEnvironmentsChanged?.();
            onServicesChanged?.();
            if (selectedEnvironmentId === sandbox.environmentId) {
                const source = environments.find((e) => e.id === sandbox.sourceEnvironmentId);
                const fallback = source ?? environments.find((e) => e.isDefault);
                if (fallback) onSelect(fallback.id);
            }
        } catch (e) {
            void alert({
                title: 'Could not delete sandbox',
                message: String(e),
            });
        }
    };

    const refreshSelectedSandbox = async (mode: 'tip' | 'same') => {
        if (!selectedSandbox || sandboxBusy) return;
        setSandboxBusy(true);
        try {
            await RefreshSandbox(selectedSandbox.id, mode);
            refresh();
            onStackActionDone?.();
            onServicesChanged?.();
        } catch (e) {
            void alert({
                title: mode === 'tip' ? 'Could not refresh to tip' : 'Could not rebuild at same SHA',
                message: String(e),
            });
        } finally {
            setSandboxBusy(false);
        }
    };

    const toggleSandboxSuspend = async () => {
        if (!selectedSandbox || sandboxBusy) return;
        setSandboxBusy(true);
        try {
            if (selectedSandbox.status === 'suspended') {
                await ResumeSandbox(selectedSandbox.id);
            } else {
                await SuspendSandbox(selectedSandbox.id);
            }
            refresh();
            onStackActionDone?.();
            onServicesChanged?.();
        } catch (e) {
            void alert({
                title: selectedSandbox.status === 'suspended' ? 'Could not resume sandbox' : 'Could not suspend sandbox',
                message: String(e),
            });
        } finally {
            setSandboxBusy(false);
        }
    };

    const setAsDefault = async (env: store.Environment) => {
        setMenuEnvId(null);
        try {
            await SetDefaultEnvironment(env.id);
            refresh();
            onEnvironmentsChanged?.();
        } catch (e) {
            void alert({
                title: 'Could not set default',
                message: String(e),
            });
        }
    };

    const openRename = (env: store.Environment) => {
        setMenuEnvId(null);
        setRenameEnv(env);
        setRenameValue(env.name);
        setError('');
    };

    const submitRename = () => {
        if (!renameEnv || renameValue.trim() === '' || submitting) return;
        setSubmitting(true);
        setError('');
        RenameEnvironment(renameEnv.id, renameValue.trim())
            .then(() => {
                setRenameEnv(null);
                setSubmitting(false);
                refresh();
                onEnvironmentsChanged?.();
            })
            .catch((e) => {
                setError(String(e));
                setSubmitting(false);
            });
    };

    const runStack = async (action: 'start' | 'stop' | 'redeploy') => {
        if (selectedEnvironmentId == null || stackBusy) return;
        const env = environments.find((e) => e.id === selectedEnvironmentId);
        const label = env?.name ?? 'environment';

        if (action === 'stop') {
            if (!await confirm({
                title: 'Stop all services?',
                message: `Stop every service in "${label}"?`,
                detail: 'Linked services that point at another environment are skipped.',
                confirmLabel: 'Stop all',
                danger: true,
            })) return;
        }

        setStackBusy(true);
        setMenuEnvId(null);
        try {
            const fn =
                action === 'start' ? StartEnvironment
                    : action === 'stop' ? StopEnvironment
                        : RedeployEnvironment;
            const result = await fn(selectedEnvironmentId);
            onStackActionDone?.();
            if (result && result.failed > 0) {
                const lines = (result.results ?? [])
                    .filter((r) => r.error)
                    .map((r) => `${r.label || r.nodeId}: ${r.error}`)
                    .slice(0, 6);
                void alert({
                    title: `${action === 'stop' ? 'Stop' : action === 'start' ? 'Start' : 'Redeploy'} finished with errors`,
                    message: `${result.succeeded} succeeded, ${result.failed} failed of ${result.total}.`,
                    detail: lines.join('\n') || undefined,
                });
            }
        } catch (e) {
            void alert({
                title: 'Stack action failed',
                message: String(e),
            });
        } finally {
            setStackBusy(false);
        }
    };

    const setMode = (nodeId: string, mode: DataMode) => {
        setChoices((prev) => ({
            ...prev,
            [nodeId]: {...(prev[nodeId] ?? {mode: 'fresh', consistency: 'consistent'}), mode},
        }));
    };

    const setConsistency = (nodeId: string, consistency: 'consistent' | 'quick') => {
        setChoices((prev) => ({
            ...prev,
            [nodeId]: {...(prev[nodeId] ?? {mode: 'clone', consistency: 'consistent'}), consistency},
        }));
    };

    const selectedEnv = environments.find((e) => e.id === selectedEnvironmentId) ?? null;
    const selectedSandbox = selectedEnv
        ? sandboxes.find((sandbox) => sandbox.environmentId === selectedEnv.id) ?? null
        : null;
    const sandboxByEnvironmentId = new Map(sandboxes.map((sandbox) => [sandbox.environmentId, sandbox]));
    const sandboxSourceEnv = selectedSandbox
        ? environments.find((env) => env.id === selectedSandbox.sourceEnvironmentId) ?? null
        : null;

    return (
        <>
            <div className="environment-switcher">
                <div className="environment-selector">
                    <select
                        className="input settings-select environment-select"
                        value={selectedEnvironmentId ?? ''}
                        onChange={(e) => onSelect(Number(e.target.value))}
                        aria-label="Selected environment"
                    >
                        {environments.map((env) => {
                            const sandbox = sandboxByEnvironmentId.get(env.id);
                            const suffix = sandbox
                                ? ' (sandbox)'
                                : env.isDefault
                                    ? ' (default)'
                                    : '';
                            return <option key={env.id} value={env.id}>{env.name}{suffix}</option>;
                        })}
                    </select>
                    {selectedEnv && (
                        <div className="environment-selector-menu-wrap" ref={menuRef}>
                            <button type="button" className="icon-button" title="Environment actions" onClick={() => setMenuEnvId(menuEnvId === selectedEnv.id ? null : selectedEnv.id)}>
                                <MoreHorizontal size={15}/>
                            </button>
                            {menuEnvId === selectedEnv.id && (
                                <div className="environment-switcher-menu">
                                    <button type="button" onClick={() => openRename(selectedEnv)}>Rename…</button>
                                    {!selectedEnv.isDefault && !selectedSandbox && <button type="button" onClick={() => void setAsDefault(selectedEnv)}>Set as default</button>}
                                    <button type="button" onClick={() => { setMenuEnvId(null); setSyncOpen(true); }}>Sync config…</button>
                                    {!selectedSandbox && (
                                        <button type="button" onClick={() => { setMenuEnvId(null); setExportPackOpen(true); }}>
                                            Export Draft pack…
                                        </button>
                                    )}
                                    {!selectedEnv.isDefault && <button type="button" className="environment-switcher-menu-danger" onClick={() => void removeEnvironment(selectedEnv)}>Delete…</button>}
                                </div>
                            )}
                        </div>
                    )}
                    <button
                        className="environment-switcher-action"
                        onClick={openNewDialog}
                        title="New environment"
                    >
                        <Plus size={13}/> New
                    </button>
                    {selectedEnv && !selectedSandbox && (
                        <button className="btn btn-ghost environment-sandbox-action" onClick={() => onCreateSandbox?.(selectedEnv.id)}>
                            <Plus size={13}/> Sandbox
                        </button>
                    )}
                </div>

                {selectedEnv && (
                    <div className="environment-stack-toolbar">
                        <button
                            type="button"
                            className="btn btn-ghost environment-stack-btn"
                            onClick={() => setSyncOpen(true)}
                            title="Sync settings and env vars from another environment"
                        >
                            <GitCompare size={13}/> Sync
                        </button>
                        {!selectedSandbox && (
                            <button
                                type="button"
                                className="btn btn-ghost environment-stack-btn"
                                onClick={() => setExportPackOpen(true)}
                                title="Export this environment as a portable Draft pack"
                            >
                                <Package size={13}/> Pack
                            </button>
                        )}
                        <button
                            type="button"
                            className="btn btn-ghost environment-stack-btn"
                            disabled={stackBusy}
                            onClick={() => void runStack('start')}
                            title="Start all services in this environment"
                        >
                            <Play size={13}/> Start all
                        </button>
                        <button
                            type="button"
                            className="btn btn-ghost environment-stack-btn"
                            disabled={stackBusy}
                            onClick={() => void runStack('redeploy')}
                            title="Redeploy all services in this environment"
                        >
                            <RefreshCw size={13}/> Redeploy all
                        </button>
                        <button
                            type="button"
                            className="btn btn-ghost environment-stack-btn"
                            disabled={stackBusy}
                            onClick={() => void runStack('stop')}
                            title="Stop all services in this environment"
                        >
                            <Power size={13}/> Stop all
                        </button>
                        {stackBusy && <span className="environment-stack-busy">Working…</span>}
                    </div>
                )}
            </div>
            {selectedSandbox && (
                <div
                    className={`environment-sandbox-banner environment-sandbox-banner--${selectedSandbox.status || 'active'}`}
                    role="status"
                >
                    <div className="environment-sandbox-banner-main">
                        <FlaskConical size={14}/>
                        <strong>Sandbox</strong>
                        <span className="environment-sandbox-status">{selectedSandbox.status}</span>
                        {sandboxSourceEnv && (
                            <span className="environment-sandbox-source">
                                from {sandboxSourceEnv.name}
                            </span>
                        )}
                        <span className="environment-sandbox-expiry" title={sandboxExpiryLabel(selectedSandbox.expiresAt)} data-tick={nowTick}>
                            <Clock3 size={12}/>
                            {selectedSandbox.status === 'expired' || selectedSandbox.status === 'cleanup_failed'
                                ? sandboxRelativeLabel(selectedSandbox.graceEndsAt, 'Deletes')
                                : `${sandboxRelativeLabel(selectedSandbox.expiresAt, 'Expires')} · ${sandboxRelativeLabel(selectedSandbox.graceEndsAt, 'deletes')}`}
                        </span>
                    </div>
                    <div className="environment-sandbox-banner-actions">
                        {sandboxSourceEnv && (
                            <button
                                type="button"
                                className="btn btn-ghost"
                                onClick={() => onSelect(sandboxSourceEnv.id)}
                            >
                                Open source
                            </button>
                        )}
                        {(selectedSandbox.status === 'active' || selectedSandbox.status === 'warning' || selectedSandbox.status === 'suspended') && (
                            <>
                                <button
                                    type="button"
                                    className="btn btn-ghost"
                                    disabled={sandboxBusy}
                                    title="Redeploy at current branch/PR tip"
                                    onClick={() => void refreshSelectedSandbox('tip')}
                                >
                                    <RefreshCw size={13}/>
                                    Tip
                                </button>
                                <button
                                    type="button"
                                    className="btn btn-ghost"
                                    disabled={sandboxBusy}
                                    title="Rebuild at the recorded commit SHA"
                                    onClick={() => void refreshSelectedSandbox('same')}
                                >
                                    Same SHA
                                </button>
                                <button
                                    type="button"
                                    className="btn btn-ghost"
                                    disabled={sandboxBusy}
                                    onClick={() => void toggleSandboxSuspend()}
                                >
                                    {selectedSandbox.status === 'suspended' ? 'Resume' : 'Suspend'}
                                </button>
                            </>
                        )}
                        <button
                            type="button"
                            className="btn btn-ghost environment-sandbox-delete"
                            onClick={() => void removeSandbox(selectedSandbox)}
                            title="Delete sandbox"
                            aria-label="Delete sandbox"
                        >
                            <Trash2 size={13}/>
                        </button>
                        <SandboxExtendControl
                            compact
                            sandboxId={selectedSandbox.id}
                            currentExpiresAt={selectedSandbox.expiresAt}
                            onExtended={refresh}
                            onError={(message) => void alert({title: 'Could not extend sandbox', message})}
                        />
                    </div>
                </div>
            )}

            {dialogOpen && (
                <Dialog
                    title={step === 1 ? 'New environment' : 'Service data & source'}
                    onClose={closeDialog}
                    footer={
                        <>
                            <button className="btn btn-ghost" onClick={closeDialog}>Cancel</button>
                            {step === 2 && (
                                <button className="btn btn-ghost" onClick={() => setStep(1)} disabled={submitting}>
                                    Back
                                </button>
                            )}
                            <button
                                className="btn btn-primary"
                                disabled={name.trim() === '' || submitting}
                                onClick={() => {
                                    if (step === 1 && sourceId !== SOURCE_BLANK) goNext();
                                    else submit();
                                }}
                            >
                                {submitting
                                    ? 'Working…'
                                    : step === 1 && sourceId !== SOURCE_BLANK
                                        ? 'Next'
                                        : sourceId !== SOURCE_BLANK
                                            ? (startAfter ? 'Duplicate & start' : 'Duplicate')
                                            : 'Create'}
                            </button>
                        </>
                    }
                >
                    {error && <p className="environment-error">{error}</p>}
                    {step === 1 && (
                        <>
                            <div className="form-field">
                                <label className="form-label">Name</label>
                                <input
                                    className="input"
                                    value={name}
                                    onChange={(e) => setName(e.target.value)}
                                    placeholder="staging"
                                    autoFocus
                                    onKeyDown={(e) => {
                                        if (e.key === 'Enter') {
                                            if (sourceId !== SOURCE_BLANK) goNext();
                                            else submit();
                                        }
                                    }}
                                />
                            </div>
                            <div className="form-field">
                                <label className="form-label">Based on</label>
                                <select
                                    className="input settings-select environment-source-select"
                                    value={sourceId}
                                    onChange={(e) => setSourceId(e.target.value)}
                                >
                                    <option value={SOURCE_BLANK}>Empty environment</option>
                                    {environments.map((env) => (
                                        <option key={env.id} value={String(env.id)}>
                                            {env.name}
                                        </option>
                                    ))}
                                </select>
                                <p className="environment-source-hint">
                                    {sourceId === SOURCE_BLANK
                                        ? 'Start with no services. You can add them after creating.'
                                        : 'Clone services and settings. Next: choose Fresh / Share / Clone per service, and optionally pin copied services to a branch or PR.'}
                                </p>
                            </div>
                        </>
                    )}

                    {step === 2 && (
                        <div className="environment-data-step">
                            <p className="environment-data-lead">
                                <strong>Fresh</strong> creates an independent copy.
                                {' '}<strong>Share</strong> attaches the source service into this environment
                                (prefer this over public hostnames).
                                {' '}<strong>Clone</strong> copies volume data once when mounts exist.
                            </p>
                            <label className="environment-start-after">
                                <input
                                    type="checkbox"
                                    checked={startAfter}
                                    onChange={(e) => setStartAfter(e.target.checked)}
                                    disabled={submitting}
                                />
                                <span>
                                    <strong>Start services after create</strong>
                                    <span className="environment-start-after-hint">
                                        Deploys every service in the new environment. Leave off if you only need the copy for now.
                                    </span>
                                </span>
                            </label>
                            {stateful.length === 0 ? (
                                <p className="environment-data-empty">No services in the source environment.</p>
                            ) : (
                                <ul className="environment-data-list">
                                    {stateful.map((svc) => {
                                        const c = choices[svc.nodeId] ?? {mode: 'fresh' as DataMode, consistency: 'consistent' as const};
                                        const hasVolumes = (svc.volumes?.length ?? 0) > 0;
                                        const modes: DataMode[] = hasVolumes
                                            ? ['fresh', 'share', 'clone']
                                            : ['fresh', 'share'];
                                        const modeLabel = (mode: DataMode) =>
                                            mode === 'fresh' ? 'Fresh' : mode === 'share' ? 'Share' : 'Clone';
                                        return (
                                            <li
                                                key={svc.nodeId}
                                                className={`environment-data-row environment-data-row--${c.mode}`}
                                            >
                                                <div className="environment-data-row-head">
                                                    <div className="environment-data-identity">
                                                        <strong className="environment-data-label">{svc.label}</strong>
                                                        {hasVolumes ? (
                                                            <span className="environment-data-meta" title={svc.volumes.join(', ')}>
                                                                {svc.volumes.join(' · ')}
                                                            </span>
                                                        ) : (
                                                            <span className="environment-data-meta environment-data-meta--muted">
                                                                no volumes
                                                            </span>
                                                        )}
                                                    </div>
                                                    <div
                                                        className="environment-data-segment"
                                                        role="radiogroup"
                                                        aria-label={`${svc.label} data mode`}
                                                    >
                                                        {modes.map((mode) => (
                                                            <label
                                                                key={mode}
                                                                className={`environment-data-segment-option${c.mode === mode ? ' is-active' : ''}`}
                                                            >
                                                                <input
                                                                    type="radio"
                                                                    name={`mode-${svc.nodeId}`}
                                                                    checked={c.mode === mode}
                                                                    onChange={() => setMode(svc.nodeId, mode)}
                                                                />
                                                                {modeLabel(mode)}
                                                            </label>
                                                        ))}
                                                    </div>
                                                </div>
                                                {c.mode === 'share' && svc.warning && (
                                                    <p className="environment-data-warning">{svc.warning}</p>
                                                )}
                                                {c.mode === 'clone' && hasVolumes && (
                                                    <div className="environment-data-subrow">
                                                        <span className="environment-data-sublabel">Consistency</span>
                                                        <div
                                                            className="environment-data-segment environment-data-segment--compact"
                                                            role="radiogroup"
                                                            aria-label={`${svc.label} clone consistency`}
                                                        >
                                                            <label
                                                                className={`environment-data-segment-option${c.consistency === 'consistent' ? ' is-active' : ''}`}
                                                            >
                                                                <input
                                                                    type="radio"
                                                                    name={`cons-${svc.nodeId}`}
                                                                    checked={c.consistency === 'consistent'}
                                                                    onChange={() => setConsistency(svc.nodeId, 'consistent')}
                                                                />
                                                                Consistent
                                                            </label>
                                                            <label
                                                                className={`environment-data-segment-option${c.consistency === 'quick' ? ' is-active' : ''}`}
                                                            >
                                                                <input
                                                                    type="radio"
                                                                    name={`cons-${svc.nodeId}`}
                                                                    checked={c.consistency === 'quick'}
                                                                    onChange={() => setConsistency(svc.nodeId, 'quick')}
                                                                />
                                                                Quick
                                                            </label>
                                                        </div>
                                                    </div>
                                                )}
                                            </li>
                                        );
                                    })}
                                </ul>
                            )}

                            <section className="environment-source-section">
                                <h3 className="environment-source-section-title">Source code</h3>
                                <p className="environment-source-hint">
                                    Optionally pin <strong>Fresh</strong> / <strong>Clone</strong> copies to a branch, ref, or PR.
                                    Shared services keep the source environment&apos;s code. Leave on keep to copy existing pins.
                                    Change later in each service&apos;s Settings.
                                </p>
                                {sourceReposLoading ? (
                                    <div className="environment-source-skeleton">
                                        {Array.from({length: 1}, (_, i) => (
                                            <div key={i} className="environment-source-repo">
                                                <div className="environment-source-repo-head">
                                                    <GitBranch size={14}/>
                                                    <Skeleton width="28%" height={13}/>
                                                    <Skeleton width="42%" height={10}/>
                                                </div>
                                                <Skeleton width="48%" height={10}/>
                                                <Skeleton width="78%" height={28} style={{marginTop: 4}}/>
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
                                            <div className="environment-source-repo" key={repo.repoRoot}>
                                                <div className="environment-source-repo-head">
                                                    <GitBranch size={14}/>
                                                    <strong title={repo.repoRoot}>{repoLeaf(repo.repoRoot)}</strong>
                                                    <span className="environment-source-repo-path" title={repo.repoRoot}>{repo.repoRoot}</span>
                                                </div>
                                                <p className="environment-source-hint">
                                                    Services: {(repo.serviceLabels ?? []).join(', ') || '—'}
                                                    {repo.defaultRef ? ` · source pin ${repo.defaultRef}` : ''}
                                                </p>
                                                <div className="environment-source-modes">
                                                    <label className="environment-source-mode">
                                                        <input
                                                            type="radio"
                                                            name={`env-src-mode-${repo.repoRoot}`}
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
                                                    <label className="environment-source-mode">
                                                        <input
                                                            type="radio"
                                                            name={`env-src-mode-${repo.repoRoot}`}
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
                                                        <label className="environment-source-mode">
                                                            <input
                                                                type="radio"
                                                                name={`env-src-mode-${repo.repoRoot}`}
                                                                checked={draft.mode === 'pr'}
                                                                onChange={() => updateRepoDraft(repo.repoRoot, {mode: 'pr'})}
                                                            />
                                                            Pull request
                                                        </label>
                                                    ) : (
                                                        <span
                                                            className="environment-source-mode environment-source-mode--disabled"
                                                            title={repo.pullRequestsError || 'GitHub CLI unavailable'}
                                                        >
                                                            PRs unavailable
                                                        </span>
                                                    )}
                                                </div>
                                                {draft.mode === 'branch' && (
                                                    <div className="environment-source-controls">
                                                        <select
                                                            className="input settings-select"
                                                            value={branches.includes(draft.ref) ? draft.ref : ''}
                                                            onChange={(e) => updateRepoDraft(repo.repoRoot, {ref: e.target.value, commitSha: ''})}
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
                                                            placeholder="branch, tag, or SHA"
                                                        />
                                                    </div>
                                                )}
                                                {draft.mode === 'pr' && repo.pullRequestsAvailable && (
                                                    <div className="environment-source-controls">
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
                                                            <p className="environment-source-hint" style={{margin: 0}}>
                                                                Head {draft.ref || '—'}
                                                                {draft.commitSha ? ` · ${draft.commitSha.slice(0, 12)}` : ''}
                                                                {draft.prTitle ? ` · ${draft.prTitle}` : ''}
                                                            </p>
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
                        </div>
                    )}
                </Dialog>
            )}

            {syncOpen && selectedEnvironmentId != null && (
                <SyncConfigDialog
                    projectId={projectId}
                    targetEnvironmentId={selectedEnvironmentId}
                    onClose={() => setSyncOpen(false)}
                    onApplied={() => {
                        onServicesChanged?.();
                        onStackActionDone?.();
                    }}
                />
            )}

            {exportPackOpen && selectedEnvironmentId != null && selectedEnv && (
                <ExportDraftPackDialog
                    scope="environment"
                    label={selectedEnv.name}
                    environmentId={selectedEnvironmentId}
                    projectId={projectId}
                    onClose={() => setExportPackOpen(false)}
                />
            )}

            {renameEnv && (
                <Dialog
                    title="Rename environment"
                    onClose={() => setRenameEnv(null)}
                    footer={
                        <>
                            <button className="btn btn-ghost" onClick={() => setRenameEnv(null)}>Cancel</button>
                            <button
                                className="btn btn-primary"
                                disabled={renameValue.trim() === '' || submitting}
                                onClick={submitRename}
                            >
                                {submitting ? 'Saving…' : 'Save'}
                            </button>
                        </>
                    }
                >
                    {error && <p className="environment-error">{error}</p>}
                    <div className="form-field">
                        <label className="form-label">Display name</label>
                        <input
                            className="input"
                            value={renameValue}
                            onChange={(e) => setRenameValue(e.target.value)}
                            autoFocus
                            onKeyDown={(e) => {
                                if (e.key === 'Enter') submitRename();
                            }}
                        />
                        <p className="environment-source-hint">
                            Hostnames and Docker networks keep the original slug ({renameEnv.slug}); only the label changes.
                        </p>
                    </div>
                </Dialog>
            )}
        </>
    );
}
