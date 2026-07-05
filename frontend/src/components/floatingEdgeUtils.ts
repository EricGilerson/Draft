import {Position, type InternalNode} from '@xyflow/react';

function getNodeIntersection(intersectionNode: InternalNode, targetNode: InternalNode) {
    const {width, height} = intersectionNode.measured;
    const intersectionNodePosition = intersectionNode.internals.positionAbsolute;
    const targetPosition = targetNode.internals.positionAbsolute;

    const w = (width ?? 0) / 2;
    const h = (height ?? 0) / 2;

    const x2 = intersectionNodePosition.x + w;
    const y2 = intersectionNodePosition.y + h;
    const x1 = targetPosition.x + (targetNode.measured.width ?? 0) / 2;
    const y1 = targetPosition.y + (targetNode.measured.height ?? 0) / 2;

    const xx1 = (x1 - x2) / (2 * w) - (y1 - y2) / (2 * h);
    const yy1 = (x1 - x2) / (2 * w) + (y1 - y2) / (2 * h);
    const a = 1 / (Math.abs(xx1) + Math.abs(yy1) || 1);
    const xx3 = a * xx1;
    const yy3 = a * yy1;

    return {
        x: w * (xx3 + yy3) + x2,
        y: h * (-xx3 + yy3) + y2,
    };
}

function getEdgePosition(node: InternalNode, intersectionPoint: {x: number; y: number}) {
    const nx = Math.round(node.internals.positionAbsolute.x);
    const ny = Math.round(node.internals.positionAbsolute.y);
    const px = Math.round(intersectionPoint.x);
    const py = Math.round(intersectionPoint.y);
    const width = node.measured.width ?? 0;
    const height = node.measured.height ?? 0;

    if (px <= nx + 1) return Position.Left;
    if (px >= nx + width - 1) return Position.Right;
    if (py <= ny + 1) return Position.Top;
    if (py >= ny + height - 1) return Position.Bottom;
    return Position.Top;
}

export function getEdgeParams(source: InternalNode, target: InternalNode) {
    const sourceIntersectionPoint = getNodeIntersection(source, target);
    const targetIntersectionPoint = getNodeIntersection(target, source);

    const sourcePos = getEdgePosition(source, sourceIntersectionPoint);
    const targetPos = getEdgePosition(target, targetIntersectionPoint);

    return {
        sx: sourceIntersectionPoint.x,
        sy: sourceIntersectionPoint.y,
        tx: targetIntersectionPoint.x,
        ty: targetIntersectionPoint.y,
        sourcePos,
        targetPos,
    };
}
