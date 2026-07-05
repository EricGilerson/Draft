import {BaseEdge, type EdgeProps, getStraightPath} from '@xyflow/react';
import './VolumeMountEdge.css';

export type VolumeMountEdgeData = {
    mountPath?: string;
    readOnly?: boolean;
    parentNodeId?: string;
    index?: number;
    parentLabel?: string;
};

export default function VolumeMountEdge({
    id,
    sourceX,
    sourceY,
    targetX,
    targetY,
    selected,
}: EdgeProps) {
    const [edgePath] = getStraightPath({sourceX, sourceY, targetX, targetY});

    return (
        <BaseEdge
            id={id}
            path={edgePath}
            style={{
                stroke: selected ? 'rgba(228, 200, 148, 0.95)' : 'rgba(212, 184, 132, 0.55)',
                strokeWidth: selected ? 2.4 : 1.6,
            }}
            className="volume-mount-edge-path"
            interactionWidth={16}
        />
    );
}
