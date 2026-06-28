import {X, Box, FolderOpen} from 'lucide-react';
import {useCallback, useEffect, useRef, useState} from 'react';
import {GetServiceRoot, SetServiceRoot, SelectServiceRoot} from '../../wailsjs/go/main/App';
import './NodeDetailPanel.css';

type Tab = {
    id: string;
    label: string;
    description: string;
};

const TABS: Tab[] = [
    {id: 'overview', label: 'Overview', description: 'Service summary, status, and image info'},
    {id: 'deployments', label: 'Deployments', description: 'Build and deploy history'},
    {id: 'variables', label: 'Variables', description: 'Environment variables and linked references'},
    {id: 'networking', label: 'Networking', description: 'Ports, domains, and service connections'},
    {id: 'logs', label: 'Logs', description: 'Live container output'},
    {id: 'metrics', label: 'Metrics', description: 'CPU, memory, and network usage'},
    {id: 'docker', label: 'Docker', description: 'Container config, image, and Dockerfile'},
    {id: 'settings', label: 'Settings', description: 'Rename, delete, and service options'},
];

type NodeDetailPanelProps = {
    nodeId: string;
    nodeLabel: string;
    projectId: number;
    projectPath: string;
    onClose: () => void;
    onRename: (nodeId: string, newLabel: string) => void;
};

export default function NodeDetailPanel({nodeId, nodeLabel, projectId, projectPath, onClose, onRename}: NodeDetailPanelProps) {
    const [activeTab, setActiveTab] = useState('overview');
    const [editing, setEditing] = useState(false);
    const [editValue, setEditValue] = useState(nodeLabel);
    const editRef = useRef<HTMLInputElement>(null);
    const current = TABS.find((t) => t.id === activeTab)!;

    useEffect(() => {
        setEditValue(nodeLabel);
    }, [nodeLabel]);

    const startEditing = () => {
        setEditValue(nodeLabel);
        setEditing(true);
        setTimeout(() => editRef.current?.select(), 0);
    };

    const commitRename = () => {
        setEditing(false);
        const trimmed = editValue.trim();
        if (trimmed && trimmed !== nodeLabel) {
            onRename(nodeId, trimmed);
        } else {
            setEditValue(nodeLabel);
        }
    };

    return (
        <div className="node-detail-panel">
            <div className="node-detail-header">
                <div className="node-detail-title-row">
                    <div className="node-detail-icon">
                        <Box size={14}/>
                    </div>
                    {editing ? (
                        <input
                            ref={editRef}
                            className="node-detail-title-input"
                            value={editValue}
                            onChange={(e) => setEditValue(e.target.value)}
                            onBlur={commitRename}
                            onKeyDown={(e) => {
                                if (e.key === 'Enter') commitRename();
                                if (e.key === 'Escape') {
                                    setEditValue(nodeLabel);
                                    setEditing(false);
                                }
                            }}
                        />
                    ) : (
                        <h2 className="node-detail-title" onClick={startEditing} title="Click to rename">
                            {nodeLabel}
                        </h2>
                    )}
                </div>
                <button className="dialog-close" onClick={onClose} aria-label="Close">
                    <X size={16}/>
                </button>
            </div>

            <nav className="node-detail-tabs">
                {TABS.map((tab) => (
                    <button
                        key={tab.id}
                        className={`node-detail-tab ${activeTab === tab.id ? 'node-detail-tab--active' : ''}`}
                        onClick={() => setActiveTab(tab.id)}
                    >
                        {tab.label}
                    </button>
                ))}
            </nav>

            <div className="node-detail-body">
                {activeTab === 'settings' ? (
                    <SettingsTab nodeId={nodeId} projectId={projectId} projectPath={projectPath} />
                ) : (
                    <div className="node-detail-placeholder">
                        <span className="node-detail-placeholder-title">{current.label}</span>
                        <span className="node-detail-placeholder-desc">{current.description}</span>
                    </div>
                )}
            </div>
        </div>
    );
}

type SettingsTabProps = {
    nodeId: string;
    projectId: number;
    projectPath: string;
};

function SettingsTab({nodeId, projectId, projectPath}: SettingsTabProps) {
    const [rootPath, setRootPath] = useState('');
    const [inputValue, setInputValue] = useState('');
    const [error, setError] = useState('');
    const [saving, setSaving] = useState(false);

    useEffect(() => {
        GetServiceRoot(nodeId, projectId).then((path) => {
            setRootPath(path || '');
            setInputValue(path || '');
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
        </div>
    );
}
