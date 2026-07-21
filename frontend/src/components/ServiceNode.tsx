import {memo} from 'react';
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
    hasReferenceIssues?: boolean;
    /** Most recent deploy attempt failed (may still show running from prior). */
    lastDeployFailed?: boolean;
    lastDeployError?: string;
    health?: string;
    hostPort?: number;
    publicUrl?: string;
    /** Linked (virtualized) service badge: e.g. "Main" */
    linkedFromEnv?: string;
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

function ServiceNode({id, data}: NodeProps) {
    const d = data as ServiceNodeData;
    const selection = useCanvasSelection();
    const selectedVolume = selection?.selectedVolume;
    const label = d.label || 'unnamed';
    const status = d.status || 'stopped';
    const statusLoading = status === 'loading';
    const icon = d.icon;
    const volumes = d.volumes ?? [];
    const hasReferenceIssues = d.hasReferenceIssues ?? false;
    const lastDeployFailed = d.lastDeployFailed ?? false;
    const lastDeployError = d.lastDeployError || 'Latest deployment failed';
    const health = d.health;
    const url = statusLoading ? null : hostLabel(d.publicUrl, d.hostPort);
    const chipClass = healthClass(health);
    // Corner badges: deploy failure takes priority (more urgent than ref issues).
    // When both, stack them slightly.
    const showDeployFailBadge = lastDeployFailed && !statusLoading;
    const showRefBadge = hasReferenceIssues && !statusLoading;

    return (
        <div
            className={[
                'service-node',
                `service-node--${status}`,
                volumes.length > 0 ? 'service-node--has-volumes' : '',
                statusLoading ? 'service-node--status-loading' : '',
                lastDeployFailed ? 'service-node--deploy-failed' : '',
            ].filter(Boolean).join(' ')}
        >
            {showDeployFailBadge && (
                <span
                    className="service-node-deploy-fail"
                    title={
                        status === 'running' || status === 'starting'
                            ? `${lastDeployError} (previous instance may still be running)`
                            : lastDeployError
                    }
                >
                    <AlertCircle size={14} strokeWidth={2.25} />
                </span>
            )}
            {showRefBadge && (
                <span
                    className={`service-node-ref-warning${showDeployFailBadge ? ' service-node-ref-warning--offset' : ''}`}
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
                    <span className="service-node-name">
                        {label}
                        {d.linkedFromEnv && (
                            <span className="service-node-linked-badge" title={`Linked to ${d.linkedFromEnv}`}>
                                Linked · {d.linkedFromEnv}
                            </span>
                        )}
                    </span>
                    <span className="service-node-status-row">
                        {chipClass && !statusLoading && (
                            <span
                                className={`service-node-health ${chipClass}`}
                                title={`Docker health: ${health}`}
                            />
                        )}
                        {statusLoading ? (
                            <span className="service-node-status-skel" aria-label="Loading status" />
                        ) : (
                            <span className="service-node-status">
                                {status}
                                {lastDeployFailed && status !== 'failed' ? ' · deploy failed' : ''}
                            </span>
                        )}
                    </span>
                    <span
                        className={`service-node-url${url ? '' : ' service-node-url--placeholder'}`}
                        title={url || undefined}
                        aria-hidden={!url}
                    >
                        {url || '\u00a0'}
                    </span>
                </div>
            </div>
            {volumes.length > 0 && (
                <div className="service-node-volumes">
                    {volumes.map((vol) => {
                        const selected = selectedVolume?.parentNodeId === id && selectedVolume.index === vol.index;
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

export default memo(ServiceNode);
