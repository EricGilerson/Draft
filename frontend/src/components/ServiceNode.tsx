import {Handle, Position, type NodeProps} from '@xyflow/react';
import {AlertCircle, Box, FolderOpen, HardDrive} from 'lucide-react';
import TemplateIcon from './TemplateIcon';
import {useCanvasSelection} from './canvasSelection';
import './ServiceNode.css';

export type ServiceNodeVolume = {
    index: number;
    containerPath: string;
    label: string;
    isNamed: boolean;
    readOnly?: boolean;
    usageBytes?: number;
    pending?: boolean;
};

type ServiceNodeData = {
    label?: string;
    status?: string;
    templateId?: number;
    icon?: string;
    iconColor?: string;
    volumes?: ServiceNodeVolume[];
    selectedVolumeIndex?: number;
    hasReferenceIssues?: boolean;
    health?: string;
    hostPort?: number;
    publicUrl?: string;
};

function healthClass(health?: string): string {
    switch (health) {
        case 'healthy':
            return 'service-node-health--healthy';
        case 'unhealthy':
            return 'service-node-health--unhealthy';
        case 'starting':
            return 'service-node-health--starting';
        default:
            return '';
    }
}

function hostLabel(publicUrl?: string, hostPort?: number): string | null {
    let host: string | undefined;
    if (publicUrl) {
        try {
            const u = new URL(publicUrl);
            host = u.hostname;
        } catch {
            host = publicUrl.replace(/^https?:\/\//, '').split('/')[0];
        }
    }
    if (host && hostPort) return `:${hostPort} · ${host}`;
    if (host) return host;
    if (hostPort) return `:${hostPort}`;
    return null;
}

function formatBytes(n: number): string {
    if (!n || n <= 0) return '';
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    let i = 0;
    let v = n;
    while (v >= 1024 && i < units.length - 1) {
        v /= 1024;
        i++;
    }
    return `${v.toFixed(v >= 10 || i === 0 ? 0 : 1)} ${units[i]}`;
}

export default function ServiceNode({id, data}: NodeProps) {
    const d = data as ServiceNodeData;
    const selection = useCanvasSelection();
    const label = d.label || 'unnamed';
    const status = d.status || 'stopped';
    const icon = d.icon;
    const volumes = d.volumes ?? [];
    const hasReferenceIssues = d.hasReferenceIssues ?? false;
    const health = d.health;
    const url = hostLabel(d.publicUrl, d.hostPort);
    const chipClass = healthClass(health);

    return (
        <div className={`service-node service-node--${status}${volumes.length > 0 ? ' service-node--has-volumes' : ''}`}>
            {hasReferenceIssues && (
                <span
                    className="service-node-ref-warning"
                    title="This service has broken variable references. Open Variables to fix them."
                >
                    <AlertCircle size={14} strokeWidth={2.25} />
                </span>
            )}
            <Handle
                type="target"
                position={Position.Left}
                id="env-in"
                className="service-node-env-handle"
                isConnectable={false}
            />
            <Handle
                type="source"
                position={Position.Bottom}
                id="env-out"
                className="service-node-env-handle"
                isConnectable={false}
            />
            <div className="service-node-main">
                <div className="service-node-icon">
                    {icon ? (
                        <TemplateIcon slug={icon} color={d.iconColor || 'currentColor'} size={14}/>
                    ) : (
                        <Box size={14}/>
                    )}
                </div>
                <div className="service-node-info">
                    <span className="service-node-name">{label}</span>
                    <span className="service-node-status-row">
                        {chipClass && (
                            <span
                                className={`service-node-health ${chipClass}`}
                                title={`Docker health: ${health}`}
                            />
                        )}
                        <span className="service-node-status">{status}</span>
                    </span>
                    {url && (
                        <span className="service-node-url" title={url}>{url}</span>
                    )}
                </div>
            </div>
            {volumes.length > 0 && (
                <div className="service-node-volumes">
                    {volumes.map((vol) => {
                        const selected = d.selectedVolumeIndex === vol.index;
                        const detail = [
                            vol.isNamed ? 'Named volume' : 'Bind mount',
                            vol.readOnly ? 'read-only' : '',
                            vol.usageBytes ? formatBytes(vol.usageBytes) : '',
                            vol.pending ? 'applies on next deploy' : '',
                        ].filter(Boolean).join(' · ');
                        return (
                            <button
                                key={vol.index}
                                type="button"
                                className={`service-node-volume${selected ? ' service-node-volume--selected' : ''}${vol.pending ? ' service-node-volume--pending' : ''}`}
                                title={detail || vol.containerPath}
                                onClick={(e) => {
                                    e.stopPropagation();
                                    selection?.selectVolume({
                                        parentNodeId: id,
                                        index: vol.index,
                                        parentLabel: label,
                                    });
                                }}
                            >
                                <span className={`service-node-volume-icon${vol.isNamed ? ' service-node-volume-icon--named' : ' service-node-volume-icon--bind'}`}>
                                    {vol.isNamed ? <HardDrive size={10}/> : <FolderOpen size={10}/>}
                                </span>
                                <span className="service-node-volume-label">{vol.label || 'volume'}</span>
                                {vol.usageBytes ? (
                                    <span className="service-node-volume-size">{formatBytes(vol.usageBytes)}</span>
                                ) : null}
                            </button>
                        );
                    })}
                </div>
            )}
        </div>
    );
}
