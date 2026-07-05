import {type NodeProps} from '@xyflow/react';
import {Box} from 'lucide-react';
import TemplateIcon from './TemplateIcon';
import './ServiceNode.css';

type ServiceNodeData = {
    label?: string;
    status?: string;
    templateId?: number;
    icon?: string;
    iconColor?: string;
};

export default function ServiceNode({data}: NodeProps) {
    const d = data as ServiceNodeData;
    const label = d.label || 'unnamed';
    const status = d.status || 'stopped';
    const icon = d.icon;

    return (
        <div className={`service-node service-node--${status}`}>
            <div className="service-node-icon">
                {icon ? (
                    <TemplateIcon slug={icon} color={d.iconColor || 'currentColor'} size={14}/>
                ) : (
                    <Box size={14}/>
                )}
            </div>
            <div className="service-node-info">
                <span className="service-node-name">{label}</span>
                <span className="service-node-status">{status}</span>
            </div>
        </div>
    );
}
