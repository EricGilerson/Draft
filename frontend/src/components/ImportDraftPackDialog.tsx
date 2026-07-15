import {useState} from 'react';
import {FileUp, FolderOpen, Loader2, Package} from 'lucide-react';
import Dialog from './Dialog';
import ConfigReport from './ConfigReport';
import {
    ImportDraftPack,
    ListEnvironments,
    PreviewDraftPackImport,
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

export default function ImportDraftPackDialog({projectId, environmentId, onClose, onImported}: Props) {
    const [path, setPath] = useState('');
    const [preview, setPreview] = useState<draftpack.ImportPreview | null>(null);
    const [mode, setMode] = useState<'newProject' | 'intoProject'>(projectId ? 'intoProject' : 'newProject');
    const [projectName, setProjectName] = useState('');
    const [projectPath, setProjectPath] = useState('');
    const [targetEnvId, setTargetEnvId] = useState<number>(environmentId || 0);
    const [envs, setEnvs] = useState<store.Environment[]>([]);
    const [serviceRoots, setServiceRoots] = useState<Record<string, string>>({});
    const [bindPaths, setBindPaths] = useState<Record<string, string>>({});
    const [secretValues, setSecretValues] = useState<Record<string, string>>({});
    const [appSecretValues, setAppSecretValues] = useState<Record<string, string>>({});
    const [importAppSecrets, setImportAppSecrets] = useState(false);
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [resultReport, setResultReport] = useState<draftpack.Report | null>(null);
    const [importedProjectId, setImportedProjectId] = useState<number | null>(null);
    const [importedEnvId, setImportedEnvId] = useState<number | undefined>(undefined);

    const finish = () => {
        if (importedProjectId != null) {
            onImported(importedProjectId, importedEnvId);
        }
        onClose();
    };

    const pickFile = async () => {
        setError(null);
        setResultReport(null);
        try {
            const p = await SelectDraftPackFile();
            if (!p) return;
            setPath(p);
            setBusy(true);
            const pv = await PreviewDraftPackImport(p);
            setPreview(pv);
            setProjectName(pv.projectName || 'imported');
            // Prefill service root hints
            const roots: Record<string, string> = {};
            for (const n of pv.needsServiceRoots ?? []) {
                if (n.hint) roots[n.serviceKey] = n.hint;
            }
            setServiceRoots(roots);
            if (projectId) {
                const list = await ListEnvironments(projectId);
                setEnvs(list || []);
                if (!targetEnvId && list?.length) {
                    const def = list.find((e) => e.isDefault) || list[0];
                    setTargetEnvId(def.id);
                }
            }
        } catch (e: any) {
            setPreview(null);
            setError(String(e?.message ?? e));
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

    const doImport = async () => {
        if (!path || !preview) return;
        setBusy(true);
        setError(null);
        try {
            const opts = draftpack.ImportOptions.createFrom({
                mode,
                projectName: projectName.trim(),
                projectPath: projectPath.trim(),
                projectId: projectId || 0,
                environmentId: targetEnvId || environmentId || 0,
                serviceRootOverrides: serviceRoots,
                bindPathOverrides: bindPaths,
                secretValues,
                appSecretValues,
                importAppSecrets,
                startAfter: false,
            });
            const res = await ImportDraftPack(path, opts);
            setResultReport(res.report);
            setImportedProjectId(res.projectId);
            setImportedEnvId(res.environmentIds?.[0]);
        } catch (e: any) {
            setError(String(e?.message ?? e));
        } finally {
            setBusy(false);
        }
    };

    const canImport = !!preview && !busy && !resultReport && (
        mode === 'newProject'
            ? projectName.trim() !== '' && projectPath.trim() !== ''
            : !!(projectId && (targetEnvId || environmentId))
    );

    const footer = (
        <div className="dialog-footer-row">
            <button className="btn btn-ghost" onClick={resultReport ? finish : onClose} disabled={busy}>
                {resultReport ? 'Done' : 'Cancel'}
            </button>
            {!resultReport && (
                <button className="btn btn-primary" onClick={doImport} disabled={!canImport}>
                    {busy ? <Loader2 size={14} className="spin"/> : <Package size={14}/>}
                    Import pack
                </button>
            )}
        </div>
    );

    return (
        <Dialog title="Import Draft pack" onClose={resultReport ? finish : onClose} footer={footer} wide>
            <div className="draftpack-import">
                <p className="draftpack-import-intro">
                    Open a <strong>.draftpack</strong> shared from another Draft install. You will remap the project
                    folder and any machine-local paths before services are created.
                </p>

                <div className="draftpack-import-file">
                    <button className="btn btn-secondary" onClick={pickFile} disabled={busy}>
                        <FileUp size={14}/> Choose pack…
                    </button>
                    {path && <code className="draftpack-path" title={path}>{path}</code>}
                </div>

                {error && <p className="form-error">{error}</p>}

                {preview && !resultReport && (
                    <>
                        <div className="draftpack-summary">
                            <span className="draftpack-scope">{preview.packScope}</span>
                            <span>{preview.services?.length ?? 0} service{(preview.services?.length ?? 0) === 1 ? '' : 's'}</span>
                            {(preview.environments?.length ?? 0) > 0 && (
                                <span>{preview.environments.length} environment{(preview.environments.length === 1) ? '' : 's'}</span>
                            )}
                        </div>

                        <ul className="draftpack-services">
                            {(preview.services ?? []).map((s) => (
                                <li key={s.key}>
                                    <span className="draftpack-svc-name">{s.label}</span>
                                    <span className={`draftpack-badge badge-${s.mode}`}>{s.mode}</span>
                                    {s.port && <span className="draftpack-svc-detail">:{s.port}</span>}
                                    {s.image && <code className="draftpack-svc-detail">{s.image}</code>}
                                </li>
                            ))}
                        </ul>

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

                        {(preview.needsServiceRoots?.length ?? 0) > 0 && (
                            <div className="draftpack-remap">
                                <h4>Service roots</h4>
                                <p className="draftpack-remap-hint">Relative paths are under the project folder. Absolute paths are accepted if inside the project.</p>
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
                                <p className="draftpack-remap-hint">Values were omitted from the pack. Fill now or later in Variables.</p>
                                {preview.needsSecrets!.map((key) => (
                                    <label key={key} className="draftpack-field">
                                        <span>{key}</span>
                                        <input
                                            className="input"
                                            type="password"
                                            value={secretValues[key] || ''}
                                            onChange={(e) => setSecretValues((p) => ({...p, [key]: e.target.value}))}
                                            placeholder="Optional"
                                            autoComplete="off"
                                        />
                                    </label>
                                ))}
                            </div>
                        )}

                        {(preview.needsAppSecrets?.length ?? 0) > 0 && (
                            <div className="draftpack-remap">
                                <h4>App secrets</h4>
                                <label className="draftpack-check">
                                    <input type="checkbox" checked={importAppSecrets} onChange={(e) => setImportAppSecrets(e.target.checked)}/>
                                    <span>Write app secrets into this Draft install</span>
                                </label>
                                {importAppSecrets && preview.needsAppSecrets!.map((key) => (
                                    <label key={key} className="draftpack-field">
                                        <span>{key}</span>
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
                            </div>
                        )}

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
