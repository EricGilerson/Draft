import {BaseEdge, EdgeLabelRenderer, getBezierPath, useInternalNode, type EdgeProps} from '@xyflow/react';
import {useEffect, useRef, useState} from 'react';
import {getEdgeParams} from './floatingEdgeUtils';
import './EnvReferenceEdge.css';

export type EnvReferenceConn = {
    sourceNodeId: string;
    sourceLabel: string;
    sourceKey: string;
    targetNodeId: string;
    targetLabel: string;
    targetAttr: string;
};

export default function EnvReferenceEdge({id, source, target, data, style}: EdgeProps) {
    const sourceNode = useInternalNode(source);
    const targetNode = useInternalNode(target);
    const [open, setOpen] = useState(false);
    const containerRef = useRef<HTMLDivElement>(null);

    const connections = ((data as {connections?: EnvReferenceConn[]} | undefined)?.connections) ?? [];

    useEffect(() => {
        if (!open) return;
        const onDocMouseDown = (e: MouseEvent) => {
            if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
                setOpen(false);
            }
        };
        document.addEventListener('mousedown', onDocMouseDown);
        return () => document.removeEventListener('mousedown', onDocMouseDown);
    }, [open]);

    if (!sourceNode || !targetNode || connections.length === 0) return null;

    const {sx, sy, tx, ty, sourcePos, targetPos} = getEdgeParams(sourceNode, targetNode);

    const [edgePath, labelX, labelY] = getBezierPath({
        sourceX: sx,
        sourceY: sy,
        sourcePosition: sourcePos,
        targetX: tx,
        targetY: ty,
        targetPosition: targetPos,
    });

    const badgeText = connections.length === 1 ? connections[0].sourceKey : `${connections.length} links`;

    return (
        <>
            <BaseEdge id={id} path={edgePath} style={style} interactionWidth={6}/>
            <EdgeLabelRenderer>
                <div
                    ref={containerRef}
                    className="env-reference-edge-container nodrag nopan"
                    style={{
                        transform: `translate(-50%, -50%) translate(${labelX}px, ${labelY}px)`,
                        // Above React Flow nodes (selected nodes use z-index 1000).
                        zIndex: open ? 1001 : 1,
                    }}
                >
                    <button
                        type="button"
                        className="env-reference-edge-label"
                        onClick={(e) => {
                            e.stopPropagation();
                            setOpen((v) => !v);
                        }}
                    >
                        {badgeText}
                    </button>
                    {open && (
                        <div className="env-reference-edge-popover">
                            {connections.map((c, i) => (
                                <div className="env-reference-edge-popover-row" key={i}>
                                    <span className="env-reference-edge-popover-var">{c.sourceLabel}.{c.sourceKey}</span>
                                    <span className="env-reference-edge-popover-arrow">references</span>
                                    <span className="env-reference-edge-popover-target">{c.targetLabel}.{c.targetAttr}</span>
                                </div>
                            ))}
                        </div>
                    )}
                </div>
            </EdgeLabelRenderer>
        </>
    );
}
