import {useEffect, useState} from 'react';
import {Check, Copy, Download, Loader2} from 'lucide-react';
import Dialog from './Dialog';
import ConfigReport from './ConfigReport';
import {ExportConfig, ExportProjectConfig, ExportConfigToPath, SelectFolder} from '../../wailsjs/go/main/App';
import {deploy} from '../../wailsjs/go/models';
import './ExportConfigDialog.css';

type Props = {
    // Exactly one of nodeId (single service) or projectId (whole project).
    nodeId?: string;
    projectId?: number;
    label: string;
    onClose: () => void;
};

const FORMATS: {id: string; label: string}[] = [
    {id: 'compose', label: 'Docker Compose'},
    {id: 'cloudrun', label: 'Cloud Run'},
    {id: 'ecs', label: 'AWS ECS'},
    {id: 'containerapps', label: 'Container Apps'},
];

export default function ExportConfigDialog({nodeId, projectId, label, onClose}: Props) {
    const [format, setFormat] = useState('compose');
    const [result, setResult] = useState<deploy.ExportResult | null>(null);
    const [activeFile, setActiveFile] = useState(0);
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [copied, setCopied] = useState(false);
    const [saved, setSaved] = useState<string | null>(null);

    useEffect(() => {
        let cancelled = false;
        (async () => {
            setBusy(true);
            setError(null);
            setSaved(null);
            try {
                const res = nodeId
                    ? await ExportConfig(nodeId, format)
                    : await ExportProjectConfig(projectId as number, format);
                if (!cancelled) {
                    setResult(res);
                    setActiveFile(0);
                }
            } catch (e: any) {
                if (!cancelled) {
                    setResult(null);
                    setError(String(e?.message ?? e));
                }
            } finally {
                if (!cancelled) setBusy(false);
            }
        })();
        return () => {
            cancelled = true;
        };
    }, [format, nodeId, projectId]);

    const files = result?.files ?? [];
    const current = files[activeFile];

    const copy = async () => {
        if (!current) return;
        await navigator.clipboard.writeText(current.content);
        setCopied(true);
        setTimeout(() => setCopied(false), 1500);
    };

    // Save-to-folder is offered for single-service exports (project export is
    // copy-only, since it can span several files across formats).
    const save = async () => {
        if (!nodeId) return;
        try {
            const dir = await SelectFolder();
            if (!dir) return;
            setBusy(true);
            await ExportConfigToPath(nodeId, format, dir);
            setSaved(dir);
        } catch (e: any) {
            setError(String(e?.message ?? e));
        } finally {
            setBusy(false);
        }
    };

    const footer = (
        <div className="dialog-footer-row">
            <button className="btn btn-ghost" onClick={onClose}>Close</button>
            <button className="btn btn-secondary" onClick={copy} disabled={!current}>
                {copied ? <Check size={14}/> : <Copy size={14}/>} {copied ? 'Copied' : 'Copy'}
            </button>
            {nodeId && (
                <button className="btn btn-primary" onClick={save} disabled={!current || busy}>
                    {busy ? <Loader2 size={14} className="spin"/> : <Download size={14}/>} Save to folder…
                </button>
            )}
        </div>
    );

    return (
        <Dialog title={`Export ${label} to cloud config`} onClose={onClose} footer={footer}>
            <div className="export-config">
                <div className="export-config-formats">
                    {FORMATS.map((f) => (
                        <button
                            key={f.id}
                            className={`export-config-format ${format === f.id ? 'active' : ''}`}
                            onClick={() => setFormat(f.id)}
                        >
                            {f.label}
                        </button>
                    ))}
                </div>

                {error && <p className="form-error">{error}</p>}
                {saved && <p className="export-config-saved">Saved to {saved}</p>}

                {busy && !result && <p className="export-config-loading"><Loader2 size={14} className="spin"/> Generating…</p>}

                {files.length > 0 && (
                    <>
                        {files.length > 1 && (
                            <div className="export-config-tabs">
                                {files.map((f, i) => (
                                    <button
                                        key={f.name}
                                        className={`export-config-tab ${i === activeFile ? 'active' : ''}`}
                                        onClick={() => setActiveFile(i)}
                                    >
                                        {f.name}
                                    </button>
                                ))}
                            </div>
                        )}
                        <pre className="export-config-code"><code>{current?.content}</code></pre>
                    </>
                )}

                {result && (
                    <div className="export-config-report">
                        <h4>Fidelity report</h4>
                        <ConfigReport report={result.report}/>
                    </div>
                )}
            </div>
        </Dialog>
    );
}
