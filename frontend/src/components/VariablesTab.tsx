import {useEffect, useRef, useState} from 'react';
import {ChevronDown, ChevronRight, Download, Eye, EyeOff, FileSearch, Link2, Plus, RefreshCw, Trash2, Upload} from 'lucide-react';
import {
    GetEnvVars, SetEnvVar, DeleteEnvVar, GetNodeSettings, SetNodeSetting, SelectFile,
    GetServiceRoot, SuggestEnvFile, ImportEnvFile, RefreshEnvFile, ExportEnvFile, SetEnvVarScope,
    PreviewEnvVars, ListReferenceTargets,
} from '../../wailsjs/go/main/App';
import {store, deploy} from '../../wailsjs/go/models';
import Dialog from './Dialog';
import './VariablesTab.css';

// Defined locally rather than imported from the generated models: Wails only
// emits a model class for types it sees as a direct return type or array
// element, not as a map value (PreviewEnvVars returns Record<string, X>), so
// EnvPreview keeps getting dropped from models.ts on every real `wails build`.
type EnvPreview = {
    value: string;
    error?: string;
};

const NEW_TARGET_KEY = '__new__';
// Field ids for the two plain-string inputs that aren't tied to an existing
// store.EnvVar row (so autocomplete/insertion can't key off `vars`): the
// +Add row's value field, and the linker's "initial value" field.
const FIELD_NEW_VALUE = '__new_value__';
const FIELD_LINKER_NEW_VALUE = '__linker_new_value__';

const DRAFT_RUNTIME_VARS = [
    {key: 'DRAFT_SERVICE_PORT', description: 'The port this service listens on inside the container'},
    {key: 'DRAFT_INTERNAL_HOSTNAME', description: 'The canonical Draft hostname for service-to-service traffic'},
    {key: 'DRAFT_INTERNAL_URL', description: 'The internal service URL using the service port'},
    {key: 'DRAFT_PUBLIC_HOSTNAME', description: 'The public Draft hostname'},
    {key: 'DRAFT_PUBLIC_URL', description: 'The public URL using the Draft proxy port'},
    {key: 'DRAFT_SERVICE_NAME', description: 'The sanitized service/node label'},
    {key: 'DRAFT_PROJECT_NAME', description: 'The sanitized project name'},
    {key: 'DRAFT_ENVIRONMENT', description: 'The environment name (defaults to "default")'},
];

type VariablesTabProps = {
    nodeId: string;
    projectId: number;
    projectPath: string;
};

// Either creating a brand-new variable whose value is a reference ('new',
// opened from the button beside +Add — needs its own key name), or adding a
// reference into an existing variable's value ('existing', opened from that
// row's link icon — the key is already fixed).
type LinkerState = {
    mode: 'new' | 'existing';
    localKey: string;
    targetId: string;
    targetAttr: string;
    newTargetKey: string;
    newTargetValue: string;
};

// Tracks an in-progress @{...} token the user is typing manually, so we can
// show matching services first, then (once a service + '.' is typed) that
// service's attributes/variables.
type AutocompleteState = {
    key: string;
    stage: 'service' | 'attr';
    query: string;
    start: number;
    end: number;
    target?: deploy.ReferenceTarget;
} | null;

function RuntimeVarsSection() {
    const [expanded, setExpanded] = useState(false);
    return (
        <div className="runtime-vars-section">
            <button className="runtime-vars-toggle" onClick={() => setExpanded(!expanded)}>
                {expanded ? <ChevronDown size={14}/> : <ChevronRight size={14}/>}
                <span>Runtime variables injected by Draft</span>
            </button>
            {expanded && (
                <div className="runtime-vars-list">
                    <p className="runtime-vars-hint">
                        Draft injects internal service identity separately from the public URL. These variables cannot be overridden.
                    </p>
                    {DRAFT_RUNTIME_VARS.map(v => (
                        <div key={v.key} className="runtime-var-row">
                            <span className="var-key">{v.key}</span>
                            <span className="runtime-var-desc">{v.description}</span>
                        </div>
                    ))}
                </div>
            )}
        </div>
    );
}

function VarAutocomplete({autocomplete, linkTargets, onSelectService, onSelectAttr}: {
    autocomplete: AutocompleteState;
    linkTargets: deploy.ReferenceTarget[];
    onSelectService: (t: deploy.ReferenceTarget) => void;
    onSelectAttr: (attr: string) => void;
}) {
    if (!autocomplete) return null;

    if (autocomplete.stage === 'service') {
        const matches = linkTargets.filter(t => t.label.toLowerCase().startsWith(autocomplete.query.toLowerCase()));
        return (
            <div className="var-autocomplete">
                {matches.length === 0 && <span className="var-autocomplete-empty">No matching service</span>}
                {matches.map(t => (
                    <button key={t.nodeId} onMouseDown={e => { e.preventDefault(); onSelectService(t); }}>
                        {t.label}
                    </button>
                ))}
            </div>
        );
    }

    if (!autocomplete.target) {
        return <div className="var-autocomplete"><span className="var-autocomplete-empty">Unknown service</span></div>;
    }
    const options = [...autocomplete.target.attributes, ...autocomplete.target.customKeys]
        .filter(a => a.toLowerCase().startsWith(autocomplete.query.toLowerCase()));
    return (
        <div className="var-autocomplete">
            {options.length === 0 && <span className="var-autocomplete-empty">No matching variable</span>}
            {options.map(a => (
                <button key={a} onMouseDown={e => { e.preventDefault(); onSelectAttr(a); }}>
                    {a}
                </button>
            ))}
        </div>
    );
}

export default function VariablesTab({nodeId, projectId, projectPath}: VariablesTabProps) {
    const [vars, setVars] = useState<store.EnvVar[]>([]);
    const [originals, setOriginals] = useState<Record<string, string>>({});
    const [edits, setEdits] = useState<Record<string, string>>({});
    const [visible, setVisible] = useState<Record<string, boolean>>({});
    const [newKey, setNewKey] = useState('');
    const [newValue, setNewValue] = useState('');
    const [loading, setLoading] = useState(true);
    const [envFile, setEnvFile] = useState('');
    const [serviceRoot, setServiceRoot] = useState('');
    const [showConfirm, setShowConfirm] = useState(false);
    const [syncing, setSyncing] = useState(false);
    const [syncResult, setSyncResult] = useState<store.EnvFileSyncResult | null>(null);
    const [syncError, setSyncError] = useState('');
    const [previews, setPreviews] = useState<Record<string, EnvPreview>>({});
    const [previewVisible, setPreviewVisible] = useState<Record<string, boolean>>({});
    const [linkTargets, setLinkTargets] = useState<deploy.ReferenceTarget[]>([]);
    const [linker, setLinker] = useState<LinkerState | null>(null);
    const [autocomplete, setAutocomplete] = useState<AutocompleteState>(null);
    const fieldRefs = useRef<Record<string, HTMLTextAreaElement | HTMLInputElement | null>>({});

    const load = async () => {
        try {
            const v = await GetEnvVars(nodeId);
            const list = v || [];
            setVars(list);
            const map: Record<string, string> = {};
            list.forEach(x => { map[x.key] = x.value; });
            setOriginals(map);
            setEdits({});
        } catch (e) {
            console.error(e);
        } finally {
            setLoading(false);
        }
    };

    const loadPreviews = async () => {
        try {
            setPreviews(await PreviewEnvVars(nodeId) || {});
        } catch (e) {
            console.error(e);
        }
    };

    const loadLinkTargets = async () => {
        try {
            setLinkTargets(await ListReferenceTargets(nodeId) || []);
        } catch (e) {
            console.error(e);
        }
    };

    const refreshAll = async () => {
        await load();
        await loadPreviews();
        await loadLinkTargets();
    };

    const loadSettings = async () => {
        try {
            const [s, root] = await Promise.all([
                GetNodeSettings(nodeId),
                GetServiceRoot(nodeId, projectId)
            ]);
            setEnvFile(s?.env_file || '');
            setServiceRoot(root || projectPath);
            if (!s?.env_file) {
                const suggestion = await SuggestEnvFile(nodeId, projectId);
                if (suggestion) {
                    setEnvFile(suggestion);
                }
            }
        } catch (e) {
            console.error(e);
        }
    };

    useEffect(() => {
        load();
        loadSettings();
        loadPreviews();
        loadLinkTargets();
    }, [nodeId]);

    const pickEnv = async () => {
        try {
            const p = await SelectFile('Select .env file', '');
            if (p) {
                await SetNodeSetting(nodeId, 'env_file', p);
                setEnvFile(p);
                setSyncResult(null);
                setSyncError('');
            }
        } catch (e) {
            console.error(e);
        }
    };

    const toggle = (key: string) => {
        setVisible(prev => ({...prev, [key]: !prev[key]}));
    };

    const togglePreview = (key: string) => {
        setPreviewVisible(prev => ({...prev, [key]: !prev[key]}));
    };

    const stageEdit = (key: string, value: string) => {
        const original = originals[key] ?? '';
        setEdits(prev => {
            const next = {...prev};
            if (value === original) {
                delete next[key];
            } else {
                next[key] = value;
            }
            return next;
        });
    };

    // getFieldValue/setFieldValue abstract over the different kinds of text
    // fields that can hold a reference token: an existing variable's value
    // (staged into `edits`, backed by `vars`), or one of the plain strings
    // that aren't a store.EnvVar yet (the +Add row's value, the linker's
    // "initial value"). This lets one autocomplete/insertion implementation
    // work across all of them.
    const getFieldValue = (fieldId: string): string => {
        if (fieldId === FIELD_NEW_VALUE) return newValue;
        if (fieldId === FIELD_LINKER_NEW_VALUE) return linker?.newTargetValue ?? '';
        return vars.find(x => x.key === fieldId)?.value ?? '';
    };

    const setFieldValue = (fieldId: string, value: string) => {
        if (fieldId === FIELD_NEW_VALUE) {
            setNewValue(value);
            return;
        }
        if (fieldId === FIELD_LINKER_NEW_VALUE) {
            setLinker(l => l && {...l, newTargetValue: value});
            return;
        }
        setVars(prev => prev.map(x => x.key === fieldId ? store.EnvVar.createFrom({...x, value}) : x));
        stageEdit(fieldId, value);
    };

    // Replaces a field's value between [start,end) with replacement — shared
    // by manual-token insertion, the link picker, and inline autocomplete.
    // Returns the cursor position right after the inserted text.
    const replaceRange = (fieldId: string, start: number, end: number, replacement: string): number => {
        const current = getFieldValue(fieldId);
        const nextValue = current.slice(0, start) + replacement + current.slice(end);
        setFieldValue(fieldId, nextValue);
        return start + replacement.length;
    };

    const focusAt = (fieldId: string, pos: number) => {
        requestAnimationFrame(() => {
            const el = fieldRefs.current[fieldId];
            el?.focus();
            el?.setSelectionRange(pos, pos);
        });
    };

    const insertAtCursor = (fieldId: string, token: string) => {
        const el = fieldRefs.current[fieldId];
        const current = getFieldValue(fieldId);
        const start = el?.selectionStart ?? current.length;
        const end = el?.selectionEnd ?? current.length;
        replaceRange(fieldId, start, end, token);
    };

    const pendingChanges = Object.entries(edits).map(([k, v]) => {
        const original = vars.find(x => x.key === k)?.value ?? '';
        return {key: k, from: original, to: v};
    });

    const saveChanges = async () => {
        try {
            for (const [k, v] of Object.entries(edits)) {
                await SetEnvVar(nodeId, k, v);
            }
            setEdits({});
            setShowConfirm(false);
            setSyncResult(null);
            setSyncError('');
            await refreshAll();
        } catch (e) {
            console.error(e);
        }
    };

    const add = async () => {
        if (!newKey.trim()) return;
        try {
            await SetEnvVar(nodeId, newKey.trim(), newValue);
            setNewKey('');
            setNewValue('');
            setSyncResult(null);
            setSyncError('');
            await refreshAll();
        } catch (e) {
            console.error(e);
        }
    };

    const removeVar = async (key: string) => {
        if (!window.confirm(`Delete ${key}? This can't be undone.`)) return;
        try {
            await DeleteEnvVar(nodeId, key);
            await refreshAll();
        } catch (e) {
            console.error(e);
        }
    };

    const toggleBuildArg = async (variable: store.EnvVar) => {
        const isBuildArg = variable.scope === 'build' || variable.scope === 'both';
        const nextScope = isBuildArg ? 'runtime' : 'both';
        try {
            await SetEnvVarScope(nodeId, variable.key, nextScope);
            setVars(prev => prev.map(v =>
                v.key === variable.key ? store.EnvVar.createFrom({...v, scope: nextScope}) : v,
            ));
        } catch (e) {
            console.error(e);
        }
    };

    // --- Linker: create a reference either into an existing variable's value
    // (mode 'existing', opened from that row) or as a brand-new variable
    // (mode 'new', opened from the button beside +Add). ---

    const openLinkerForKey = (key: string) => {
        setVisible(prev => ({...prev, [key]: true}));
        setLinker({mode: 'existing', localKey: key, targetId: '', targetAttr: '', newTargetKey: '', newTargetValue: ''});
    };

    const openNewLinker = () => {
        setLinker({mode: 'new', localKey: '', targetId: '', targetAttr: '', newTargetKey: '', newTargetValue: ''});
    };

    const closeLinker = () => setLinker(null);

    const linkerTarget = linker ? linkTargets.find(t => t.nodeId === linker.targetId) ?? null : null;

    const confirmLinker = async () => {
        if (!linker || !linkerTarget) return;
        try {
            let attrName = linker.targetAttr;
            if (attrName === NEW_TARGET_KEY) {
                const key = linker.newTargetKey.trim();
                if (!key) return;
                await SetEnvVar(linkerTarget.nodeId, key, linker.newTargetValue);
                attrName = key;
            }
            if (!attrName) return;
            const token = `@{${linkerTarget.label}.${attrName}}`;

            if (linker.mode === 'existing') {
                insertAtCursor(linker.localKey, token);
                setLinker(null);
                // The insert only stages an edit (like manual typing) — it's
                // not saved until "Save changes", so don't reload vars here,
                // that would discard the unsaved token we just inserted.
                if (linker.targetAttr === NEW_TARGET_KEY) await loadLinkTargets();
            } else {
                const localKey = linker.localKey.trim();
                if (!localKey) return;
                await SetEnvVar(nodeId, localKey, token);
                setLinker(null);
                await refreshAll();
            }
        } catch (e) {
            console.error(e);
        }
    };

    // --- Manual @{...} autocomplete while typing in a textarea or input. ---

    const handleCaretActivity = (key: string, el: HTMLTextAreaElement | HTMLInputElement) => {
        const value = el.value;
        const cursor = el.selectionStart ?? value.length;
        const before = value.slice(0, cursor);
        const openIdx = before.lastIndexOf('@{');
        if (openIdx === -1) {
            setAutocomplete(a => (a?.key === key ? null : a));
            return;
        }
        const inner = before.slice(openIdx + 2);
        if (inner.includes('}') || inner.includes('@') || inner.includes('\n')) {
            setAutocomplete(a => (a?.key === key ? null : a));
            return;
        }
        const dotIdx = inner.indexOf('.');
        if (dotIdx === -1) {
            setAutocomplete({key, stage: 'service', query: inner, start: openIdx + 2, end: cursor});
        } else {
            const label = inner.slice(0, dotIdx).trim();
            const attrQuery = inner.slice(dotIdx + 1);
            const target = linkTargets.find(t => t.label.toLowerCase() === label.toLowerCase());
            setAutocomplete({key, stage: 'attr', query: attrQuery, start: openIdx + 2 + dotIdx + 1, end: cursor, target});
        }
    };

    const selectAutocompleteService = (target: deploy.ReferenceTarget) => {
        if (!autocomplete) return;
        const {key, start, end} = autocomplete;
        const pos = replaceRange(key, start, end, `${target.label}.`);
        setAutocomplete({key, stage: 'attr', query: '', start: pos, end: pos, target});
        focusAt(key, pos);
    };

    const selectAutocompleteAttr = (attrName: string) => {
        if (!autocomplete) return;
        const {key, start, end} = autocomplete;
        const pos = replaceRange(key, start, end, `${attrName}}`);
        setAutocomplete(null);
        focusAt(key, pos);
    };

    const persistEnvPath = async () => {
        await SetNodeSetting(nodeId, 'env_file', envFile.trim());
    };

    const runSync = async (action: 'import' | 'refresh' | 'export') => {
        setSyncing(true);
        setSyncError('');
        setSyncResult(null);
        try {
            await persistEnvPath();
            let result: store.EnvFileSyncResult;
            if (action === 'import') {
                result = await ImportEnvFile(nodeId, envFile.trim());
            } else if (action === 'refresh') {
                result = await RefreshEnvFile(nodeId);
            } else {
                result = await ExportEnvFile(nodeId);
            }
            setSyncResult(result);
            await load();
        } catch (e: any) {
            setSyncError(typeof e === 'string' ? e : e?.message || `${action} failed`);
        } finally {
            setSyncing(false);
        }
    };

    const conflicts = syncResult?.conflicts ?? [];
    const resultText = syncResult ? [
        syncResult.imported ? `${syncResult.imported} imported` : '',
        syncResult.updated ? `${syncResult.updated} updated` : '',
        syncResult.unchanged ? `${syncResult.unchanged} unchanged` : '',
        syncResult.exported ? `${syncResult.exported} exported` : '',
        syncResult.skipped ? `${syncResult.skipped} skipped` : '',
        conflicts.length ? `${conflicts.length} conflict${conflicts.length > 1 ? 's' : ''}` : '',
    ].filter(Boolean).join(' · ') : '';

    if (loading) {
        return <div className="variables-loading">Loading...</div>;
    }

    const renderLinkerPanel = () => (
        <div className="var-link-picker">
            {linker!.mode === 'new' && (
                <input
                    className="var-link-key-input"
                    placeholder="NEW_KEY"
                    value={linker!.localKey}
                    onChange={e => setLinker(l => l && {...l, localKey: e.target.value})}
                />
            )}
            <select
                value={linker!.targetId}
                onChange={e => setLinker(l => l && {...l, targetId: e.target.value, targetAttr: '', newTargetKey: '', newTargetValue: ''})}
            >
                <option value="">Select a service…</option>
                {linkTargets.map(t => (
                    <option key={t.nodeId} value={t.nodeId}>{t.label}</option>
                ))}
            </select>
            {linkerTarget && (
                <select
                    value={linker!.targetAttr}
                    onChange={e => setLinker(l => l && {...l, targetAttr: e.target.value})}
                >
                    <option value="">Select a value…</option>
                    <optgroup label="Address">
                        {linkerTarget.attributes.map(a => (
                            <option key={a} value={a}>{a}</option>
                        ))}
                    </optgroup>
                    {linkerTarget.customKeys.length > 0 && (
                        <optgroup label="Variables">
                            {linkerTarget.customKeys.map(k => (
                                <option key={k} value={k}>{k}</option>
                            ))}
                        </optgroup>
                    )}
                    <option value={NEW_TARGET_KEY}>+ New variable on {linkerTarget.label}…</option>
                </select>
            )}
            {linker!.targetAttr === NEW_TARGET_KEY && linkerTarget && (
                <>
                    <input
                        className="var-link-key-input"
                        placeholder={`KEY on ${linkerTarget.label}`}
                        value={linker!.newTargetKey}
                        onChange={e => setLinker(l => l && {...l, newTargetKey: e.target.value})}
                    />
                    <input
                        ref={el => { fieldRefs.current[FIELD_LINKER_NEW_VALUE] = el; }}
                        className="var-link-key-input"
                        placeholder="initial value"
                        value={linker!.newTargetValue}
                        onChange={e => {
                            setLinker(l => l && {...l, newTargetValue: e.target.value});
                            handleCaretActivity(FIELD_LINKER_NEW_VALUE, e.target);
                        }}
                        onClick={e => handleCaretActivity(FIELD_LINKER_NEW_VALUE, e.currentTarget)}
                        onKeyUp={e => handleCaretActivity(FIELD_LINKER_NEW_VALUE, e.currentTarget)}
                        onBlur={() => {
                            setTimeout(() => setAutocomplete(a => (a?.key === FIELD_LINKER_NEW_VALUE ? null : a)), 120);
                        }}
                    />
                    {autocomplete?.key === FIELD_LINKER_NEW_VALUE && (
                        <VarAutocomplete
                            autocomplete={autocomplete}
                            linkTargets={linkTargets}
                            onSelectService={selectAutocompleteService}
                            onSelectAttr={selectAutocompleteAttr}
                        />
                    )}
                </>
            )}
            <button className="btn btn-primary" onClick={confirmLinker}>Link</button>
            <button className="btn btn-ghost" onClick={closeLinker}>Cancel</button>
        </div>
    );

    return (
        <div className="variables-tab">
            <div className="form-field">
                <label className="form-label">Linked environment file</label>
                <span className="settings-hint">Draft stores variables in SQLite. Use this file only for explicit import, refresh, or export.</span>
                <div className="input-with-action">
                    <input
                        className="input"
                        value={envFile}
                        onChange={(e) => setEnvFile(e.target.value)}
                        onBlur={async () => {
                            await persistEnvPath();
                        }}
                        placeholder=".env"
                    />
                    <button className="btn btn-ghost input-action-btn" onClick={pickEnv} title="Browse for .env">
                        <FileSearch size={14}/>
                    </button>
                </div>
                {!envFile && serviceRoot && (
                    <span className="settings-hint">No linked .env file yet. Draft variables can still be managed here.</span>
                )}
                <div className="env-sync-actions">
                    <button className="btn btn-ghost" onClick={() => runSync('import')} disabled={syncing || !envFile.trim()}>
                        <Upload size={13}/> Import
                    </button>
                    <button className="btn btn-ghost" onClick={() => runSync('refresh')} disabled={syncing}>
                        <RefreshCw size={13}/> Refresh
                    </button>
                    <button className="btn btn-ghost" onClick={() => runSync('export')} disabled={syncing}>
                        <Download size={13}/> Export
                    </button>
                </div>
                {(resultText || syncError) && (
                    <div className={`env-sync-status ${syncError ? 'env-sync-status--error' : ''}`}>
                        {syncError || resultText}
                    </div>
                )}
                {conflicts.length > 0 && (
                    <div className="env-conflicts">
                        {conflicts.map((conflict) => (
                            <div key={conflict.key} className="env-conflict-row">
                                <span className="var-key">{conflict.key}</span>
                                <span>Draft kept its value instead of overwriting it from file.</span>
                            </div>
                        ))}
                    </div>
                )}
            </div>

            <div className="var-add-col">
                <div className="var-add">
                    <input
                        placeholder="KEY"
                        value={newKey}
                        onChange={e => setNewKey(e.target.value)}
                    />
                    <input
                        ref={el => { fieldRefs.current[FIELD_NEW_VALUE] = el; }}
                        placeholder="value"
                        value={newValue}
                        onChange={e => {
                            setNewValue(e.target.value);
                            handleCaretActivity(FIELD_NEW_VALUE, e.target);
                        }}
                        onClick={e => handleCaretActivity(FIELD_NEW_VALUE, e.currentTarget)}
                        onKeyUp={e => handleCaretActivity(FIELD_NEW_VALUE, e.currentTarget)}
                        onBlur={() => {
                            setTimeout(() => setAutocomplete(a => (a?.key === FIELD_NEW_VALUE ? null : a)), 120);
                        }}
                    />
                    <button className="btn btn-primary" onClick={add}>
                        <Plus size={14}/> Add
                    </button>
                    <button
                        className={`btn btn-ghost ${linker?.mode === 'new' ? 'var-toggle--active' : ''}`}
                        onClick={() => linker?.mode === 'new' ? closeLinker() : openNewLinker()}
                        disabled={linkTargets.length === 0}
                        title="Add a new variable that references another service"
                    >
                        <Link2 size={14}/> Link
                    </button>
                </div>
                {autocomplete?.key === FIELD_NEW_VALUE && (
                    <VarAutocomplete
                        autocomplete={autocomplete}
                        linkTargets={linkTargets}
                        onSelectService={selectAutocompleteService}
                        onSelectAttr={selectAutocompleteAttr}
                    />
                )}
            </div>

            {linker?.mode === 'new' && renderLinkerPanel()}

            <div className="variables-list">
                {vars.length === 0 && (
                    <div className="variables-empty">No Draft variables yet.</div>
                )}
                {vars.map(v => (
                    <div key={v.key} className="var-row">
                        <div className="var-key-cell">
                            <div className="var-key" title={v.key}>{v.key}</div>
                            <div className={`var-source var-source--${v.source || 'manual'}`}>
                                {v.source || 'manual'}
                            </div>
                        </div>
                        <div className="var-value-col">
                            <div className="var-value">
                                {visible[v.key] ? (
                                    <textarea
                                        ref={el => { fieldRefs.current[v.key] = el; }}
                                        className="var-value-editor"
                                        value={v.value}
                                        rows={v.value.includes('\n') || v.value.length > 160 ? 7 : 2}
                                        wrap="off"
                                        spellCheck={false}
                                        onChange={e => {
                                            const nv = [...vars];
                                            const idx = nv.findIndex(x => x.key === v.key);
                                            nv[idx] = store.EnvVar.createFrom({...v, value: e.target.value});
                                            setVars(nv);
                                            stageEdit(v.key, e.target.value);
                                            handleCaretActivity(v.key, e.target);
                                        }}
                                        onClick={e => handleCaretActivity(v.key, e.currentTarget)}
                                        onKeyUp={e => handleCaretActivity(v.key, e.currentTarget)}
                                        onBlur={() => {
                                            setTimeout(() => setAutocomplete(a => (a?.key === v.key ? null : a)), 120);
                                        }}
                                    />
                                ) : (
                                    <input
                                        className="var-value-mask"
                                        type="password"
                                        value={v.value}
                                        disabled
                                        readOnly
                                    />
                                )}
                                <button className="var-toggle" onClick={() => toggle(v.key)}>
                                    {visible[v.key] ? <EyeOff size={14}/> : <Eye size={14}/>}
                                </button>
                                <button
                                    className={`var-toggle ${linker?.mode === 'existing' && linker.localKey === v.key ? 'var-toggle--active' : ''}`}
                                    onClick={() => (linker?.mode === 'existing' && linker.localKey === v.key) ? closeLinker() : openLinkerForKey(v.key)}
                                    title="Reference another service's variable"
                                    disabled={linkTargets.length === 0}
                                >
                                    <Link2 size={14}/>
                                </button>
                                <button
                                    className={`var-scope-toggle ${v.scope === 'build' || v.scope === 'both' ? 'var-scope-toggle--active' : ''}`}
                                    onClick={() => toggleBuildArg(v)}
                                    title={v.scope === 'build' || v.scope === 'both' ? 'Included in Docker build args' : 'Runtime only'}
                                >
                                    ARG
                                </button>
                                <button className="var-toggle var-toggle--danger" onClick={() => removeVar(v.key)} title="Delete variable">
                                    <Trash2 size={14}/>
                                </button>
                            </div>
                            {previews[v.key]?.error && (
                                <div className="var-preview var-preview--error">{previews[v.key].error}</div>
                            )}
                            {!previews[v.key]?.error && previews[v.key] && previews[v.key].value !== v.value && (
                                <div className="var-preview">
                                    resolves to: {previewVisible[v.key] ? (previews[v.key].value || '(empty)') : '••••••••'}
                                    <button className="var-preview-toggle" onClick={() => togglePreview(v.key)} title={previewVisible[v.key] ? 'Hide resolved value' : 'Show resolved value'}>
                                        {previewVisible[v.key] ? <EyeOff size={12}/> : <Eye size={12}/>}
                                    </button>
                                </div>
                            )}
                            {autocomplete?.key === v.key && (
                                <VarAutocomplete
                                    autocomplete={autocomplete}
                                    linkTargets={linkTargets}
                                    onSelectService={selectAutocompleteService}
                                    onSelectAttr={selectAutocompleteAttr}
                                />
                            )}
                            {linker?.mode === 'existing' && linker.localKey === v.key && renderLinkerPanel()}
                        </div>
                    </div>
                ))}
            </div>

            <RuntimeVarsSection />

            {Object.keys(edits).length > 0 && (
                <button className="btn btn-primary save-btn" onClick={() => setShowConfirm(true)}>
                    Save {Object.keys(edits).length} Draft change{Object.keys(edits).length > 1 ? 's' : ''}
                </button>
            )}

            {showConfirm && (
                <Dialog title="Confirm variable changes" onClose={() => setShowConfirm(false)} footer={
                    <div style={{display:'flex',gap:8,justifyContent:'flex-end'}}>
                        <button className="btn btn-ghost" onClick={() => setShowConfirm(false)}>Cancel</button>
                        <button className="btn btn-primary" onClick={saveChanges}>Save changes</button>
                    </div>
                }>
                    <p className="var-confirm-note">
                        These changes update Draft's database. Use Export when you want to write them back to the linked .env file.
                    </p>
                    <div className="var-diff">
                        {pendingChanges.map(c => (
                            <div key={c.key} className="var-diff-row">
                                <div className="var-key">{c.key}</div>
                                <div className="var-diff-values">
                                    <span className="old">{c.from || '(empty)'}</span>
                                    <span>→</span>
                                    <span className="new">{c.to}</span>
                                </div>
                            </div>
                        ))}
                    </div>
                </Dialog>
            )}
        </div>
    );
}
