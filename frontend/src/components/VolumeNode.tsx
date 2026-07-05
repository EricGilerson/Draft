import {Handle, Position, type NodeProps} from '@xyflow/react';
import {FolderOpen, HardDrive} from 'lucide-react';
import type {VolumeEntry} from './VolumeEditor';
import {useCanvasSelection} from './canvasSelection';
import './VolumeNode.css';
export type VolumeNodeData = VolumeEntry & {
    parentNodeId: string;
    parentLabel?: string;
    index: number;
    resolvedName?: string;
    usageBytes?: number;
};

function basename(path: string): string {
    const trimmed = path.replace(/\/+$/, '');
    const i = trimmed.lastIndexOf('/');
    return i >= 0 ? trimmed.slice(i + 1) || trimmed : trimmed;
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

export default function VolumeNode({data, selected}: NodeProps) {
    const d = data as VolumeNodeData;
    const selection = useCanvasSelection();
    const isNamed = d.type === 'volume';
    const label = basename(d.containerPath || 'volume');
    const sublabel = isNamed
        ? (d.resolvedName ? d.resolvedName : 'Draft-managed')
        : (d.source || d.hostPath || 'Host path');

    const handleSelect = () => {
        selection?.selectVolume({
            parentNodeId: d.parentNodeId,
            index: d.index,
            parentLabel: d.parentLabel,
        });
    };

    return (
        <div
            className={`volume-node nodrag nopan ${selected ? 'volume-node--selected' : ''}`}
            onClick={(e) => {
                e.stopPropagation();
                handleSelect();
            }}
        >
            <Handle
                type="source"
                position={Position.Left}
                id="volume-mount"
                className="volume-node-mount-handle"
                isConnectable={false}
            />
            <div className={`volume-node-icon ${isNamed ? 'volume-node-icon--named' : 'volume-node-icon--bind'}`}>
                {isNamed ? <HardDrive size={12}/> : <FolderOpen size={12}/>}
            </div>
            <div className="volume-node-info">
                <span className="volume-node-name" title={d.containerPath}>{label || 'Volume'}</span>
                <span className="volume-node-meta" title={sublabel}>
                    {isNamed ? 'Named' : 'Bind'}
                    {d.readOnly ? ' · RO' : ''}
                    {d.usageBytes ? ` · ${formatBytes(d.usageBytes)}` : ''}
                </span>
            </div>
            <span className="volume-node-mount-tag">mount</span>
        </div>
    );
}