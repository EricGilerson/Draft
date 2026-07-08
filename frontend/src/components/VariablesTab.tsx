import {useCallback, useEffect, useMemo, useRef, useState} from 'react';
import {AlertTriangle, ChevronDown, ChevronRight, Download, Eye, EyeOff, Link2, Plus, RefreshCw, Trash2, Upload, FileSearch} from 'lucide-react';
import {
    GetEnvVars, SetEnvVar, SetNodeSetting, SelectFile,
    GetServiceRoot, SuggestEnvFile, ImportEnvFile, RefreshEnvFile, ExportEnvFile,
    PreviewEnvVars, ListReferenceTargets, ListReferenceIssues,
    InspectDockerfileBuildInfo, ListProjectEnvVars, ListAppSecrets,
} from '../../wailsjs/go/main/App';
import {store, deploy} from '../../wailsjs/go/models';
import {useServiceConfigEditor} from '../lib/serviceConfigEditor';
import {computeBuildEnvWarnings} from '../lib/buildEnvWarnings';
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
    source: 'service' | 'app-secret' | 'project';
    localKey: string;
    targetId: string;
    targetAttr: string;
    appSecretKey: string;
    projectRefKey: string;
    newTargetKey: string;
    newTargetValue: string;
};

// Tracks an in-progress @{...}, {{secret...}}, or {{project...}} token while typing.
type AutocompleteState = {
    key: string;
    stage: 'service' | 'attr' | 'secret' | 'project';
    query: string;
    start: number;
    end: number;
    target?: deploy.ReferenceTarget;
} | null;

type ExprOpenMatch = {
    stage: 'secret' | 'project';
    openIdx: number;
    prefixLen: number;
};

function findExprOpen(before: string): ExprOpenMatch | null {
    const candidates: ExprOpenMatch[] = [
        {stage: 'project', openIdx: before.lastIndexOf('{{project.'), prefixLen: 10},
        {stage: 'secret', openIdx: before.lastIndexOf('{{secret.'), prefixLen: 9},
    ];
    let best: ExprOpenMatch | null = null;
    for (const c of candidates) {
        if (c.openIdx < 0) continue;
        if (!best || c.openIdx > best.openIdx) {
            best = c;
        }
    }
    return best;
}

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

function ProjectVarsSection({vars, serviceKeys, loading}: {
    vars: store.ProjectEnvVar[];
    serviceKeys: Set<string>;
    loading: boolean;
}) {
    const [expanded, setExpanded] = useState(true);

    if (!loading && vars.length === 0) {
        return null;
    }

    return (
        <div className="project-vars-section">
            <button className="runtime-vars-toggle" onClick={() => setExpanded(!expanded)}>
                {expanded ? <ChevronDown size={14}/> : <ChevronRight size={14}/>}
                <span>Project references{vars.length > 0 ? ` (${vars.length})` : ''}</span>
            </button>
            {expanded && (
                <div className="project-vars-list">
                    <p className="runtime-vars-hint">
                        Reference shared project values using {`{{project.KEY}}`}. Use the Link button or type to insert a token.
                    </p>
                    {loading && <div className="variables-empty">Loading project variables…</div>}
                    {!loading && vars.map((v) => {
                        const definedLocally = serviceKeys.has(v.key);
                        const token = `{{project.${v.key}}}`;
                        return (
                            <div key={v.key} className={`project-var-row ${definedLocally ? 'project-var-row--overridden' : ''}`}>
                                <div className="var-key-cell">
                                    <div className="var-key" title={v.key}>{v.key}</div>
                                    <div className="var-source var-source--project">project var</div>
                                </div>
                                <div className="var-value-col">
                                    <div className="var-value project-var-value">
                                        <code className="project-var-token">{token}</code>
                                        <span className="project-var-scope" title="Variable scope">{v.scope || 'runtime'}</span>
                                        {definedLocally && (
                                            <span className="project-var-override" title="This service also defines its own variable with the same key">
                                                local key too
                                            </span>
                                        )}
                                    </div>
                                </div>
                            </div>
                        );
                    })}
                </div>
            )}
        </div>
    );
}

function VarAutocomplete({autocomplete, linkTargets, appSecrets, projectVars, onSelectService, onSelectAttr, onSelectSecret, onSelectProject}: {
    autocomplete: AutocompleteState;
    linkTargets: deploy.ReferenceTarget[];
    appSecrets: store.AppSecret[];
    projectVars: store.ProjectEnvVar[];
    onSelectService: (t: deploy.ReferenceTarget) => void;
    onSelectAttr: (attr: string) => void;
    onSelectSecret: (key: string) => void;
    onSelectProject: (key: string) => void;
}) {
    if (!autocomplete) return null;

    if (autocomplete.stage === 'secret') {
        const matches = appSecrets.filter(s => s.key.toLowerCase().startsWith(autocomplete.query.toLowerCase()));
        return (
            <div className="var-autocomplete">
                {matches.length === 0 && <span className="var-autocomplete-empty">No matching app secret</span>}
                {matches.map(s => (
                    <button key={s.key} onMouseDown={e => { e.preventDefault(); onSelectSecret(s.key); }}>
                        {s.key}
                    </button>
                ))}
            </div>
        );
    }

    if (autocomplete.stage === 'project') {
        const matches = projectVars.filter(s => s.key.toLowerCase().startsWith(autocomplete.query.toLowerCase()));
        return (
            <div className="var-autocomplete">
                {matches.length === 0 && <span className="var-autocomplete-empty">No matching project value</span>}
                {matches.map(s => (
                    <button key={s.key} onMouseDown={e => { e.preventDefault(); onSelectProject(s.key); }}>
                        {s.key}
                    </button>
                ))}
            </div>
        );
    }

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
    const {
        appliedSettings,
        stagedEnvChanges,
        setEnvDraftUpsert,
        setEnvDraftDelete,
        isSessionDirty,
        hasStagedChanges,
        reload,
    } = useServiceConfigEditor();
    const [vars, setVars] = useState<store.EnvVar[]>([]);
    const [originals, setOriginals] = useState<Record<string, string>>({});
    const [edits, setEdits] = useState<Record<string, string>>({});
    const [visible, setVisible] = useState<Record<string, boolean>>({});
    const [newKey, setNewKey] = useState('');
    const [newValue, setNewValue] = useState('');
    const [loading, setLoading] = useState(true);
    const [envFile, setEnvFile] = useState('');
    const [serviceRoot, setServiceRoot] = useState('');
    const [syncing, setSyncing] = useState(false);
    const [syncResult, setSyncResult] = useState<store.EnvFileSyncResult | null>(null);
    const [syncError, setSyncError] = useState('');
    const [previews, setPreviews] = useState<Record<string, EnvPreview>>({});
    const [previewVisible, setPreviewVisible] = useState<Record<string, boolean>>({});
    const [linkTargets, setLinkTargets] = useState<deploy.ReferenceTarget[]>([]);
    const [referenceIssues, setReferenceIssues] = useState<deploy.ReferenceIssue[]>([]);
    const [linker, setLinker] = useState<LinkerState | null>(null);
    const [autocomplete, setAutocomplete] = useState<AutocompleteState>(null);
    const [buildInfo, setBuildInfo] = useState<deploy.DockerfileBuildInfo | null>(null);
    const [projectVars, setProjectVars] = useState<store.ProjectEnvVar[]>([]);
    const [appSecrets, setAppSecrets] = useState<store.AppSecret[]>([]);
    const [loadingProjectVars, setLoadingProjectVars] = useState(true);
    const fieldRefs = useRef<Record<string, HTMLTextAreaElement | HTMLInputElement | null>>({});

    const applyStagedEnv = useCallback((list: store.EnvVar[]) => {
        let out = [...list];
        for (const ch of stagedEnvChanges) {
            if (ch.delete) {
                out = out.filter((x) => x.key !== ch.key);
                continue;
            }
            const existing = out.find((x) => x.key === ch.key);
            if (existing) {
                out = out.map((x) => x.key === ch.key
                    ? store.EnvVar.createFrom({...x, value: ch.value, scope: ch.scope || x.scope})
                    : x);
            } else {
                out.push(store.EnvVar.createFrom({
                    nodeId,
                    key: ch.key,
                    value: ch.value,
                    scope: ch.scope || 'runtime',
                    source: 'manual',
                }));
            }
        }
        out.sort((a, b) => a.key.localeCompare(b.key));
        return out;
    }, [nodeId, stagedEnvChanges]);

    const load = async () => {
        try {
            const v = await GetEnvVars(nodeId);
            const list = applyStagedEnv(v || []);
            setVars(list);
            const map: Record<string, string> = {};
            list.forEach((x: store.EnvVar) => { map[x.key] = x.value; });
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

    const loadReferenceIssues = async () => {
        try {
            setReferenceIssues(await ListReferenceIssues(nodeId) || []);
        } catch (e) {
            console.error(e);
        }
    };

    const loadBuildInfo = async () => {
        try {
            setBuildInfo(await InspectDockerfileBuildInfo(nodeId));
        } catch (e) {
            console.error(e);
        }
    };

    const loadProjectVars = async () => {
        try {
            setProjectVars(await ListProjectEnvVars(projectId) || []);
        } catch (e) {
            console.error(e);
        } finally {
            setLoadingProjectVars(false);
        }
    };

    const loadAppSecrets = async () => {
        try {
            setAppSecrets(await ListAppSecrets() || []);
        } catch (e) {
            console.error(e);
        }
    };

    const refreshAll = async () => {
        await load();
        await loadProjectVars();
        await loadAppSecrets();
        await loadPreviews();
        await loadLinkTargets();
        await loadReferenceIssues();
        await loadBuildInfo();
    };

    const loadSettings = async () => {
        try {
            const root = await GetServiceRoot(nodeId, projectId);
            setEnvFile(appliedSettings.env_file || '');
            setServiceRoot(root || projectPath);
            if (!appliedSettings.env_file) {
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
        setLoadingProjectVars(true);
        void load();
        void loadProjectVars();
        void loadAppSecrets();
        void loadPreviews();
        void loadLinkTargets();
    }, [nodeId, projectId]);

    // The Dockerfile facts only change when the node or its Dockerfile/service
    // root settings change, so keep this off the hot per-keystroke path.
    useEffect(() => {
        void loadBuildInfo();
    }, [nodeId, appliedSettings.dockerfile, appliedSettings.service_root]);

    const buildWarnings = useMemo(
        () => computeBuildEnvWarnings(vars, buildInfo),
        [vars, buildInfo],
    );

    const serviceVarKeys = useMemo(() => new Set(vars.map((v) => v.key)), [vars]);

    useEffect(() => {
        if (!isSessionDirty) {
            void load();
        }
    }, [stagedEnvChanges, isSessionDirty]);

    useEffect(() => {
        void loadSettings();
    }, [nodeId, projectId, appliedSettings.env_file]);

    const persistEnvPath = useCallback(async (path?: string) => {
        const trimmed = (path ?? envFile).trim();
        if (trimmed === (appliedSettings.env_file || '')) {
            return;
        }
        await SetNodeSetting(nodeId, 'env_file', trimmed);
        await reload();
    }, [nodeId, envFile, appliedSettings.env_file, reload]);

    const pickEnv = async () => {
        try {
            const p = await SelectFile('Select .env file', '');
            if (p) {
                setEnvFile(p);
                setSyncResult(null);
                setSyncError('');
                await persistEnvPath(p);
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
        const hasOriginal = Object.prototype.hasOwnProperty.call(originals, key);
        const original = hasOriginal ? originals[key] : null;
        const variable = vars.find((x) => x.key === key);
        setEdits(prev => {
            const next = {...prev};
            if (hasOriginal && value === original) {
                delete next[key];
            } else {
                next[key] = value;
            }
            return next;
        });
        if (hasOriginal && value === original) {
            setEnvDraftDelete(key, false);
            return;
        }
        setEnvDraftUpsert({
            key,
            value,
            scope: variable?.scope || 'runtime',
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

    const add = () => {
        if (!newKey.trim()) return;
        const key = newKey.trim();
        if (vars.some((v) => v.key === key)) {
            window.alert(`Variable ${key} already exists on this service.`);
            return;
        }
        setEnvDraftUpsert({key, value: newValue, scope: 'runtime'});
        setVars(prev => [...prev, store.EnvVar.createFrom({
            nodeId,
            key,
            value: newValue,
            scope: 'runtime',
            source: 'manual',
        })]);
        setNewKey('');
        setNewValue('');
        setSyncResult(null);
        setSyncError('');
    };

    const removeVar = (key: string) => {
        if (!window.confirm(`Delete ${key}? It will be removed on the next deploy.`)) return;
        setEnvDraftDelete(key, true);
        setVars(prev => prev.filter((v) => v.key !== key));
        setEdits(prev => {
            const next = {...prev};
            delete next[key];
            return next;
        });
    };

    const toggleBuildArg = (variable: store.EnvVar) => {
        const isBuildArg = variable.scope === 'build' || variable.scope === 'both';
        const nextScope = isBuildArg ? 'runtime' : 'both';
        setEnvDraftUpsert({key: variable.key, value: variable.value, scope: nextScope});
        setVars(prev => prev.map(v =>
            v.key === variable.key ? store.EnvVar.createFrom({...v, scope: nextScope}) : v,
        ));
    };

    // --- Linker: create a reference either into an existing variable's value
    // (mode 'existing', opened from that row) or as a brand-new variable
    // (mode 'new', opened from the button beside +Add). ---

    const openLinkerForKey = (key: string) => {
        setVisible(prev => ({...prev, [key]: true}));
        setLinker({mode: 'existing', source: 'service', localKey: key, targetId: '', targetAttr: '', appSecretKey: '', projectRefKey: '', newTargetKey: '', newTargetValue: ''});
    };

    const openNewLinker = () => {
        setLinker({mode: 'new', source: 'service', localKey: '', targetId: '', targetAttr: '', appSecretKey: '', projectRefKey: '', newTargetKey: '', newTargetValue: ''});
    };

    const closeLinker = () => setLinker(null);

    const linkerTarget = linker ? linkTargets.find(t => t.nodeId === linker.targetId) ?? null : null;

    const confirmLinker = async () => {
        if (!linker) return;
        try {
            if (linker.source === 'app-secret') {
                const secretKey = linker.appSecretKey.trim();
                if (!secretKey) return;
                const token = `{{secret.${secretKey}}}`;
                if (linker.mode === 'existing') {
                    insertAtCursor(linker.localKey, token);
                    setLinker(null);
                } else {
                    const localKey = linker.localKey.trim();
                    if (!localKey) return;
                    await SetEnvVar(nodeId, localKey, token);
                    setLinker(null);
                    await refreshAll();
                }
                return;
            }

            if (linker.source === 'project') {
                const refKey = linker.projectRefKey.trim();
                if (!refKey) return;
                const token = `{{project.${refKey}}}`;
                if (linker.mode === 'existing') {
                    insertAtCursor(linker.localKey, token);
                    setLinker(null);
                } else {
                    const localKey = linker.localKey.trim();
                    if (!localKey) return;
                    await SetEnvVar(nodeId, localKey, token);
                    setLinker(null);
                    await refreshAll();
                }
                return;
            }

            if (!linkerTarget) return;
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

        const exprOpen = findExprOpen(before);
        const serviceOpenIdx = before.lastIndexOf('@{');
        if (exprOpen && (serviceOpenIdx < 0 || exprOpen.openIdx > serviceOpenIdx)) {
            const inner = before.slice(exprOpen.openIdx + exprOpen.prefixLen);
            if (inner.includes('}') || inner.includes('\n')) {
                setAutocomplete(a => (a?.key === key ? null : a));
                return;
            }
            setAutocomplete({key, stage: exprOpen.stage, query: inner, start: exprOpen.openIdx + exprOpen.prefixLen, end: cursor});
            return;
        }

        const openIdx = serviceOpenIdx;
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

    const selectAutocompleteSecret = (secretKey: string) => {
        if (!autocomplete) return;
        const {key, start, end} = autocomplete;
        const pos = replaceRange(key, start, end, `${secretKey}}}`);
        setAutocomplete(null);
        focusAt(key, pos);
    };

    const selectAutocompleteProject = (refKey: string) => {
        if (!autocomplete) return;
        const {key, start, end} = autocomplete;
        const pos = replaceRange(key, start, end, `${refKey}}}`);
        setAutocomplete(null);
        focusAt(key, pos);
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

    const canLink = linkTargets.length > 0 || appSecrets.length > 0 || projectVars.length > 0;

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
                value={linker!.source}
                onChange={e => setLinker(l => l && {...l, source: e.target.value as LinkerState['source'], targetId: '', targetAttr: '', appSecretKey: '', projectRefKey: ''})}
            >
                <option value="service">Service reference</option>
                <option value="app-secret">App secret</option>
                <option value="project">Project value</option>
            </select>
            {linker!.source === 'app-secret' ? (
                <select
                    value={linker!.appSecretKey}
                    onChange={e => setLinker(l => l && {...l, appSecretKey: e.target.value})}
                >
                    <option value="">Select app secret…</option>
                    {appSecrets.map(s => (
                        <option key={s.key} value={s.key}>{s.key}</option>
                    ))}
                </select>
            ) : linker!.source === 'project' ? (
                <select
                    value={linker!.projectRefKey}
                    onChange={e => setLinker(l => l && {...l, projectRefKey: e.target.value})}
                >
                    <option value="">Select project value…</option>
                    {projectVars.map(s => (
                        <option key={s.key} value={s.key}>{s.key}</option>
                    ))}
                </select>
            ) : (
            <>
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
                            appSecrets={appSecrets}
                            projectVars={projectVars}
                            onSelectService={selectAutocompleteService}
                            onSelectAttr={selectAutocompleteAttr}
                            onSelectSecret={selectAutocompleteSecret}
                            onSelectProject={selectAutocompleteProject}
                        />
                    )}
                </>
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
                <span className="settings-hint">
                    Draft stores variables in SQLite. The linked path applies immediately for import, refresh, and export. Variable values still deploy when you stage and redeploy.
                </span>
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
                <span className="settings-hint">Values support @{'{Service.ATTR}'} cross-service references, {`{{secret.KEY}}`} app secrets, {`{{project.KEY}}`} project values, and {`{{draft.X}}`} identity expressions.</span>
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
                        disabled={!canLink}
                        title="Add a new variable that references another service, secret, or project value"
                    >
                        <Link2 size={14}/> Link
                    </button>
                </div>
                {autocomplete?.key === FIELD_NEW_VALUE && (
                    <VarAutocomplete
                        autocomplete={autocomplete}
                        linkTargets={linkTargets}
                        appSecrets={appSecrets}
                        projectVars={projectVars}
                        onSelectService={selectAutocompleteService}
                        onSelectAttr={selectAutocompleteAttr}
                        onSelectSecret={selectAutocompleteSecret}
                        onSelectProject={selectAutocompleteProject}
                    />
                )}
            </div>

            {linker?.mode === 'new' && renderLinkerPanel()}

            <div className="variables-list">
                {vars.length === 0 && (
                    <div className="variables-empty">No Draft variables yet.</div>
                )}
                {vars.map(v => {
                    const varIssues = referenceIssues.filter((issue) => issue.varKey === v.key);
                    const varBuildWarnings = buildWarnings.filter((w) => w.key === v.key);
                    return (
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
                            {varIssues.map((issue) => (
                                <div key={`${issue.token}:${issue.reason}`} className="var-preview var-preview--error">
                                    {issue.token}: {issue.reason}
                                </div>
                            ))}
                            {varBuildWarnings.map((w) => (
                                <div key={w.kind} className="var-build-warning">
                                    <AlertTriangle size={13} className="var-build-warning-icon"/>
                                    <div className="var-build-warning-body">
                                        <span>{w.message}</span>
                                        {w.kind === 'scope' ? (
                                            <button className="var-build-warning-action" onClick={() => toggleBuildArg(v)}>
                                                Enable build arg
                                            </button>
                                        ) : w.suggestion ? (
                                            <span className="var-build-warning-hint">
                                                Add <code>{w.suggestion}</code> to your Dockerfile{w.kind === 'arg_wrong_stage' ? ' build stage' : ''}.
                                            </span>
                                        ) : null}
                                    </div>
                                </div>
                            ))}
                            {previews[v.key]?.error && varIssues.length === 0 && (
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
                                    appSecrets={appSecrets}
                                    projectVars={projectVars}
                                    onSelectService={selectAutocompleteService}
                                    onSelectAttr={selectAutocompleteAttr}
                                    onSelectSecret={selectAutocompleteSecret}
                                    onSelectProject={selectAutocompleteProject}
                                />
                            )}
                            {linker?.mode === 'existing' && linker.localKey === v.key && renderLinkerPanel()}
                        </div>
                    </div>
                    );
                })}
            </div>

            <ProjectVarsSection vars={projectVars} serviceKeys={serviceVarKeys} loading={loadingProjectVars} />

            <RuntimeVarsSection />

            {hasStagedChanges && (
                <p className="variables-staged-hint">Staged variable changes will apply on the next deploy.</p>
            )}
        </div>
    );
}
