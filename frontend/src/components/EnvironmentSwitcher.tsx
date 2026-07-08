import {useEffect, useState} from 'react';
import {Plus} from 'lucide-react';
import {CreateEnvironment, DeleteEnvironment, DuplicateEnvironment, ListEnvironments} from '../../wailsjs/go/main/App';
import {store} from '../../wailsjs/go/models';
import {useAppDialog} from './AppDialogProvider';
import Dialog from './Dialog';
import './EnvironmentSwitcher.css';

/** Sentinel for "start empty" in the source dropdown. */
const SOURCE_BLANK = '';

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
    const [name, setName] = useState('');
    /** Empty string = blank env; otherwise the source environment id. */
    const [sourceId, setSourceId] = useState(SOURCE_BLANK);
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
        // Prefill source with the environment the user is currently viewing.
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
    };

    const submit = () => {
        if (name.trim() === '' || submitting) return;
        setSubmitting(true);
        setError('');

        const isDuplicate = sourceId !== SOURCE_BLANK;
        const request = isDuplicate
            ? DuplicateEnvironment(Number(sourceId), name.trim())
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
            detail: 'This stops and removes its services.',
            confirmLabel: 'Delete',
            danger: true,
        })) return;
        DeleteEnvironment(env.id).then(() => {
            refresh();
            if (selectedEnvironmentId === env.id) {
                const fallback = environments.find((e) => e.isDefault);
                if (fallback) onSelect(fallback.id);
            }
        });
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
                    title="New environment"
                    onClose={closeDialog}
                    footer={
                        <>
                            <button className="btn btn-ghost" onClick={closeDialog}>Cancel</button>
                            <button
                                className="btn btn-primary"
                                disabled={name.trim() === '' || submitting}
                                onClick={submit}
                            >
                                {submitting
                                    ? 'Working…'
                                    : sourceId !== SOURCE_BLANK
                                        ? 'Duplicate'
                                        : 'Create'}
                            </button>
                        </>
                    }
                >
                    <div className="form-field">
                        <label className="form-label">Name</label>
                        <input
                            className="input"
                            value={name}
                            onChange={(e) => setName(e.target.value)}
                            placeholder="staging"
                            autoFocus
                            onKeyDown={(e) => {
                                if (e.key === 'Enter') submit();
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
                                : 'Clone services, settings, and env vars from the selected environment. Deployments and volumes start fresh.'}
                        </p>
                    </div>
                    {error && <p className="form-error">{error}</p>}
                </Dialog>
            )}
        </>
    );
}
