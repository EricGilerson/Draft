import {useEffect, useState} from 'react';
import {Plus, Copy} from 'lucide-react';
import {CreateEnvironment, DeleteEnvironment, DuplicateEnvironment, ListEnvironments} from '../../wailsjs/go/main/App';
import {store} from '../../wailsjs/go/models';
import Dialog from './Dialog';
import './EnvironmentSwitcher.css';

type EnvironmentSwitcherProps = {
    projectId: number;
    selectedEnvironmentId: number | null;
    onSelect: (environmentId: number) => void;
    onDuplicating?: (duplicating: boolean) => void;
};

type PromptMode = {kind: 'new'} | {kind: 'duplicate'; sourceId: number; sourceName: string};

export default function EnvironmentSwitcher({projectId, selectedEnvironmentId, onSelect, onDuplicating}: EnvironmentSwitcherProps) {
    const [environments, setEnvironments] = useState<store.Environment[]>([]);
    const [prompt, setPrompt] = useState<PromptMode | null>(null);
    const [name, setName] = useState('');
    const [submitting, setSubmitting] = useState(false);
    const [error, setError] = useState('');

    const refresh = () => {
        ListEnvironments(projectId).then((envs) => setEnvironments(envs ?? [])).catch(() => setEnvironments([]));
    };

    useEffect(() => {
        refresh();
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [projectId]);

    const closePrompt = () => {
        setPrompt(null);
        setName('');
        setError('');
        setSubmitting(false);
    };

    const submitPrompt = () => {
        if (!prompt || name.trim() === '' || submitting) return;
        setSubmitting(true);
        setError('');
        const request =
            prompt.kind === 'new'
                ? CreateEnvironment(projectId, name.trim())
                : DuplicateEnvironment(prompt.sourceId, name.trim());
        if (prompt.kind === 'duplicate') onDuplicating?.(true);
        request
            .then((env) => {
                closePrompt();
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

    const removeEnvironment = (env: store.Environment) => {
        if (env.isDefault) return;
        if (!window.confirm(`Delete environment "${env.name}"? This stops and removes its services.`)) return;
        DeleteEnvironment(env.id).then(() => {
            refresh();
            if (selectedEnvironmentId === env.id) {
                const fallback = environments.find((e) => e.isDefault);
                if (fallback) onSelect(fallback.id);
            }
        });
    };

    const selected = environments.find((e) => e.id === selectedEnvironmentId);

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
                                    removeEnvironment(env);
                                }}
                            >
                                ×
                            </span>
                        )}
                    </button>
                ))}
                <button
                    className="environment-switcher-action"
                    onClick={() => setPrompt({kind: 'new'})}
                    title="New environment"
                >
                    <Plus size={13}/> New
                </button>
                {selected && (
                    <button
                        className="environment-switcher-action"
                        onClick={() => setPrompt({kind: 'duplicate', sourceId: selected.id, sourceName: selected.name})}
                        title="Duplicate environment"
                    >
                        <Copy size={13}/> Duplicate
                    </button>
                )}
            </nav>

            {prompt && (
                <Dialog
                    title={prompt.kind === 'new' ? 'New environment' : `Duplicate "${prompt.sourceName}"`}
                    onClose={closePrompt}
                    footer={
                        <>
                            <button className="btn btn-ghost" onClick={closePrompt}>Cancel</button>
                            <button
                                className="btn btn-primary"
                                disabled={name.trim() === '' || submitting}
                                onClick={submitPrompt}
                            >
                                {submitting ? 'Working…' : prompt.kind === 'new' ? 'Create' : 'Duplicate'}
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
                                if (e.key === 'Enter') submitPrompt();
                            }}
                        />
                    </div>
                    {error && <p className="form-error">{error}</p>}
                </Dialog>
            )}
        </>
    );
}
