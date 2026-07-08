import {useState} from 'react';
import {FileUp, Loader2} from 'lucide-react';
import Dialog from './Dialog';
import ConfigReport from './ConfigReport';
import {
    ImportConfigPreview,
    ImportConfigAsProject,
    ImportConfigIntoProject,
    SelectFile,
} from '../../wailsjs/go/main/App';
import {deploy, store} from '../../wailsjs/go/models';
import './ImportConfigDialog.css';

type Props =
    | {
          mode: 'project';
          onClose: () => void;
          onImported: (projectId: number) => void;
      }
    | {
          mode: 'canvas';
          projectId: number;
          position: {x: number; y: number};
          onClose: () => void;
          onImported: (nodes: store.CanvasNode[]) => void;
      };

const FORMAT_LABELS: Record<string, string> = {
    compose: 'Docker Compose',
    cloudrun: 'Google Cloud Run',
    ecs: 'AWS ECS',
    containerapps: 'Azure Container Apps',
};

export default function ImportConfigDialog(props: Props) {
    const [path, setPath] = useState('');
    const [preview, setPreview] = useState<deploy.ImportPreview | null>(null);
    const [projectName, setProjectName] = useState('');
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState<string | null>(null);

    const pickFile = async () => {
        setError(null);
        try {
            const p = await SelectFile('Select a cloud config file', '');
            if (!p) return;
            setPath(p);
            setBusy(true);
            const pv = await ImportConfigPreview(p);
            setPreview(pv);
            // Default project name from the file's parent directory.
            const parts = p.replace(/\\/g, '/').split('/');
            setProjectName(parts[parts.length - 2] || 'imported');
        } catch (e: any) {
            setPreview(null);
            setError(String(e?.message ?? e));
        } finally {
            setBusy(false);
        }
    };

    const doImport = async () => {
        setBusy(true);
        setError(null);
        try {
            if (props.mode === 'project') {
                const res = await ImportConfigAsProject(path, projectName);
                props.onImported(res.projectId);
            } else {
                const res = await ImportConfigIntoProject(props.projectId, path, props.position.x, props.position.y);
                props.onImported(res.nodes ?? []);
            }
            props.onClose();
        } catch (e: any) {
            setError(String(e?.message ?? e));
        } finally {
            setBusy(false);
        }
    };

    const canImport = !!preview && (preview.services?.length ?? 0) > 0 && !busy &&
        (props.mode !== 'project' || projectName.trim() !== '');

    const footer = (
        <div className="dialog-footer-row">
            <button className="btn btn-ghost" onClick={props.onClose} disabled={busy}>Cancel</button>
            <button className="btn btn-primary" onClick={doImport} disabled={!canImport}>
                {busy ? <Loader2 size={14} className="spin"/> : <FileUp size={14}/>}
                {props.mode === 'project' ? 'Create project' : 'Import services'}
            </button>
        </div>
    );

    return (
        <Dialog title="Import from cloud config" onClose={props.onClose} footer={footer}>
            <div className="import-config">
                <p className="import-config-intro">
                    Import a <strong>docker-compose</strong>, <strong>Cloud Run</strong>, <strong>ECS</strong>, or{' '}
                    <strong>Container Apps</strong> file. Draft maps the container run-contract and build recipe; platform
                    control-plane settings (autoscaling, IAM) are preserved for round-trip export.
                </p>

                <div className="import-config-file">
                    <button className="btn btn-secondary" onClick={pickFile} disabled={busy}>
                        <FileUp size={14}/> Choose file…
                    </button>
                    {path && <code className="import-config-path" title={path}>{path}</code>}
                </div>

                {error && <p className="form-error">{error}</p>}

                {preview && (
                    <>
                        <div className="import-config-summary">
                            <span className="import-config-format">{FORMAT_LABELS[preview.format] ?? preview.format}</span>
                            <span className="import-config-svc-count">
                                {preview.services?.length ?? 0} service{(preview.services?.length ?? 0) === 1 ? '' : 's'}
                            </span>
                        </div>

                        <ul className="import-config-services">
                            {(preview.services ?? []).map((s) => (
                                <li key={s.name}>
                                    <span className="import-config-svc-name">{s.name}</span>
                                    <span className={`import-config-badge badge-${s.mode}`}>{s.mode}</span>
                                    {s.image && <code className="import-config-svc-detail">{s.image}</code>}
                                    {s.port && <span className="import-config-svc-detail">:{s.port}</span>}
                                </li>
                            ))}
                        </ul>

                        {props.mode === 'project' && (
                            <label className="import-config-field">
                                <span>Project name</span>
                                <input
                                    className="input"
                                    value={projectName}
                                    onChange={(e) => setProjectName(e.target.value)}
                                    placeholder="my-project"
                                />
                            </label>
                        )}

                        <div className="import-config-report">
                            <h4>Fidelity report</h4>
                            <ConfigReport report={preview.report}/>
                        </div>
                    </>
                )}
            </div>
        </Dialog>
    );
}
