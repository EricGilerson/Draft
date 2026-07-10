import {X, Box, GitCompare, RefreshCw, Upload} from 'lucide-react';
import {useEffect, useRef, useState} from 'react';
import {GetNode, ReapplyTemplate} from '../../wailsjs/go/main/App';
import ExportConfigDialog from './ExportConfigDialog';
import {store} from '../../wailsjs/go/models';
import OverviewTab from './OverviewTab';
import DeploymentsTab from './DeploymentsTab';
import VariablesTab from './VariablesTab';
import LogsTab from './LogsTab';
import MetricsTab from './MetricsTab';
import SettingsTab from './SettingsTab';
import ShellTab from './ShellTab';
import ServiceDraftBar from './ServiceDraftBar';
import SyncConfigDialog from './SyncConfigDialog';
import {useAppDialog} from './AppDialogProvider';
import {ServiceConfigEditorProvider, useServiceConfigEditor} from '../lib/serviceConfigEditor';
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
    {id: 'shell', label: 'Shell'},
    {id: 'metrics', label: 'Metrics'},
    {id: 'settings', label: 'Settings'},
];

type NodeDetailPanelProps = {
    nodeId: string;
    nodeLabel: string;
    projectId: number;
    projectPath: string;
    environmentId?: number;
    onClose: () => void;
    onRename: (nodeId: string, newLabel: string) => Promise<void>;
    onServicesChanged?: () => void;
    onServiceDeleted?: (nodeId: string) => void;
    onOpenRootService?: (projectId: number, rootNodeId: string, rootEnvironmentId: number) => void;
};

function NodeDetailPanelBody({
    nodeId,
    nodeLabel,
    projectId,
    projectPath,
    environmentId,
    onClose,
    onRename,
    onServicesChanged,
    onServiceDeleted,
    onOpenRootService,
}: NodeDetailPanelProps) {
    const [activeTab, setActiveTab] = useState('overview');
    const [editing, setEditing] = useState(false);
    const [editValue, setEditValue] = useState(nodeLabel);
    const [renameError, setRenameError] = useState<string | null>(null);
    const [templateId, setTemplateId] = useState<number>(0);
    const [nodeEnvironmentId, setNodeEnvironmentId] = useState<number | null>(environmentId ?? null);
    const [reapplying, setReapplying] = useState(false);
    const [showExport, setShowExport] = useState(false);
    const [showSync, setShowSync] = useState(false);
    const [reapplyError, setReapplyError] = useState<string | null>(null);
    const editRef = useRef<HTMLInputElement>(null);
    const {isSessionDirty, discardSessionDraft, reload} = useServiceConfigEditor();
    const {confirm} = useAppDialog();

    useEffect(() => {
        setEditValue(nodeLabel);
        setRenameError(null);
    }, [nodeLabel]);

    useEffect(() => {
        setTemplateId(0);
        setReapplyError(null);
        GetNode(nodeId)
            .then((n: store.CanvasNode) => {
                setTemplateId(n.templateId || 0);
                if (n.environmentId) setNodeEnvironmentId(n.environmentId);
            })
            .catch(() => setTemplateId(0));
    }, [nodeId]);

    const handleReapply = async () => {
        if (!templateId) return;
        if (!await confirm({
            title: 'Re-apply template?',
            message: 'Template-derived settings, generated env vars, and the Dockerfile will be reset to the template’s current defaults.',
            detail: 'Env vars you added or edited manually are kept.',
            confirmLabel: 'Re-apply',
        })) return;
        setReapplying(true);
        setReapplyError(null);
        ReapplyTemplate(nodeId)
            .then(() => {
                setReapplying(false);
                onServicesChanged?.();
            })
            .catch((e: any) => {
                setReapplying(false);
                setReapplyError(typeof e === 'string' ? e : e?.message || 're-apply failed');
            });
    };

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
                setRenameError(String(e));
                setTimeout(() => editRef.current?.focus(), 0);
            });
    };

    const switchTab = async (tabId: string) => {
        if (tabId === activeTab) return;
        if (isSessionDirty) {
            const ok = await confirm({
                title: 'Discard unsaved edits?',
                message: 'You have unsaved edits. Discard them and switch tabs?',
                confirmLabel: 'Discard',
                cancelLabel: 'Stay',
                danger: true,
            });
            if (!ok) return;
            discardSessionDraft();
        }
        setActiveTab(tabId);
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
                <div className="node-detail-header-actions">
                    <button
                        className="btn btn-ghost node-detail-reapply"
                        onClick={() => setShowSync(true)}
                        disabled={!nodeEnvironmentId}
                        title="Sync settings/env from another environment into this service"
                    >
                        <GitCompare size={13}/>
                        Sync
                    </button>
                    <button
                        className="btn btn-ghost node-detail-reapply"
                        onClick={() => setShowExport(true)}
                        title="Export this service to a cloud config format"
                    >
                        <Upload size={13}/>
                        Export
                    </button>
                    <button
                        className="btn btn-ghost node-detail-reapply"
                        onClick={() => { void handleReapply(); }}
                        disabled={!templateId || reapplying}
                        title={templateId ? 'Re-apply this service\u2019s template defaults' : 'No template linked to this service'}
                    >
                        <RefreshCw size={13} className={reapplying ? 'spin' : ''}/>
                        Re-apply template
                    </button>
                    <button className="dialog-close" onClick={onClose} aria-label="Close">
                        <X size={16}/>
                    </button>
                </div>
            </div>
            {renameError && <p className="form-error node-detail-rename-error">{renameError}</p>}
            {showSync && nodeEnvironmentId != null && (
                <SyncConfigDialog
                    projectId={projectId}
                    targetEnvironmentId={nodeEnvironmentId}
                    targetNodeId={nodeId}
                    targetNodeLabel={nodeLabel}
                    onClose={() => setShowSync(false)}
                    onApplied={() => {
                        void reload();
                        onServicesChanged?.();
                    }}
                />
            )}
            {reapplyError && <p className="form-error node-detail-rename-error">{reapplyError}</p>}

            <nav className="node-detail-tabs">
                {TABS.map((tab) => (
                    <button
                        key={tab.id}
                        className={`node-detail-tab ${activeTab === tab.id ? 'node-detail-tab--active' : ''}`}
                        onClick={() => { void switchTab(tab.id); }}
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
                {activeTab === 'shell' && <ShellTab nodeId={nodeId} />}
                {activeTab === 'metrics' && <MetricsTab nodeId={nodeId} />}
                {activeTab === 'settings' && (
                    <SettingsTab
                        nodeId={nodeId}
                        projectId={projectId}
                        projectPath={projectPath}
                        serviceLabel={nodeLabel}
                        onServicesChanged={onServicesChanged}
                        onServiceDeleted={() => onServiceDeleted?.(nodeId)}
                        onOpenRootService={onOpenRootService}
                    />
                )}
            </div>
            <ServiceDraftBar onStaged={onServicesChanged} onDeploy={onServicesChanged} />
            {showExport && (
                <ExportConfigDialog nodeId={nodeId} label={nodeLabel} onClose={() => setShowExport(false)}/>
            )}
        </div>
    );
}

export default function NodeDetailPanel(props: NodeDetailPanelProps) {
    return (
        <ServiceConfigEditorProvider nodeId={props.nodeId} projectId={props.projectId}>
            <NodeDetailPanelBody {...props} />
        </ServiceConfigEditorProvider>
    );
}
