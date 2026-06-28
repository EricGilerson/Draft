import {type NodeProps} from '@xyflow/react';
import {Box} from 'lucide-react';
import './ServiceNode.css';

export default function ServiceNode({data}: NodeProps) {
    const label = (data.label as string) || 'unnamed';
    const status = (data.status as string) || 'stopped';

    return (
        <div className={`service-node service-node--${status}`}>
            <div className="service-node-icon">
                <Box size={14}/>
            </div>
            <div className="service-node-info">
                <span className="service-node-name">{label}</span>
                <span className="service-node-status">{status}</span>
            </div>
        </div>
    );
}
