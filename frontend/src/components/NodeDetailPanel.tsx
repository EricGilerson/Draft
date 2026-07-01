import {X, Box} from 'lucide-react';
import {useEffect, useRef, useState} from 'react';
import OverviewTab from './OverviewTab';
import DeploymentsTab from './DeploymentsTab';
import VariablesTab from './VariablesTab';
import LogsTab from './LogsTab';
import MetricsTab from './MetricsTab';
import SettingsTab from './SettingsTab';
import './NodeDetailPanel.css';

type Tab = {
    id: string;
    label: string;
};

const TABS: Tab[] = [
    {id: 'overview', label: 'Overview'},
    {id: 'deployments', label: 'Deployments'},
    {id: 'variables', label: 'Variables'},
    {id: 'logs', label: 'Logs'},
    {id: 'metrics', label: 'Metrics'},
    {id: 'settings', label: 'Settings'},
];

type NodeDetailPanelProps = {
    nodeId: string;
    nodeLabel: string;
    projectId: number;
    projectPath: string;
    onClose: () => void;
    onRename: (nodeId: string, newLabel: string) => Promise<void>;
    onServicesChanged?: () => void;
};

export default function NodeDetailPanel({nodeId, nodeLabel, projectId, projectPath, onClose, onRename, onServicesChanged}: NodeDetailPanelProps) {
    const [activeTab, setActiveTab] = useState('overview');
    const [editing, setEditing] = useState(false);
    const [editValue, setEditValue] = useState(nodeLabel);
    const [renameError, setRenameError] = useState<string | null>(null);
    const editRef = useRef<HTMLInputElement>(null);

    useEffect(() => {
        setEditValue(nodeLabel);
        setRenameError(null);
    }, [nodeLabel]);

    const startEditing = () => {
        setEditValue(nodeLabel);
        setRenameError(null);
        setEditing(true);
        setTimeout(() => editRef.current?.select(), 0);
    };

    const commitRename = () => {
        const trimmed = editValue.trim();
        if (!trimmed || trimmed === nodeLabel) {
            setEditing(false);
            setEditValue(nodeLabel);
            setRenameError(null);
            return;
        }
        setRenameError(null);
        onRename(nodeId, trimmed)
            .then(() => {
                setEditing(false);
            })
            .catch((e) => {
                // Keep the input open with the attempted value so the user
                // can see what failed (e.g. a duplicate service name) and
                // fix it, rather than silently reverting.
                setRenameError(String(e));
                setTimeout(() => editRef.current?.focus(), 0);
            });
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
                            onChange={(e) => {
                                setEditValue(e.target.value);
                                setRenameError(null);
                            }}
                            onBlur={commitRename}
                            onKeyDown={(e) => {
                                if (e.key === 'Enter') commitRename();
                                if (e.key === 'Escape') {
                                    setEditValue(nodeLabel);
                                    setRenameError(null);
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
            {renameError && <p className="form-error node-detail-rename-error">{renameError}</p>}

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
                {activeTab === 'overview' && <OverviewTab nodeId={nodeId} onServicesChanged={onServicesChanged} />}
                {activeTab === 'deployments' && <DeploymentsTab nodeId={nodeId} />}
                {activeTab === 'variables' && <VariablesTab nodeId={nodeId} projectId={projectId} projectPath={projectPath} />}
                {activeTab === 'logs' && <LogsTab nodeId={nodeId} />}
                {activeTab === 'metrics' && <MetricsTab nodeId={nodeId} />}
                {activeTab === 'settings' && <SettingsTab nodeId={nodeId} projectId={projectId} projectPath={projectPath} onServicesChanged={onServicesChanged} />}
            </div>
        </div>
    );
}
