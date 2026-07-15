import {useEffect, useState} from 'react';
import {Check, Copy, Download, Loader2, Package} from 'lucide-react';
import Dialog from './Dialog';
import ConfigReport from './ConfigReport';
import {
    DefaultDraftPackExportOptions,
    ExportDraftPackEnvironment,
    ExportDraftPackProject,
    ExportDraftPackService,
    ExportDraftPackToPath,
    SaveDraftPackFile,
} from '../../wailsjs/go/main/App';
import {draftpack} from '../../wailsjs/go/models';
import './ExportDraftPackDialog.css';

export type DraftPackExportScope = 'service' | 'environment' | 'project';

type Props = {
    scope: DraftPackExportScope;
    label: string;
    nodeId?: string;
    projectId?: number;
    environmentId?: number;
    onClose: () => void;
};

function defaultOpts(): draftpack.ExportOptions {
    return draftpack.ExportOptions.createFrom({
        includeSecretValues: false,
        includeAppSecrets: false,
        includeBindHostPaths: false,
        includeServiceRoots: true,
        includeProjectEnvVars: true,
        includeSandboxProfiles: false,
        includeCanvasLayout: true,
        includeGitSettings: true,
        includeSourceConfig: true,
    });
}

export default function ExportDraftPackDialog({
    scope, label, nodeId, projectId, environmentId, onClose,
}: Props) {
    const [opts, setOpts] = useState<draftpack.ExportOptions>(defaultOpts());
    const [result, setResult] = useState<draftpack.ExportResult | null>(null);
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [copied, setCopied] = useState(false);
    const [saved, setSaved] = useState<string | null>(null);

    useEffect(() => {
        DefaultDraftPackExportOptions()
            .then((o) => setOpts(o))
            .catch(() => {/* keep local defaults */});
    }, []);

    useEffect(() => {
        let cancelled = false;
        (async () => {
            setBusy(true);
            setError(null);
            setSaved(null);
            try {
                let res: draftpack.ExportResult;
                if (scope === 'service' && nodeId) {
                    res = await ExportDraftPackService(nodeId, opts);
                } else if (scope === 'environment' && environmentId) {
                    res = await ExportDraftPackEnvironment(environmentId, opts);
                } else if (scope === 'project' && projectId) {
                    res = await ExportDraftPackProject(projectId, opts);
                } else {
                    throw new Error('Missing export target');
                }
                if (!cancelled) setResult(res);
            } catch (e: any) {
                if (!cancelled) {
                    setResult(null);
                    setError(String(e?.message ?? e));
                }
            } finally {
                if (!cancelled) setBusy(false);
            }
        })();
        return () => { cancelled = true; };
    }, [scope, nodeId, projectId, environmentId, opts]);

    const toggle = (key: keyof draftpack.ExportOptions) => {
        setOpts((prev) => {
            const next = draftpack.ExportOptions.createFrom({...prev});
            (next as any)[key] = !(prev as any)[key];
            return next;
        });
    };

    const copy = async () => {
        if (!result?.json) return;
        await navigator.clipboard.writeText(result.json);
        setCopied(true);
        setTimeout(() => setCopied(false), 1500);
    };

    const save = async () => {
        try {
            const path = await SaveDraftPackFile(result?.fileName || 'pack.draftpack');
            if (!path) return;
            setBusy(true);
            setError(null);
            const res = await ExportDraftPackToPath(
                scope,
                nodeId || '',
                projectId || 0,
                environmentId || 0,
                opts,
                path,
            );
            setSaved(res.path || path);
            if (res) setResult(res);
        } catch (e: any) {
            setError(String(e?.message ?? e));
        } finally {
            setBusy(false);
        }
    };

    const footer = (
        <div className="dialog-footer-row">
            <button className="btn btn-ghost" onClick={onClose}>Close</button>
            <button className="btn btn-secondary" onClick={copy} disabled={!result?.json}>
                {copied ? <Check size={14}/> : <Copy size={14}/>} {copied ? 'Copied' : 'Copy JSON'}
            </button>
            <button className="btn btn-primary" onClick={save} disabled={!result || busy}>
                {busy ? <Loader2 size={14} className="spin"/> : <Download size={14}/>} Save pack…
            </button>
        </div>
    );

    const scopeLabel =
        scope === 'service' ? 'service' :
        scope === 'environment' ? 'environment' : 'project';

    return (
        <Dialog title={`Export ${label} as Draft pack`} onClose={onClose} footer={footer} wide>
            <div className="draftpack-export">
                <p className="draftpack-export-intro">
                    Share this <strong>{scopeLabel}</strong> with another machine. Config only — no containers,
                    routes, or deployment history. Absolute paths are stripped; secrets are empty unless you opt in.
                </p>

                <div className="draftpack-options">
                    <h4>Include</h4>
                    <label className="draftpack-check">
                        <input type="checkbox" checked={!!opts.includeServiceRoots} onChange={() => toggle('includeServiceRoots')}/>
                        <span>Project-relative service roots</span>
                        <em>Recommended</em>
                    </label>
                    <label className="draftpack-check">
                        <input type="checkbox" checked={!!opts.includeProjectEnvVars} onChange={() => toggle('includeProjectEnvVars')}/>
                        <span>Project values</span>
                    </label>
                    <label className="draftpack-check">
                        <input type="checkbox" checked={!!opts.includeGitSettings} onChange={() => toggle('includeGitSettings')}/>
                        <span>Git branch &amp; deploy triggers</span>
                    </label>
                    <label className="draftpack-check">
                        <input type="checkbox" checked={!!opts.includeCanvasLayout} onChange={() => toggle('includeCanvasLayout')}/>
                        <span>Canvas layout</span>
                    </label>
                    <label className="draftpack-check">
                        <input type="checkbox" checked={!!opts.includeSourceConfig} onChange={() => toggle('includeSourceConfig')}/>
                        <span>Original cloud-config documents</span>
                    </label>
                    {scope === 'project' && (
                        <label className="draftpack-check">
                            <input type="checkbox" checked={!!opts.includeSandboxProfiles} onChange={() => toggle('includeSandboxProfiles')}/>
                            <span>Sandbox profiles</span>
                        </label>
                    )}

                    <h4 className="draftpack-options-warn">Sensitive / machine-local</h4>
                    <label className="draftpack-check warn">
                        <input type="checkbox" checked={!!opts.includeSecretValues} onChange={() => toggle('includeSecretValues')}/>
                        <span>Secret env values</span>
                        <em>Off by default</em>
                    </label>
                    <label className="draftpack-check warn">
                        <input type="checkbox" checked={!!opts.includeAppSecrets} onChange={() => toggle('includeAppSecrets')}/>
                        <span>Referenced app secret values</span>
                        <em>Off by default</em>
                    </label>
                    <label className="draftpack-check warn">
                        <input type="checkbox" checked={!!opts.includeBindHostPaths} onChange={() => toggle('includeBindHostPaths')}/>
                        <span>Bind-mount host paths</span>
                        <em>Off by default</em>
                    </label>
                </div>

                {error && <p className="form-error">{error}</p>}
                {saved && <p className="draftpack-saved"><Package size={14}/> Saved to {saved}</p>}
                {busy && !result && (
                    <p className="draftpack-loading"><Loader2 size={14} className="spin"/> Building pack…</p>
                )}

                {result && (
                    <>
                        <div className="draftpack-meta">
                            <code>{result.fileName || 'pack.draftpack'}</code>
                            <span>{result.json ? `${Math.round(result.json.length / 1024)} KB` : ''}</span>
                        </div>
                        <div className="draftpack-report">
                            <h4>Export report</h4>
                            <ConfigReport report={result.report}/>
                        </div>
                    </>
                )}
            </div>
        </Dialog>
    );
}
