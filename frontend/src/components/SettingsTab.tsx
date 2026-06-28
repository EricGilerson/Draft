import {FolderOpen, FileSearch} from 'lucide-react';
import {useCallback, useEffect, useState} from 'react';
import {
    GetServiceRoot, SetServiceRoot, SelectServiceRoot,
    GetNodeSettings, SetNodeSetting, SelectFile, ParseDockerfileExpose,
} from '../../wailsjs/go/main/App';
import {dockerfile} from '../../wailsjs/go/models';

type SettingsTabProps = {
    nodeId: string;
    projectId: number;
    projectPath: string;
};

export default function SettingsTab({nodeId, projectId, projectPath}: SettingsTabProps) {
    const [rootPath, setRootPath] = useState('');
    const [inputValue, setInputValue] = useState('');
    const [error, setError] = useState('');
    const [saving, setSaving] = useState(false);
    const [dockerfilePath, setDockerfilePath] = useState('');
    const [dockerfileInput, setDockerfileInput] = useState('');
    const [port, setPort] = useState('');
    const [portInput, setPortInput] = useState('');
    const [exposePorts, setExposePorts] = useState<dockerfile.ExposePort[]>([]);

    useEffect(() => {
        GetServiceRoot(nodeId, projectId).then((path) => {
            setRootPath(path || '');
            setInputValue(path || '');
        });
        GetNodeSettings(nodeId).then((settings) => {
            const df = settings?.dockerfile || '';
            setDockerfilePath(df);
            setDockerfileInput(df);
            const p = settings?.service_port || '';
            setPort(p);
            setPortInput(p);
            if (df) {
                ParseDockerfileExpose(df, projectId).then(setExposePorts).catch(() => setExposePorts([]));
            }
        });
    }, [nodeId, projectId]);

    const saveRoot = useCallback(async (absolutePath: string) => {
        setError('');
        setSaving(true);
        try {
            await SetServiceRoot(nodeId, projectId, absolutePath);
            setRootPath(absolutePath);
            setInputValue(absolutePath);
        } catch (e: any) {
            const msg = typeof e === 'string' ? e : e?.message || 'Failed to set service root';
            setError(msg);
            setInputValue(rootPath);
        } finally {
            setSaving(false);
        }
    }, [nodeId, projectId, rootPath]);

    const handleInputCommit = useCallback(() => {
        const trimmed = inputValue.trim();
        if (!trimmed) {
            setInputValue(rootPath);
            return;
        }
        if (trimmed === rootPath) return;

        const sep = projectPath.includes('\\') ? '\\' : '/';
        const isAbsolute = /^[A-Za-z]:[\\/]/.test(trimmed) || trimmed.startsWith('/');
        const absolutePath = isAbsolute ? trimmed : projectPath + sep + trimmed;

        saveRoot(absolutePath);
    }, [inputValue, rootPath, projectPath, saveRoot]);

    const handleBrowse = useCallback(async () => {
        setError('');
        try {
            const selected = await SelectServiceRoot(projectId);
            if (selected) {
                await saveRoot(selected);
            }
        } catch (e: any) {
            const msg = typeof e === 'string' ? e : e?.message || 'Failed to select folder';
            setError(msg);
        }
    }, [projectId, saveRoot]);

    const commitDockerfile = useCallback((value?: string) => {
        const trimmed = (value ?? dockerfileInput).trim();
        if (trimmed === dockerfilePath) return;
        SetNodeSetting(nodeId, 'dockerfile', trimmed).then(() => {
            setDockerfilePath(trimmed);
            setDockerfileInput(trimmed);
            if (trimmed) {
                ParseDockerfileExpose(trimmed, projectId).then(setExposePorts).catch(() => setExposePorts([]));
            } else {
                setExposePorts([]);
            }
        });
    }, [nodeId, projectId, dockerfileInput, dockerfilePath]);

    const browseDockerfile = useCallback(async () => {
        const selected = await SelectFile('Select Dockerfile', projectPath);
        if (selected) {
            setDockerfileInput(selected);
            commitDockerfile(selected);
        }
    }, [projectPath, commitDockerfile]);

    const commitPort = useCallback((value?: string) => {
        const trimmed = (value ?? portInput).trim();
        if (trimmed === port) return;
        if (trimmed && (isNaN(Number(trimmed)) || Number(trimmed) < 1 || Number(trimmed) > 65535)) return;
        SetNodeSetting(nodeId, 'service_port', trimmed).then(() => {
            setPort(trimmed);
            setPortInput(trimmed);
        });
    }, [nodeId, portInput, port]);

    const applyExposePort = useCallback((exposePort: number) => {
        const val = String(exposePort);
        setPortInput(val);
        SetNodeSetting(nodeId, 'service_port', val).then(() => {
            setPort(val);
        });
    }, [nodeId]);

    const displayPath = rootPath
        ? (rootPath.toLowerCase().startsWith(projectPath.toLowerCase())
            ? '.' + rootPath.slice(projectPath.length)
            : rootPath)
        : '';

    return (
        <div className="settings-tab">
            <div className="settings-section">
                <h3 className="settings-section-title">Source</h3>
                <div className="form-field">
                    <label className="form-label">
                        Root Directory
                    </label>
                    <span className="settings-hint">
                        Path to the service source code, relative to the project root.
                        Must be inside <span className="settings-mono">{projectPath}</span>
                    </span>
                    <div className="input-with-action">
                        <input
                            className="input"
                            value={inputValue}
                            onChange={(e) => {
                                setInputValue(e.target.value);
                                setError('');
                            }}
                            onBlur={handleInputCommit}
                            onKeyDown={(e) => {
                                if (e.key === 'Enter') handleInputCommit();
                                if (e.key === 'Escape') setInputValue(rootPath);
                            }}
                            placeholder={projectPath}
                            disabled={saving}
                        />
                        <button
                            className="btn btn-ghost input-action-btn"
                            onClick={handleBrowse}
                            disabled={saving}
                            title="Browse for folder"
                        >
                            <FolderOpen size={14} />
                        </button>
                    </div>
                    {error && <p className="form-error">{error}</p>}
                    {displayPath && !error && (
                        <span className="settings-resolved">
                            {displayPath}
                        </span>
                    )}
                </div>
            </div>

            <div className="settings-section">
                <h3 className="settings-section-title">Docker</h3>
                <div className="form-field">
                    <label className="form-label">
                        Dockerfile
                    </label>
                    <span className="settings-hint">
                        Path to the Dockerfile used to build this service.
                    </span>
                    <div className="input-with-action">
                        <input
                            className="input"
                            value={dockerfileInput}
                            onChange={(e) => setDockerfileInput(e.target.value)}
                            onBlur={() => commitDockerfile()}
                            onKeyDown={(e) => {
                                if (e.key === 'Enter') commitDockerfile();
                                if (e.key === 'Escape') setDockerfileInput(dockerfilePath);
                            }}
                            placeholder="Dockerfile"
                        />
                        <button
                            className="btn btn-ghost input-action-btn"
                            onClick={browseDockerfile}
                            title="Browse for Dockerfile"
                        >
                            <FileSearch size={14} />
                        </button>
                    </div>
                </div>
            </div>

            <div className="settings-section">
                <h3 className="settings-section-title">Networking</h3>
                <div className="form-field">
                    <label className="form-label">
                        Port
                    </label>
                    <span className="settings-hint">
                        The port your service listens on inside the container. Required for deployment.
                    </span>
                    <input
                        className="input"
                        type="number"
                        min={1}
                        max={65535}
                        value={portInput}
                        onChange={(e) => setPortInput(e.target.value)}
                        onBlur={() => commitPort()}
                        onKeyDown={(e) => {
                            if (e.key === 'Enter') commitPort();
                            if (e.key === 'Escape') setPortInput(port);
                        }}
                        placeholder="e.g. 3000"
                    />
                    {exposePorts.length > 0 && (
                        <div className="settings-expose">
                            <span className="settings-expose-label">
                                Dockerfile EXPOSE:
                            </span>
                            <div className="settings-expose-ports">
                                {exposePorts.map((ep) => {
                                    const label = ep.protocol === 'udp' ? `${ep.port}/udp` : String(ep.port);
                                    const isActive = port === String(ep.port);
                                    return (
                                        <button
                                            key={`${ep.port}/${ep.protocol}`}
                                            className={`settings-expose-port ${isActive ? 'settings-expose-port--active' : ''}`}
                                            onClick={() => applyExposePort(ep.port)}
                                            title={isActive ? 'Currently set' : `Use port ${ep.port}`}
                                        >
                                            {label}
                                        </button>
                                    );
                                })}
                            </div>
                            <span className="settings-expose-note">
                                EXPOSE is documentation only — click a port to use it, or enter your own above.
                            </span>
                        </div>
                    )}
                    {port && exposePorts.length > 0 && !exposePorts.some(ep => String(ep.port) === port) && (
                        <span className="settings-expose-warning">
                            Port {port} differs from Dockerfile EXPOSE ({exposePorts.map(ep => ep.port).join(', ')}). This is fine if intentional.
                        </span>
                    )}
                </div>
            </div>
        </div>
    );
}
