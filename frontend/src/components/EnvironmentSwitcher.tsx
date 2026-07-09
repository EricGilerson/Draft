import {useEffect, useState} from 'react';
import {Plus} from 'lucide-react';
import {
    CreateEnvironment,
    DeleteEnvironment,
    DuplicateEnvironment,
    ListEnvironments,
    PreviewEnvironmentDuplicate,
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
};

export default function EnvironmentSwitcher({
    projectId,
    selectedEnvironmentId,
    onSelect,
    onDuplicating,
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
    const {confirm} = useAppDialog();

    const refresh = () => {
        ListEnvironments(projectId).then((envs) => setEnvironments(envs ?? [])).catch(() => setEnvironments([]));
    };

    useEffect(() => {
        refresh();
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [projectId]);

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
        DeleteEnvironment(env.id).then(() => {
            refresh();
            if (selectedEnvironmentId === env.id) {
                const fallback = environments.find((e) => e.isDefault);
                if (fallback) onSelect(fallback.id);
            }
        }).catch((e) => {
            void confirm({
                title: 'Could not delete environment',
                message: String(e),
                confirmLabel: 'OK',
            });
        });
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

    return (
        <>
            <nav className="environment-switcher-tabs">
                {environments.map((env) => (
                    <button
                        key={env.id}
                        className={`environment-switcher-tab ${env.id === selectedEnvironmentId ? 'environment-switcher-tab--active' : ''}`}
                        onClick={() => onSelect(env.id)}
                    >
                        {env.name}
                        {!env.isDefault && (
                            <span
                                className="environment-switcher-tab-remove"
                                onClick={(e) => {
                                    e.stopPropagation();
                                    void removeEnvironment(env);
                                }}
                            >
                                ×
                            </span>
                        )}
                    </button>
                ))}
                <button
                    className="environment-switcher-action"
                    onClick={openNewDialog}
                    title="New environment"
                >
                    <Plus size={13}/> New
                </button>
            </nav>

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
                                                            Consistent (stop source)
                                                        </label>
                                                        <label className="environment-data-mode">
                                                            <input
                                                                type="radio"
                                                                name={`cons-${svc.nodeId}`}
                                                                checked={c.consistency === 'quick'}
                                                                onChange={() => setConsistency(svc.nodeId, 'quick')}
                                                            />
                                                            Quick (source may keep running)
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
                    {error && <p className="form-error">{error}</p>}
                </Dialog>
            )}
        </>
    );
}
