import {Copy, Plus, Trash2} from 'lucide-react';
import {useEffect, useMemo, useState} from 'react';
import {
    CreateServiceTemplate, UpdateServiceTemplate, CloneServiceTemplate,
} from '../../wailsjs/go/main/App';
import {store} from '../../wailsjs/go/models';
import Dialog from './Dialog';
import TemplateIcon, {TEMPLATE_ICON_OPTIONS} from './TemplateIcon';
import './TemplateEditorDialog.css';

type Mode = 'create' | 'edit' | 'view';

type Props = {
    mode: Mode;
    template?: store.ServiceTemplate;
    onClose: () => void;
    onSaved: () => void;
};

type EnvEntry = {key: string; value: string; scope: string};
type LabelEntry = {key: string; value: string};

const CATEGORIES: {value: string; label: string}[] = [
    {value: 'web', label: 'Web'},
    {value: 'datastore', label: 'Datastore'},
    {value: 'language', label: 'Language'},
];

const SCOPES: {value: string; label: string}[] = [
    {value: 'runtime', label: 'runtime'},
    {value: 'build', label: 'build'},
    {value: 'both', label: 'both'},
];

// DRAFT_TOKENS is the catalog of {{draft.X}} placeholder expressions template
// authors can drop into env-var values (and cmd/entrypoint). Draft expands them
// at node-creation time and in the live preview path to identity-derived
// values, so two DBs in different projects/environments never collide. Keep
// this in sync with internal/deploy/template_expr.go resolveExprToken.
const DRAFT_TOKENS: {token: string; hint: string}[] = [
    {token: '{{draft.db_name}}', hint: 'Unique DB name (proj_env_svc)'},
    {token: '{{draft.db_user}}', hint: 'Unique DB user (proj_env_svc)'},
    {token: '{{draft.password}}', hint: 'Per-node derived password'},
    {token: '{{draft.uuid}}', hint: 'Random one-time uuid'},
    {token: '{{draft.internal_hostname}}', hint: 'Docker-network hostname'},
    {token: '{{draft.internal_url}}', hint: 'Internal http URL'},
    {token: '{{draft.public_hostname}}', hint: 'Host-side hostname'},
    {token: '{{draft.public_url}}', hint: 'Public http URL'},
    {token: '{{draft.service_port}}', hint: 'Configured container port'},
    {token: '{{draft.service}}', hint: 'Sanitized service label'},
    {token: '{{draft.project}}', hint: 'Sanitized project name'},
    {token: '{{draft.environment}}', hint: 'Environment name'},
    {token: '{{draft.uid}}', hint: 'Node permanent UID'},
];

function blankTemplate(): store.ServiceTemplate {
    return new store.ServiceTemplate({
        name: '',
        description: '',
        category: 'web',
        icon: '',
        color: '',
        mode: 'build',
        image: '',
        port: 3000,
        dockerfile: 'FROM node:20-alpine\nWORKDIR /app\nCOPY . .\nEXPOSE 3000\nCMD ["npm", "start"]\n',
        cmdOverride: '',
        entrypoint: '',
        workingDir: '',
        envVars: '[]',
        labels: '{}',
        builtin: false,
    });
}

function parseEnvVars(raw?: string): EnvEntry[] {
    if (!raw) return [];
    try {
        const arr = JSON.parse(raw);
        if (!Array.isArray(arr)) return [];
        return arr.map((e: any) => ({
            key: String(e?.key ?? ''),
            value: String(e?.value ?? ''),
            scope: String(e?.scope ?? 'runtime'),
        })).filter((e) => e.key);
    } catch {
        return [];
    }
}

function parseLabels(raw?: string): LabelEntry[] {
    if (!raw) return [];
    try {
        const obj = JSON.parse(raw);
        if (!obj || typeof obj !== 'object') return [];
        return Object.entries(obj).map(([key, value]) => ({key, value: String(value)}));
    } catch {
        return [];
    }
}

export default function TemplateEditorDialog({mode, template, onClose, onSaved}: Props) {
    const [currentMode, setCurrentMode] = useState<Mode>(mode);
    const [draft, setDraft] = useState<store.ServiceTemplate>(() => template ? new store.ServiceTemplate(template) : blankTemplate());
    const [envEntries, setEnvEntries] = useState<EnvEntry[]>(() => parseEnvVars(template?.envVars));
    const [labelEntries, setLabelEntries] = useState<LabelEntry[]>(() => parseLabels(template?.labels));
    const [iconQuery, setIconQuery] = useState('');
    const [error, setError] = useState('');
    const [submitting, setSubmitting] = useState(false);
    const [focusedEnv, setFocusedEnv] = useState<number | null>(null);

    const readOnly = currentMode === 'view';

    useEffect(() => {
        setDraft(template ? new store.ServiceTemplate(template) : blankTemplate());
        setEnvEntries(parseEnvVars(template?.envVars));
        setLabelEntries(parseLabels(template?.labels));
    }, [template]);

    const set = <K extends keyof store.ServiceTemplate>(key: K, value: store.ServiceTemplate[K]) => {
        setDraft((prev) => {
            const next = new store.ServiceTemplate(prev);
            (next as any)[key] = value;
            return next;
        });
    };

    const iconOptions = useMemo(() => {
        const q = iconQuery.trim().toLowerCase();
        const opts = q
            ? TEMPLATE_ICON_OPTIONS.filter((o) => o.label.toLowerCase().includes(q) || o.slug.includes(q))
            : TEMPLATE_ICON_OPTIONS;
        return opts.slice(0, 48);
    }, [iconQuery]);

    const canSubmit = draft.name.trim() !== '' && !submitting;

    const serialize = (): store.ServiceTemplate => {
        const env = envEntries.filter((e) => e.key.trim());
        const labels: Record<string, string> = {};
        labelEntries.forEach((l) => { if (l.key.trim()) labels[l.key.trim()] = l.value; });
        return new store.ServiceTemplate({
            ...draft,
            name: draft.name.trim(),
            envVars: JSON.stringify(env),
            labels: JSON.stringify(labels),
        });
    };

    const submit = () => {
        if (!canSubmit) return;
        setSubmitting(true);
        setError('');
        const payload = serialize();
        const done = UpdateServiceTemplate(payload);
        done.then(() => { setSubmitting(false); onSaved(); })
            .catch((e) => { setSubmitting(false); setError(typeof e === 'string' ? e : e?.message || 'Failed to save template'); });
    };

    const insertToken = (token: string) => {
        setEnvEntries((prev) => {
            if (prev.length === 0) {
                return [{key: '', value: token, scope: 'runtime'}];
            }
            const i = focusedEnv != null ? focusedEnv : prev.length - 1;
            const next = [...prev];
            next[i] = {...next[i], value: (next[i].value ?? '') + token};
            return next;
        });
    };

    const create = () => {
        if (!canSubmit) return;
        setSubmitting(true);
        setError('');
        const payload = new store.ServiceTemplate({...serialize(), id: 0});
        CreateServiceTemplate(payload)
            .then(() => { setSubmitting(false); onSaved(); })
            .catch((e) => { setSubmitting(false); setError(typeof e === 'string' ? e : e?.message || 'Failed to create template'); });
    };

    const clone = () => {
        if (!template) return;
        setSubmitting(true);
        setError('');
        CloneServiceTemplate(template.id)
            .then((cloned) => {
                setSubmitting(false);
                // Open the freshly cloned user-owned copy for editing.
                setCurrentMode('edit');
                setDraft(new store.ServiceTemplate(cloned));
                setEnvEntries(parseEnvVars(cloned.envVars));
                setLabelEntries(parseLabels(cloned.labels));
            })
            .catch((e) => { setSubmitting(false); setError(typeof e === 'string' ? e : e?.message || 'Failed to clone template'); });
    };

    const title = currentMode === 'create'
        ? 'New template'
        : currentMode === 'view'
            ? `${draft.name} (built-in)`
            : `Edit ${draft.name}`;

    const footer = readOnly ? (
        <>
            <button className="btn btn-ghost" onClick={onClose}>Close</button>
            <button className="btn btn-primary" onClick={clone} disabled={submitting}>
                <Copy size={14}/> {submitting ? 'Cloning…' : 'Clone to customize'}
            </button>
        </>
    ) : (
        <>
            <button className="btn btn-ghost" onClick={onClose}>Cancel</button>
            <button
                className="btn btn-primary"
                disabled={!canSubmit}
                onClick={currentMode === 'create' ? create : submit}
            >
                {submitting ? 'Saving…' : currentMode === 'create' ? 'Create' : 'Save'}
            </button>
        </>
    );

    return (
        <Dialog title={title} onClose={onClose} footer={footer}>
            <div className="template-editor">
                {readOnly && (
                    <p className="template-editor-readonly-note">
                        Built-in templates can’t be modified. Clone it to make your own editable copy.
                    </p>
                )}

                <div className="form-field">
                    <label className="form-label">Name</label>
                    <input
                        className="input"
                        value={draft.name}
                        onChange={(e) => set('name', e.target.value)}
                        placeholder="e.g. Next.js API"
                        disabled={readOnly}
                        autoFocus
                    />
                </div>

                <div className="form-field">
                    <label className="form-label">Description</label>
                    <input
                        className="input"
                        value={draft.description}
                        onChange={(e) => set('description', e.target.value)}
                        placeholder="What does this service do?"
                        disabled={readOnly}
                    />
                </div>

                <div className="template-editor-row">
                    <div className="form-field">
                        <label className="form-label">Category</label>
                        <select
                            className="input"
                            value={draft.category}
                            onChange={(e) => set('category', e.target.value)}
                            disabled={readOnly}
                        >
                            {CATEGORIES.map((c) => <option key={c.value} value={c.value}>{c.label}</option>)}
                        </select>
                    </div>
                    <div className="form-field">
                        <label className="form-label">Port</label>
                        <input
                            className="input"
                            type="number"
                            min={1}
                            max={65535}
                            value={draft.port}
                            onChange={(e) => set('port', Number(e.target.value) || 0)}
                            disabled={readOnly}
                        />
                    </div>
                </div>

                <div className="form-field">
                    <label className="form-label">Run mode</label>
                    <div className="template-editor-seg" role="group">
                        {([['build', 'Build from Dockerfile'], ['image', 'Run prebuilt image']] as [string, string][]).map(([value, label]) => (
                            <button
                                key={value}
                                type="button"
                                className={`trigger-seg-btn ${draft.mode === value ? 'trigger-seg-btn--active' : ''}`}
                                onClick={() => set('mode', value)}
                                disabled={readOnly}
                            >
                                {label}
                            </button>
                        ))}
                    </div>
                </div>

                {draft.mode === 'image' && (
                    <div className="form-field">
                        <label className="form-label">Image</label>
                        <span className="settings-hint">Official image to pull, e.g. postgres:16-alpine.</span>
                        <input
                            className="input"
                            value={draft.image}
                            onChange={(e) => set('image', e.target.value)}
                            placeholder="e.g. postgres:16-alpine"
                            disabled={readOnly}
                        />
                    </div>
                )}

                {draft.mode === 'build' && (
                    <div className="form-field">
                        <label className="form-label">Dockerfile</label>
                        <span className="settings-hint">Embedded Dockerfile content written into the service root at create time.</span>
                        <textarea
                            className="input template-editor-dockerfile"
                            value={draft.dockerfile}
                            onChange={(e) => set('dockerfile', e.target.value)}
                            spellCheck={false}
                            disabled={readOnly}
                        />
                    </div>
                )}

                <div className="form-field">
                    <label className="form-label">Icon</label>
                    <div className="template-editor-icon-preview">
                        <TemplateIcon slug={draft.icon} color={draft.color || 'currentColor'} size={22}/>
                        <span className="template-editor-icon-slug">{draft.icon || 'none'}</span>
                    </div>
                    {!readOnly && (
                        <>
                            <input
                                className="input template-editor-icon-search"
                                value={iconQuery}
                                onChange={(e) => setIconQuery(e.target.value)}
                                placeholder="Search brands…"
                            />
                            <div className="template-editor-icon-grid">
                                {iconOptions.map((opt) => (
                                    <button
                                        key={opt.slug}
                                        type="button"
                                        className={`template-editor-icon-btn ${draft.icon === opt.slug ? 'active' : ''}`}
                                        onClick={() => set('icon', opt.slug)}
                                        title={opt.label}
                                    >
                                        <TemplateIcon slug={opt.slug} size={18}/>
                                    </button>
                                ))}
                            </div>
                        </>
                    )}
                </div>

                <div className="form-field">
                    <label className="form-label">Brand color <span className="form-optional">optional</span></label>
                    <input
                        className="input"
                        value={draft.color}
                        onChange={(e) => set('color', e.target.value)}
                        placeholder="#000000 or leave blank to inherit"
                        disabled={readOnly}
                    />
                </div>

                <div className="template-editor-row">
                    <div className="form-field">
                        <label className="form-label">Command</label>
                        <input
                            className="input"
                            value={draft.cmdOverride}
                            onChange={(e) => set('cmdOverride', e.target.value)}
                            placeholder="Override CMD"
                            disabled={readOnly}
                        />
                    </div>
                    <div className="form-field">
                        <label className="form-label">Entrypoint</label>
                        <input
                            className="input"
                            value={draft.entrypoint}
                            onChange={(e) => set('entrypoint', e.target.value)}
                            placeholder="Override ENTRYPOINT"
                            disabled={readOnly}
                        />
                    </div>
                </div>

                <div className="form-field">
                    <label className="form-label">Working directory</label>
                    <input
                        className="input"
                        value={draft.workingDir}
                        onChange={(e) => set('workingDir', e.target.value)}
                        placeholder="e.g. /app"
                        disabled={readOnly}
                    />
                </div>

                <div className="form-field">
                    <label className="form-label">Default env vars</label>
                    <span className="settings-hint">Seeded onto services created from this template.</span>
                    {!readOnly && (
                        <div className="template-editor-tokens">
                            <span className="template-editor-tokens-label">Draft expressions:</span>
                            {DRAFT_TOKENS.map((t) => (
                                <button
                                    key={t.token}
                                    type="button"
                                    className="template-editor-token"
                                    onClick={() => insertToken(t.token)}
                                    title={t.hint}
                                >
                                    {t.token}
                                </button>
                            ))}
                        </div>
                    )}
                    {envEntries.map((entry, i) => (
                        <div key={i} className="settings-kv-row">
                            <input
                                className="input settings-kv-input"
                                value={entry.key}
                                onChange={(e) => {
                                    const next = [...envEntries]; next[i] = {...entry, key: e.target.value}; setEnvEntries(next);
                                }}
                                placeholder="KEY"
                                disabled={readOnly}
                            />
                            <input
                                className="input settings-kv-input"
                                value={entry.value}
                                onFocus={() => setFocusedEnv(i)}
                                onChange={(e) => {
                                    const next = [...envEntries]; next[i] = {...entry, value: e.target.value}; setEnvEntries(next);
                                }}
                                placeholder="value"
                                disabled={readOnly}
                            />
                            <select
                                className="input settings-kv-input settings-kv-scope"
                                value={entry.scope}
                                onChange={(e) => {
                                    const next = [...envEntries]; next[i] = {...entry, scope: e.target.value}; setEnvEntries(next);
                                }}
                                disabled={readOnly}
                            >
                                {SCOPES.map((s) => <option key={s.value} value={s.value}>{s.label}</option>)}
                            </select>
                            {!readOnly && (
                                <button
                                    className="btn btn-ghost settings-kv-remove"
                                    onClick={() => setEnvEntries(envEntries.filter((_, j) => j !== i))}
                                    title="Remove"
                                >
                                    <Trash2 size={12}/>
                                </button>
                            )}
                        </div>
                    ))}
                    {!readOnly && (
                        <button
                            className="btn btn-ghost settings-add-btn"
                            onClick={() => setEnvEntries([...envEntries, {key: '', value: '', scope: 'runtime'}])}
                        >
                            <Plus size={12}/> Add env var
                        </button>
                    )}
                </div>

                <div className="form-field">
                    <label className="form-label">Labels</label>
                    {labelEntries.map((entry, i) => (
                        <div key={i} className="settings-kv-row">
                            <input
                                className="input settings-kv-input"
                                value={entry.key}
                                onChange={(e) => {
                                    const next = [...labelEntries]; next[i] = {...entry, key: e.target.value}; setLabelEntries(next);
                                }}
                                placeholder="Label key"
                                disabled={readOnly}
                            />
                            <input
                                className="input settings-kv-input"
                                value={entry.value}
                                onChange={(e) => {
                                    const next = [...labelEntries]; next[i] = {...entry, value: e.target.value}; setLabelEntries(next);
                                }}
                                placeholder="Label value"
                                disabled={readOnly}
                            />
                            {!readOnly && (
                                <button
                                    className="btn btn-ghost settings-kv-remove"
                                    onClick={() => setLabelEntries(labelEntries.filter((_, j) => j !== i))}
                                    title="Remove"
                                >
                                    <Trash2 size={12}/>
                                </button>
                            )}
                        </div>
                    ))}
                    {!readOnly && (
                        <button
                            className="btn btn-ghost settings-add-btn"
                            onClick={() => setLabelEntries([...labelEntries, {key: '', value: ''}])}
                        >
                            <Plus size={12}/> Add label
                        </button>
                    )}
                </div>

                {error && <p className="form-error">{error}</p>}
            </div>
        </Dialog>
    );
}
