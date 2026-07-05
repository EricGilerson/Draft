import {BaseEdge, EdgeLabelRenderer, getBezierPath, useInternalNode, type EdgeProps} from '@xyflow/react';
import {getEdgeParams} from './floatingEdgeUtils';
import './EnvReferenceEdge.css';

export default function EnvReferenceEdge({id, source, target, label, style}: EdgeProps) {
    const sourceNode = useInternalNode(source);
    const targetNode = useInternalNode(target);

    if (!sourceNode || !targetNode) return null;

    const {sx, sy, tx, ty, sourcePos, targetPos} = getEdgeParams(sourceNode, targetNode);

    const [edgePath, labelX, labelY] = getBezierPath({
        sourceX: sx,
        sourceY: sy,
        sourcePosition: sourcePos,
        targetX: tx,
        targetY: ty,
        targetPosition: targetPos,
    });

    return (
        <>
            <BaseEdge id={id} path={edgePath} style={style} interactionWidth={6}/>
            {label && (
                <EdgeLabelRenderer>
                    <div
                        className="env-reference-edge-label nodrag nopan"
                        style={{
                            transform: `translate(-50%, -50%) translate(${labelX}px, ${labelY}px)`,
                        }}
                    >
                        {label}
                    </div>
                </EdgeLabelRenderer>
            )}
        </>
    );
}
