import {X, Box} from 'lucide-react';
import {useEffect, useRef, useState} from 'react';
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
    onClose: () => void;
    onRename: (nodeId: string, newLabel: string) => void;
};

export default function NodeDetailPanel({nodeId, nodeLabel, onClose, onRename}: NodeDetailPanelProps) {
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
                <div className="node-detail-placeholder">
                    <span className="node-detail-placeholder-title">{current.label}</span>
                    <span className="node-detail-placeholder-desc">{current.description}</span>
                </div>
            </div>
        </div>
    );
}
