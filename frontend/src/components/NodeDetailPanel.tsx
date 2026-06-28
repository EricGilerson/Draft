import {X, Box} from 'lucide-react';
import {useState} from 'react';
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
};

export default function NodeDetailPanel({nodeLabel, onClose}: NodeDetailPanelProps) {
    const [activeTab, setActiveTab] = useState('overview');
    const current = TABS.find((t) => t.id === activeTab)!;

    return (
        <div className="node-detail-panel">
            <div className="node-detail-header">
                <div className="node-detail-title-row">
                    <div className="node-detail-icon">
                        <Box size={14}/>
                    </div>
                    <h2 className="node-detail-title">{nodeLabel}</h2>
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
