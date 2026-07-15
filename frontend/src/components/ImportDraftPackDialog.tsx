import {useCallback, useEffect, useState} from 'react';
import {ClipboardPaste, FileUp, FolderOpen, Loader2, Package} from 'lucide-react';
import Dialog from './Dialog';
import ConfigReport from './ConfigReport';
import DraftPackLayoutMap from './DraftPackLayoutMap';
import {useAppDialog} from './AppDialogProvider';
import {
    ImportDraftPack,
    ImportDraftPackJSON,
    ListEnvironments,
    PreviewDraftPackImport,
    PreviewDraftPackJSON,
    SelectDraftPackFile,
    SelectFolder,
} from '../../wailsjs/go/main/App';
import {draftpack, store} from '../../wailsjs/go/models';
import './ImportDraftPackDialog.css';

type Props = {
    /** When set, offer "into this project" as well as new project. */
    projectId?: number;
    environmentId?: number;
    onClose: () => void;
    onImported: (projectId: number, environmentId?: number) => void;
};

type SourceMode = 'file' | 'paste';

export default function ImportDraftPackDialog({projectId, environmentId, onClose, onImported}: Props) {
    const [sourceMode, setSourceMode] = useState<SourceMode>('file');
    const [path, setPath] = useState('');
    const [pasteJSON, setPasteJSON] = useState('');
    /** Last JSON text that successfully produced a preview (file path or paste body). */
    const [activeJSON, setActiveJSON] = useState<string | null>(null);
    const [preview, setPreview] = useState<draftpack.ImportPreview | null>(null);
    const [mode, setMode] = useState<'newProject' | 'intoProject'>(projectId ? 'intoProject' : 'newProject');
    const [projectName, setProjectName] = useState('');
    const [projectPath, setProjectPath] = useState('');
    const [targetEnvId, setTargetEnvId] = useState<number>(environmentId || 0);
    const [envs, setEnvs] = useState<store.Environment[]>([]);
    const [serviceRoots, setServiceRoots] = useState<Record<string, string>>({});
    const [bindPaths, setBindPaths] = useState<Record<string, string>>({});
    const [serviceLabels, setServiceLabels] = useState<Record<string, string>>({});
    const [hostPorts, setHostPorts] = useState<Record<string, string>>({});
    const [secretValues, setSecretValues] = useState<Record<string, string>>({});
    const [secretAppLinks, setSecretAppLinks] = useState<Record<string, string>>({});
    const [appSecretValues, setAppSecretValues] = useState<Record<string, string>>({});
    const [importAppSecrets, setImportAppSecrets] = useState(false);
    const [linkExistingAppSecrets, setLinkExistingAppSecrets] = useState(true);
    const [startAfter, setStartAfter] = useState(false);
    const [layoutMode, setLayoutMode] = useState<'auto' | 'preserve' | 'grid'>('auto');
    const [envImportMode, setEnvImportMode] = useState<'flatten' | 'recreate'>('flatten');
    const [selectedKeys, setSelectedKeys] = useState<Record<string, boolean>>({});
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [resultReport, setResultReport] = useState<draftpack.Report | null>(null);
    const [importedProjectId, setImportedProjectId] = useState<number | null>(null);
    const [importedEnvId, setImportedEnvId] = useState<number | undefined>(undefined);
    const {alert} = useAppDialog();

    const hasSource = sourceMode === 'file' ? !!path : !!activeJSON;

    const selectedServiceKeys = useCallback((): string[] => {
        const all = preview?.services ?? [];
        if (all.length === 0) return [];
        const keys = all.map((s) => s.key).filter((k) => selectedKeys[k] !== false);
        // If nothing explicitly selected, treat as all (initial state before seed).
        if (Object.keys(selectedKeys).length === 0) return all.map((s) => s.key);
        return keys;
    }, [preview, selectedKeys]);

    const finish = () => {
        if (importedProjectId != null) {
            onImported(importedProjectId, importedEnvId);
        }
        onClose();
    };

    const buildPreviewOptions = useCallback((): draftpack.PreviewOptions => {
        const keys = selectedServiceKeys();
        return draftpack.PreviewOptions.createFrom({
            mode,
            projectId: projectId || 0,
            environmentId: targetEnvId || environmentId || 0,
            projectName: projectName.trim(),
            projectPath: projectPath.trim(),
            serviceLabelOverrides: serviceLabels,
            hostPortOverrides: hostPorts,
            serviceKeys: keys,
            envImportMode: mode === 'intoProject' ? envImportMode : undefined,
            layoutMode,
        });
    }, [mode, projectId, targetEnvId, environmentId, projectName, projectPath, serviceLabels, hostPorts, envImportMode, layoutMode, selectedServiceKeys]);

    const runPreview = useCallback(async (opts: draftpack.PreviewOptions, filePath?: string, jsonText?: string) => {
        if (jsonText != null && jsonText.trim() !== '') {
            return PreviewDraftPackJSON(jsonText, opts);
        }
        if (filePath) {
            return PreviewDraftPackImport(filePath, opts);
        }
        throw new Error('No pack source');
    }, []);

    const applyPreviewSeed = async (pv: draftpack.ImportPreview, keepSelection = false) => {
        setPreview(pv);
        const name = pv.suggestedProjectName || pv.projectName || 'imported';
        setProjectName(name);

        if (!keepSelection) {
            const sel: Record<string, boolean> = {};
            for (const s of pv.services ?? []) {
                sel[s.key] = true;
            }
            setSelectedKeys(sel);
        }

        // Default recreate for multi-env packs into a project.
        if (pv.multiEnv && projectId && mode === 'intoProject') {
            setEnvImportMode((prev) => (prev === 'flatten' ? 'recreate' : prev));
        }

        const roots: Record<string, string> = {};
        for (const n of pv.needsServiceRoots ?? []) {
            if (n.hint) roots[n.serviceKey] = n.hint;
        }
        setServiceRoots(roots);

        const labels: Record<string, string> = {};
        const ports: Record<string, string> = {};
        for (const c of pv.collisions ?? []) {
            if (c.kind === 'service_label' && c.suggested) {
                labels[c.field] = c.suggested;
            }
            if (c.kind === 'host_port') {
                ports[c.field] = '';
            }
            if (c.kind === 'project_name' && c.suggested) {
                setProjectName(c.suggested);
            }
        }
        setServiceLabels(labels);
        setHostPorts(ports);

        // Auto-link secret keys that already exist as app secrets.
        const links: Record<string, string> = {};
        const existing = new Set(pv.existingAppSecrets ?? []);
        for (const key of pv.needsSecrets ?? []) {
            if (existing.has(key)) {
                links[key] = key;
            }
        }
        setSecretAppLinks(links);

        if (projectId) {
            const list = await ListEnvironments(projectId);
            setEnvs(list || []);
            if (!targetEnvId && list?.length) {
                const def = list.find((e) => e.isDefault) || list[0];
                setTargetEnvId(def.id);
            }
        }
    };

    // Re-check collisions when mode / target / identity fields change.
    useEffect(() => {
        if (!hasSource || resultReport) return;
        let cancelled = false;
        (async () => {
            try {
                const opts = buildPreviewOptions();
                const pv = await runPreview(
                    opts,
                    sourceMode === 'file' ? path : undefined,
                    sourceMode === 'paste' ? (activeJSON ?? undefined) : undefined,
                );
                if (!cancelled) setPreview(pv);
            } catch {
                /* keep last preview */
            }
        })();
        return () => { cancelled = true; };
    }, [hasSource, path, activeJSON, sourceMode, mode, targetEnvId, projectName, projectPath, serviceLabels, hostPorts, buildPreviewOptions, resultReport, runPreview]);

    const switchSourceMode = (next: SourceMode) => {
        if (next === sourceMode) return;
        setSourceMode(next);
        setError(null);
        setResultReport(null);
        setPreview(null);
        setPath('');
        setActiveJSON(null);
        // Keep pasteJSON when switching so users don't lose clipboard content.
        if (next === 'file') {
            // leave paste text; path is empty until pick
        }
    };

    const pickFile = async () => {
        setError(null);
        setResultReport(null);
        try {
            const p = await SelectDraftPackFile();
            if (!p) return;
            setPath(p);
            setActiveJSON(null);
            setSourceMode('file');
            setBusy(true);
            const opts = draftpack.PreviewOptions.createFrom({
                mode,
                projectId: projectId || 0,
                environmentId: targetEnvId || environmentId || 0,
            });
            const pv = await PreviewDraftPackImport(p, opts);
            await applyPreviewSeed(pv);
        } catch (e: any) {
            setPreview(null);
            setError(String(e?.message ?? e));
        } finally {
            setBusy(false);
        }
    };

    const parsePastedJSON = async () => {
        setError(null);
        setResultReport(null);
        const text = pasteJSON.trim();
        if (!text) {
            setError('Paste draft pack JSON first.');
            return;
        }
        setBusy(true);
        try {
            // Fast local sanity check so bad paste fails with a clear message.
            JSON.parse(text);
            const opts = draftpack.PreviewOptions.createFrom({
                mode,
                projectId: projectId || 0,
                environmentId: targetEnvId || environmentId || 0,
            });
            const pv = await PreviewDraftPackJSON(text, opts);
            setPath('');
            setActiveJSON(text);
            setSourceMode('paste');
            await applyPreviewSeed(pv);
        } catch (e: any) {
            setPreview(null);
            setActiveJSON(null);
            const msg = String(e?.message ?? e);
            setError(msg.includes('JSON') || msg.includes('json') || msg.includes('Unexpected')
                ? `Invalid JSON: ${msg}`
                : msg);
        } finally {
            setBusy(false);
        }
    };

    const pickProjectFolder = async () => {
        const dir = await SelectFolder();
        if (dir) setProjectPath(dir);
    };

    const pickBind = async (key: string) => {
        const dir = await SelectFolder();
        if (dir) setBindPaths((prev) => ({...prev, [key]: dir}));
    };

    const applySuggestion = (c: draftpack.Collision) => {
        if (c.kind === 'project_name' && c.suggested) {
            setProjectName(c.suggested);
        } else if (c.kind === 'service_label' && c.suggested) {
            setServiceLabels((prev) => ({...prev, [c.field]: c.suggested!}));
        } else if (c.kind === 'host_port') {
            setHostPorts((prev) => ({...prev, [c.field]: ''}));
        }
    };

    const doImport = async () => {
        if (!preview || !hasSource) return;
        setBusy(true);
        setError(null);
        try {
            const labels = {...serviceLabels};
            const ports = {...hostPorts};
            let name = projectName.trim();
            for (const c of preview.collisions ?? []) {
                if (c.kind === 'service_label' && !labels[c.field] && c.suggested) {
                    labels[c.field] = c.suggested;
                }
                if (c.kind === 'host_port' && ports[c.field] === undefined) {
                    ports[c.field] = '';
                }
                if (c.kind === 'project_name' && c.suggested && (name === c.current || !name)) {
                    name = c.suggested;
                }
            }

            const keys = selectedServiceKeys();
            if (keys.length === 0) {
                setError('Select at least one service to import.');
                setBusy(false);
                return;
            }

            const opts = draftpack.ImportOptions.createFrom({
                mode,
                projectName: name,
                projectPath: projectPath.trim(),
                projectId: projectId || 0,
                environmentId: targetEnvId || environmentId || 0,
                envImportMode: mode === 'intoProject' ? envImportMode : undefined,
                layoutMode,
                serviceKeys: keys,
                serviceLabelOverrides: labels,
                serviceRootOverrides: serviceRoots,
                bindPathOverrides: bindPaths,
                hostPortOverrides: ports,
                secretValues,
                secretAppLinks,
                appSecretValues,
                importAppSecrets,
                linkExistingAppSecrets,
                startAfter,
            });

            const res = sourceMode === 'paste' && activeJSON
                ? await ImportDraftPackJSON(activeJSON, opts)
                : await ImportDraftPack(path, opts);
            setResultReport(res.report);
            setImportedProjectId(res.projectId);
            setImportedEnvId(res.environmentIds?.[0]);
            if (res.startError) {
                void alert({
                    title: 'Pack imported, start incomplete',
                    message: res.startError,
                    detail: 'The import succeeded. Open the project and deploy individual services if needed.',
                });
            }
        } catch (e: any) {
            setError(String(e?.message ?? e));
            try {
                const opts = buildPreviewOptions();
                const pv = await runPreview(
                    opts,
                    sourceMode === 'file' ? path : undefined,
                    sourceMode === 'paste' ? (activeJSON ?? undefined) : undefined,
                );
                setPreview(pv);
            } catch { /* ignore */ }
        } finally {
            setBusy(false);
        }
    };

    const hasBlocking = !!preview?.hasBlockingCollision;
    const anyServiceSelected = selectedServiceKeys().length > 0;
    const canImport = !!preview && !busy && !resultReport && !hasBlocking && hasSource && anyServiceSelected && (
        mode === 'newProject'
            ? projectName.trim() !== '' && projectPath.trim() !== ''
            : mode === 'intoProject' && envImportMode === 'recreate'
                ? !!projectId
                : !!(projectId && (targetEnvId || environmentId))
    );

    const toggleService = (key: string) => {
        setSelectedKeys((prev) => ({...prev, [key]: prev[key] === false}));
    };

    const existingSecretSet = new Set(preview?.existingAppSecrets ?? []);

    const collisions = preview?.collisions ?? [];

    const footer = (
        <div className="dialog-footer-row">
            <button className="btn btn-ghost" onClick={resultReport ? finish : onClose} disabled={busy}>
                {resultReport ? 'Done' : 'Cancel'}
            </button>
            {!resultReport && (
                <button className="btn btn-primary" onClick={doImport} disabled={!canImport}>
                    {busy ? <Loader2 size={14} className="spin"/> : <Package size={14}/>}
                    {startAfter ? 'Import & start' : 'Import pack'}
                </button>
            )}
        </div>
    );

    return (
        <Dialog title="Import Draft pack" onClose={resultReport ? finish : onClose} footer={footer} wide>
            <div className="draftpack-import">
                <p className="draftpack-import-intro">
                    Open a <strong>.draftpack</strong> file or paste the JSON you copied from export.
                    Fix any name or path collisions below, then import — Draft will not reject the pack for renamable clashes.
                </p>

                <div className="draftpack-source-tabs">
                    <button
                        type="button"
                        className={`draftpack-source-tab${sourceMode === 'file' ? ' is-active' : ''}`}
                        onClick={() => switchSourceMode('file')}
                        disabled={busy || !!resultReport}
                    >
                        <FileUp size={14}/> From file
                    </button>
                    <button
                        type="button"
                        className={`draftpack-source-tab${sourceMode === 'paste' ? ' is-active' : ''}`}
                        onClick={() => switchSourceMode('paste')}
                        disabled={busy || !!resultReport}
                    >
                        <ClipboardPaste size={14}/> Paste JSON
                    </button>
                </div>

                {sourceMode === 'file' && (
                    <div className="draftpack-import-file">
                        <button className="btn btn-secondary" onClick={pickFile} disabled={busy || !!resultReport}>
                            <FileUp size={14}/> Choose pack…
                        </button>
                        {path && <code className="draftpack-path" title={path}>{path}</code>}
                    </div>
                )}

                {sourceMode === 'paste' && !resultReport && (
                    <div className="draftpack-paste">
                        <textarea
                            className="input draftpack-paste-area"
                            value={pasteJSON}
                            onChange={(e) => setPasteJSON(e.target.value)}
                            placeholder='Paste draft pack JSON here… (same content as “Copy JSON” on export)'
                            spellCheck={false}
                            disabled={busy}
                            rows={10}
                        />
                        <div className="draftpack-paste-actions">
                            <button
                                type="button"
                                className="btn btn-secondary"
                                onClick={parsePastedJSON}
                                disabled={busy || !pasteJSON.trim()}
                            >
                                {busy ? <Loader2 size={14} className="spin"/> : <ClipboardPaste size={14}/>}
                                Parse pack
                            </button>
                            {activeJSON && (
                                <span className="draftpack-paste-ready">JSON ready · {preview?.services?.length ?? 0} service{(preview?.services?.length ?? 0) === 1 ? '' : 's'}</span>
                            )}
                        </div>
                    </div>
                )}

                {error && <p className="form-error">{error}</p>}

                {preview && !resultReport && (
                    <>
                        <div className="draftpack-summary">
                            <span className="draftpack-scope">{preview.packScope}</span>
                            <span>{preview.services?.length ?? 0} service{(preview.services?.length ?? 0) === 1 ? '' : 's'}</span>
                            {(preview.environments?.length ?? 0) > 0 && (
                                <span>{preview.environments.length} environment{(preview.environments.length === 1) ? '' : 's'}</span>
                            )}
                            {sourceMode === 'paste' && <span className="draftpack-source-badge">from clipboard</span>}
                            {preview.contentHash && (
                                <span
                                    className={`draftpack-source-badge${preview.contentHashOk === false ? ' is-bad' : ''}`}
                                    title={preview.contentHash}
                                >
                                    {preview.contentHashOk === false ? 'hash mismatch' : 'integrity ok'}
                                </span>
                            )}
                        </div>

                        <div className="draftpack-remap">
                            <h4>Services to import</h4>
                            <p className="draftpack-remap-hint">Uncheck services you do not want on this machine.</p>
                            <ul className="draftpack-services draftpack-services-select">
                                {(preview.services ?? []).map((s) => (
                                    <li key={s.key}>
                                        <label className="draftpack-check draftpack-svc-check">
                                            <input
                                                type="checkbox"
                                                checked={selectedKeys[s.key] !== false}
                                                onChange={() => toggleService(s.key)}
                                            />
                                            <span className="draftpack-svc-name">
                                                {serviceLabels[s.key] || s.label}
                                            </span>
                                        </label>
                                        <span className={`draftpack-badge badge-${s.mode}`}>{s.mode}</span>
                                        {s.port && <span className="draftpack-svc-detail">:{s.port}</span>}
                                        {s.image && <code className="draftpack-svc-detail">{s.image}</code>}
                                    </li>
                                ))}
                            </ul>
                        </div>

                        {projectId ? (
                            <div className="draftpack-mode">
                                <label className="draftpack-radio">
                                    <input type="radio" checked={mode === 'newProject'} onChange={() => setMode('newProject')}/>
                                    New project
                                </label>
                                <label className="draftpack-radio">
                                    <input type="radio" checked={mode === 'intoProject'} onChange={() => setMode('intoProject')}/>
                                    Into this project
                                </label>
                            </div>
                        ) : null}

                        {mode === 'newProject' && (
                            <div className="draftpack-fields">
                                <label className="draftpack-field">
                                    <span>Project name</span>
                                    <input className="input" value={projectName} onChange={(e) => setProjectName(e.target.value)}/>
                                </label>
                                <label className="draftpack-field">
                                    <span>Project folder</span>
                                    <div className="draftpack-path-row">
                                        <input className="input" value={projectPath} onChange={(e) => setProjectPath(e.target.value)} placeholder="Choose a local folder…"/>
                                        <button type="button" className="btn btn-secondary" onClick={pickProjectFolder}>
                                            <FolderOpen size={14}/>
                                        </button>
                                    </div>
                                </label>
                            </div>
                        )}

                        {mode === 'intoProject' && projectId && (
                            <div className="draftpack-fields">
                                {preview.multiEnv && preview.canRecreateEnvs && (
                                    <div className="draftpack-mode">
                                        <label className="draftpack-radio">
                                            <input
                                                type="radio"
                                                checked={envImportMode === 'flatten'}
                                                onChange={() => setEnvImportMode('flatten')}
                                            />
                                            Flatten into one environment
                                        </label>
                                        <label className="draftpack-radio">
                                            <input
                                                type="radio"
                                                checked={envImportMode === 'recreate'}
                                                onChange={() => setEnvImportMode('recreate')}
                                            />
                                            Recreate pack environments
                                        </label>
                                    </div>
                                )}
                                {envImportMode === 'flatten' && (
                                    <label className="draftpack-field">
                                        <span>Target environment</span>
                                        <select
                                            className="input"
                                            value={targetEnvId}
                                            onChange={(e) => setTargetEnvId(Number(e.target.value))}
                                        >
                                            {envs.map((env) => (
                                                <option key={env.id} value={env.id}>{env.name}</option>
                                            ))}
                                        </select>
                                    </label>
                                )}
                            </div>
                        )}

                        {preview.layout && (
                            <DraftPackLayoutMap
                                layout={preview.layout}
                                layoutMode={layoutMode}
                                onLayoutModeChange={setLayoutMode}
                            />
                        )}

                        {collisions.length > 0 && (
                            <div className="draftpack-remap draftpack-collisions">
                                <h4>Name &amp; unique field conflicts</h4>
                                <p className="draftpack-remap-hint">
                                    These clash with something already on this machine. Edit the values or use the suggested fix.
                                    Import will auto-apply suggestions if you leave them blank (except a folder that is already a project).
                                </p>
                                {collisions.map((c) => (
                                    <div key={`${c.kind}:${c.field}`} className={`draftpack-collision${c.blocking ? ' is-blocking' : ''}`}>
                                        <div className="draftpack-collision-msg">{c.message}</div>
                                        {c.kind === 'project_name' && (
                                            <div className="draftpack-path-row">
                                                <input
                                                    className="input"
                                                    value={projectName}
                                                    onChange={(e) => setProjectName(e.target.value)}
                                                />
                                                {c.suggested && (
                                                    <button type="button" className="btn btn-secondary" onClick={() => applySuggestion(c)}>
                                                        Use {c.suggested}
                                                    </button>
                                                )}
                                            </div>
                                        )}
                                        {c.kind === 'project_path' && (
                                            <div className="draftpack-path-row">
                                                <input className="input" value={projectPath} readOnly placeholder="Choose a different folder…"/>
                                                <button type="button" className="btn btn-secondary" onClick={pickProjectFolder}>
                                                    <FolderOpen size={14}/> Choose folder
                                                </button>
                                            </div>
                                        )}
                                        {c.kind === 'service_label' && (
                                            <div className="draftpack-path-row">
                                                <input
                                                    className="input"
                                                    value={serviceLabels[c.field] ?? c.suggested ?? c.current}
                                                    onChange={(e) => setServiceLabels((p) => ({...p, [c.field]: e.target.value}))}
                                                    placeholder={c.suggested || c.current}
                                                />
                                                {c.suggested && (
                                                    <button type="button" className="btn btn-secondary" onClick={() => applySuggestion(c)}>
                                                        Use {c.suggested}
                                                    </button>
                                                )}
                                            </div>
                                        )}
                                        {c.kind === 'host_port' && (
                                            <div className="draftpack-path-row">
                                                <input
                                                    className="input"
                                                    value={hostPorts[c.field] ?? ''}
                                                    onChange={(e) => setHostPorts((p) => ({...p, [c.field]: e.target.value}))}
                                                    placeholder="Leave empty to auto-assign"
                                                />
                                                <button type="button" className="btn btn-secondary" onClick={() => applySuggestion(c)}>
                                                    Clear port
                                                </button>
                                            </div>
                                        )}
                                    </div>
                                ))}
                            </div>
                        )}

                        {(preview.needsServiceRoots?.length ?? 0) > 0 && (
                            <div className="draftpack-remap">
                                <h4>Service roots</h4>
                                <p className="draftpack-remap-hint">Relative paths are under the project folder.</p>
                                {preview.needsServiceRoots!.map((n) => (
                                    <label key={n.serviceKey} className="draftpack-field">
                                        <span>{n.label}{n.hint ? ` (hint: ${n.hint})` : ''}</span>
                                        <input
                                            className="input"
                                            value={serviceRoots[n.serviceKey] || ''}
                                            onChange={(e) => setServiceRoots((p) => ({...p, [n.serviceKey]: e.target.value}))}
                                            placeholder={n.hint || 'services/api'}
                                        />
                                    </label>
                                ))}
                            </div>
                        )}

                        {(preview.needsBinds?.length ?? 0) > 0 && (
                            <div className="draftpack-remap">
                                <h4>Bind mounts</h4>
                                <p className="draftpack-remap-hint">Optional — skip to leave the mount unset.</p>
                                {preview.needsBinds!.map((b) => {
                                    const key = `${b.serviceKey}|${b.containerPath}`;
                                    return (
                                        <label key={key} className="draftpack-field">
                                            <span>{b.label} → {b.containerPath}</span>
                                            <div className="draftpack-path-row">
                                                <input
                                                    className="input"
                                                    value={bindPaths[key] || ''}
                                                    onChange={(e) => setBindPaths((p) => ({...p, [key]: e.target.value}))}
                                                    placeholder={b.originalHost || 'Host folder…'}
                                                />
                                                <button type="button" className="btn btn-secondary" onClick={() => pickBind(key)}>
                                                    <FolderOpen size={14}/>
                                                </button>
                                            </div>
                                        </label>
                                    );
                                })}
                            </div>
                        )}

                        {(preview.needsSecrets?.length ?? 0) > 0 && (
                            <div className="draftpack-remap">
                                <h4>Secrets</h4>
                                <p className="draftpack-remap-hint">
                                    Values were omitted from the pack. Link to an existing app secret or paste a value.
                                </p>
                                {preview.needsSecrets!.map((key) => {
                                    const canLink = existingSecretSet.has(key);
                                    const linked = !!secretAppLinks[key];
                                    return (
                                        <div key={key} className="draftpack-secret-row">
                                            <div className="draftpack-secret-key">{key}</div>
                                            {canLink && (
                                                <label className="draftpack-check">
                                                    <input
                                                        type="checkbox"
                                                        checked={linked}
                                                        onChange={(e) => {
                                                            if (e.target.checked) {
                                                                setSecretAppLinks((p) => ({...p, [key]: key}));
                                                                setSecretValues((p) => {
                                                                    const next = {...p};
                                                                    delete next[key];
                                                                    return next;
                                                                });
                                                            } else {
                                                                setSecretAppLinks((p) => {
                                                                    const next = {...p};
                                                                    delete next[key];
                                                                    return next;
                                                                });
                                                            }
                                                        }}
                                                    />
                                                    <span>Use existing app secret <code>{key}</code></span>
                                                </label>
                                            )}
                                            {!linked && (
                                                <input
                                                    className="input"
                                                    type="password"
                                                    value={secretValues[key] || ''}
                                                    onChange={(e) => setSecretValues((p) => ({...p, [key]: e.target.value}))}
                                                    placeholder={canLink ? 'Or paste a value…' : 'Optional'}
                                                    autoComplete="off"
                                                />
                                            )}
                                        </div>
                                    );
                                })}
                            </div>
                        )}

                        {((preview.needsAppSecrets?.length ?? 0) > 0 || (preview.existingAppSecrets?.length ?? 0) > 0) && (
                            <div className="draftpack-remap">
                                <h4>App secrets</h4>
                                {(preview.existingAppSecrets?.length ?? 0) > 0 && (
                                    <label className="draftpack-check">
                                        <input
                                            type="checkbox"
                                            checked={linkExistingAppSecrets}
                                            onChange={(e) => setLinkExistingAppSecrets(e.target.checked)}
                                        />
                                        <span>
                                            Keep existing app secrets ({preview.existingAppSecrets!.join(', ')})
                                        </span>
                                    </label>
                                )}
                                {(preview.needsAppSecrets?.length ?? 0) > 0 && (
                                    <>
                                        <label className="draftpack-check">
                                            <input type="checkbox" checked={importAppSecrets} onChange={(e) => setImportAppSecrets(e.target.checked)}/>
                                            <span>Write missing app secrets into this Draft install</span>
                                        </label>
                                        {importAppSecrets && preview.needsAppSecrets!.map((key) => (
                                            <label key={key} className="draftpack-field">
                                                <span>{key}{existingSecretSet.has(key) ? ' (exists)' : ''}</span>
                                                <input
                                                    className="input"
                                                    type="password"
                                                    value={appSecretValues[key] || ''}
                                                    onChange={(e) => setAppSecretValues((p) => ({...p, [key]: e.target.value}))}
                                                    placeholder="Value"
                                                    autoComplete="off"
                                                />
                                            </label>
                                        ))}
                                    </>
                                )}
                            </div>
                        )}

                        <label className="draftpack-check draftpack-start-after">
                            <input
                                type="checkbox"
                                checked={startAfter}
                                onChange={(e) => setStartAfter(e.target.checked)}
                                disabled={busy}
                            />
                            <span>Start services after import</span>
                        </label>

                        <div className="draftpack-report">
                            <h4>Pack report</h4>
                            <ConfigReport report={preview.report}/>
                        </div>
                    </>
                )}

                {resultReport && (
                    <div className="draftpack-report">
                        <h4>Import complete</h4>
                        <ConfigReport report={resultReport}/>
                    </div>
                )}
            </div>
        </Dialog>
    );
}
