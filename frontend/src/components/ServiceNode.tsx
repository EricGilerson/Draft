import {Handle, Position, type NodeProps} from '@xyflow/react';
import {AlertCircle, Box, HardDrive} from 'lucide-react';
import TemplateIcon from './TemplateIcon';
import './ServiceNode.css';

type ServiceNodeData = {
    label?: string;
    status?: string;
    templateId?: number;
    icon?: string;
    iconColor?: string;
    volumeCount?: number;
    hasReferenceIssues?: boolean;
};

export default function ServiceNode({data}: NodeProps) {
    const d = data as ServiceNodeData;
    const label = d.label || 'unnamed';
    const status = d.status || 'stopped';
    const icon = d.icon;
    const volumeCount = d.volumeCount ?? 0;
    const hasReferenceIssues = d.hasReferenceIssues ?? false;

    return (
        <div className={`service-node service-node--${status}`}>
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
            {volumeCount > 0 && (
                <Handle
                    type="target"
                    position={Position.Right}
                    id="volume-mount"
                    className="service-node-mount-handle"
                    isConnectable={false}
                />
            )}
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
                    <span className="service-node-status">{status}</span>
                    {volumeCount > 0 && (
                        <span className="service-node-volumes" title={`${volumeCount} volume${volumeCount === 1 ? '' : 's'}`}>
                            <HardDrive size={10}/>
                            {volumeCount}
                        </span>
                    )}
                </span>
            </div>
        </div>
    );
}
