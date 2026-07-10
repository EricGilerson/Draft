import {useEffect, useRef, useState} from 'react';
import {MoreHorizontal, Play, Plus, Power, RefreshCw} from 'lucide-react';
import {
    CreateEnvironment,
    DeleteEnvironment,
    DuplicateEnvironment,
    ListEnvironments,
    PreviewEnvironmentDuplicate,
    RedeployEnvironment,
    RenameEnvironment,
    SetDefaultEnvironment,
    StartEnvironment,
    StopEnvironment,
} from '../../wailsjs/go/main/App';
import {deploy, store} from '../../wailsjs/go/models';
import {useAppDialog} from './AppDialogProvider';
import Dialog from './Dialog';
import './EnvironmentSwitcher.css';

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
};

export default function EnvironmentSwitcher({
    projectId,
    selectedEnvironmentId,
    onSelect,
    onDuplicating,
    onEnvironmentsChanged,
    onStackActionDone,
}: EnvironmentSwitcherProps) {
    const [environments, setEnvironments] = useState<store.Environment[]>([]);
    const [dialogOpen, setDialogOpen] = useState(false);
    const [step, setStep] = useState<1 | 2>(1);
    const [name, setName] = useState('');
    /** Empty string = blank env; otherwise the source environment id. */
    const [sourceId, setSourceId] = useState(SOURCE_BLANK);
    const [stateful, setStateful] = useState<deploy.StatefulServiceSummary[]>([]);
    const [choices, setChoices] = useState<Record<string, ChoiceState>>({});
    const [submitting, setSubmitting] = useState(false);
    const [error, setError] = useState('');
    const [menuEnvId, setMenuEnvId] = useState<number | null>(null);
    const [renameEnv, setRenameEnv] = useState<store.Environment | null>(null);
    const [renameValue, setRenameValue] = useState('');
    const [stackBusy, setStackBusy] = useState(false);
    const menuRef = useRef<HTMLDivElement | null>(null);
    const {confirm, alert} = useAppDialog();

    const refresh = () => {
        ListEnvironments(projectId).then((envs) => setEnvironments(envs ?? [])).catch(() => setEnvironments([]));
    };

    useEffect(() => {
        refresh();
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [projectId]);

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

    const openNewDialog = () => {
        setName('');
        setError('');
        setSubmitting(false);
        setStep(1);
        setStateful([]);
        setChoices({});
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
    };

    const loadStateful = (envId: number) => {
        PreviewEnvironmentDuplicate(envId)
            .then((list) => {
                const rows = list ?? [];
                setStateful(rows);
                const next: Record<string, ChoiceState> = {};
                for (const row of rows) {
                    next[row.nodeId] = {mode: 'fresh', consistency: 'consistent'};
                }
                setChoices(next);
            })
            .catch(() => {
                setStateful([]);
                setChoices({});
            });
    };

    const goNext = () => {
        if (name.trim() === '') return;
        if (sourceId === SOURCE_BLANK) {
            submit();
            return;
        }
        loadStateful(Number(sourceId));
        setStep(2);
    };

    const submit = () => {
        if (name.trim() === '' || submitting) return;
        setSubmitting(true);
        setError('');

        const isDuplicate = sourceId !== SOURCE_BLANK;
        const dataChoices: deploy.ServiceDataChoice[] = isDuplicate
            ? Object.entries(choices).map(([sourceNodeId, c]) =>
                deploy.ServiceDataChoice.createFrom({
                    sourceNodeId,
                    mode: c.mode,
                    consistency: c.mode === 'clone' ? c.consistency : undefined,
                }),
            )
            : [];

        const request = isDuplicate
            ? DuplicateEnvironment(Number(sourceId), name.trim(), dataChoices)
            : CreateEnvironment(projectId, name.trim());

        if (isDuplicate) onDuplicating?.(true);
        request
            .then((env) => {
                closeDialog();
                onDuplicating?.(false);
                refresh();
                onEnvironmentsChanged?.();
                onSelect(env.id);
            })
            .catch((e) => {
                setError(String(e));
                setSubmitting(false);
                onDuplicating?.(false);
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

    return (
        <>
            <div className="environment-switcher">
                <nav className="environment-switcher-tabs">
                    {environments.map((env) => (
                        <div key={env.id} className="environment-switcher-tab-wrap">
                            <button
                                className={`environment-switcher-tab ${env.id === selectedEnvironmentId ? 'environment-switcher-tab--active' : ''}`}
                                onClick={() => onSelect(env.id)}
                            >
                                <span className="environment-switcher-tab-name">{env.name}</span>
                                {env.isDefault && (
                                    <span className="environment-switcher-default-badge" title="Default environment">
                                        default
                                    </span>
                                )}
                            </button>
                            <button
                                type="button"
                                className="environment-switcher-tab-menu-btn"
                                title="Environment actions"
                                onClick={(e) => {
                                    e.stopPropagation();
                                    setMenuEnvId(menuEnvId === env.id ? null : env.id);
                                }}
                            >
                                <MoreHorizontal size={13}/>
                            </button>
                            {menuEnvId === env.id && (
                                <div className="environment-switcher-menu" ref={menuRef}>
                                    <button type="button" onClick={() => openRename(env)}>Rename…</button>
                                    {!env.isDefault && (
                                        <button type="button" onClick={() => void setAsDefault(env)}>
                                            Set as default
                                        </button>
                                    )}
                                    {env.id === selectedEnvironmentId && (
                                        <>
                                            <button type="button" disabled={stackBusy} onClick={() => void runStack('start')}>
                                                Start all
                                            </button>
                                            <button type="button" disabled={stackBusy} onClick={() => void runStack('redeploy')}>
                                                Redeploy all
                                            </button>
                                            <button type="button" disabled={stackBusy} onClick={() => void runStack('stop')}>
                                                Stop all
                                            </button>
                                        </>
                                    )}
                                    {!env.isDefault && (
                                        <button
                                            type="button"
                                            className="environment-switcher-menu-danger"
                                            onClick={() => void removeEnvironment(env)}
                                        >
                                            Delete…
                                        </button>
                                    )}
                                </div>
                            )}
                        </div>
                    ))}
                    <button
                        className="environment-switcher-action"
                        onClick={openNewDialog}
                        title="New environment"
                    >
                        <Plus size={13}/> New
                    </button>
                </nav>

                {selectedEnv && (
                    <div className="environment-stack-toolbar">
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

            {dialogOpen && (
                <Dialog
                    title={step === 1 ? 'New environment' : 'Service data'}
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
                                            ? 'Duplicate'
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
                                        : 'Clone services and settings. On the next step you choose how each database or volume-backed service gets its data.'}
                                </p>
                            </div>
                        </>
                    )}

                    {step === 2 && (
                        <div className="environment-data-step">
                            <p className="environment-source-hint">
                                Default is a <strong>fresh</strong> empty volume for each service. Share uses the
                                source environment&apos;s running service (no second container). Clone copies volume
                                data once into new volumes.
                            </p>
                            {stateful.length === 0 ? (
                                <p className="settings-hint">No volume-backed services in the source environment.</p>
                            ) : (
                                <ul className="environment-data-list">
                                    {stateful.map((svc) => {
                                        const c = choices[svc.nodeId] ?? {mode: 'fresh' as DataMode, consistency: 'consistent' as const};
                                        return (
                                            <li key={svc.nodeId} className="environment-data-row">
                                                <div className="environment-data-row-head">
                                                    <strong>{svc.label}</strong>
                                                    <span className="settings-hint">
                                                        {svc.volumes?.join(', ')}
                                                    </span>
                                                </div>
                                                <div className="environment-data-modes">
                                                    {(['fresh', 'share', 'clone'] as DataMode[]).map((mode) => (
                                                        <label key={mode} className="environment-data-mode">
                                                            <input
                                                                type="radio"
                                                                name={`mode-${svc.nodeId}`}
                                                                checked={c.mode === mode}
                                                                onChange={() => setMode(svc.nodeId, mode)}
                                                            />
                                                            {mode === 'fresh' ? 'Fresh' : mode === 'share' ? 'Share service' : 'Clone data'}
                                                        </label>
                                                    ))}
                                                </div>
                                                {c.mode === 'share' && svc.warning && (
                                                    <p className="environment-data-warning">{svc.warning}</p>
                                                )}
                                                {c.mode === 'clone' && (
                                                    <div className="environment-data-modes">
                                                        <label className="environment-data-mode">
                                                            <input
                                                                type="radio"
                                                                name={`cons-${svc.nodeId}`}
                                                                checked={c.consistency === 'consistent'}
                                                                onChange={() => setConsistency(svc.nodeId, 'consistent')}
                                                            />
                                                            Consistent
                                                        </label>
                                                        <label className="environment-data-mode">
                                                            <input
                                                                type="radio"
                                                                name={`cons-${svc.nodeId}`}
                                                                checked={c.consistency === 'quick'}
                                                                onChange={() => setConsistency(svc.nodeId, 'quick')}
                                                            />
                                                            Quick
                                                        </label>
                                                    </div>
                                                )}
                                            </li>
                                        );
                                    })}
                                </ul>
                            )}
                        </div>
                    )}
                </Dialog>
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
