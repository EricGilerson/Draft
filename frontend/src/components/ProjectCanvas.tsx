import {
    Background,
    BackgroundVariant,
    ControlButton,
    Controls,
    type Node,
    type Edge,
    type NodeChange,
    ReactFlow,
    useReactFlow,
    useNodesState,
    useEdgesState,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import {Maximize2, Minus, Plus, PlusCircle, X} from 'lucide-react';
import {useCallback, useEffect, useMemo, useRef, useState} from 'react';
import {EventsOn} from '../../wailsjs/runtime/runtime';
import {CreateNode, DeleteNode, GetActiveDeployment, ListNodes, UpdateNode} from '../../wailsjs/go/main/App';
import {store} from '../../wailsjs/go/models';
import ServiceNode from './ServiceNode';
import NodeDetailPanel from './NodeDetailPanel';
import ResizablePanel from './ResizablePanel';
import './ProjectCanvas.css';

type ProjectCanvasProps = {
    project: store.Project;
    onServicesChanged?: () => void;
};

function CanvasControls() {
    const {zoomIn, zoomOut, fitView} = useReactFlow();

    return (
        <Controls
            position="bottom-left"
            showZoom={false}
            showFitView={false}
            showInteractive={false}
            className="canvas-controls"
        >
            <ControlButton className="canvas-control-button" onClick={() => zoomIn()}>
                <Plus size={15}/>
            </ControlButton>
            <ControlButton className="canvas-control-button" onClick={() => zoomOut()}>
                <Minus size={15}/>
            </ControlButton>
            <ControlButton className="canvas-control-button" onClick={() => fitView({padding: 0.2})}>
                <Maximize2 size={15}/>
            </ControlButton>
        </Controls>
    );
}

function serviceStatusFromDeployment(status: string): string {
    switch (status) {
        case 'running':
            return 'running';
        case 'failed':
            return 'error';
        case 'building':
        case 'built':
        case 'starting':
        case 'pending':
            return 'starting';
        default:
            return 'stopped';
    }
}

let nodeCounter = 0;

function generateId(): string {
    nodeCounter++;
    const hex = nodeCounter.toString(16).padStart(4, '0');
    const rand = Math.random().toString(16).slice(2, 10);
    return `svc-${hex}-${rand}`;
}

export default function ProjectCanvas({project, onServicesChanged}: ProjectCanvasProps) {
    const [nodes, setNodes, onNodesChange] = useNodesState<Node>([]);
    const [edges, _setEdges, onEdgesChange] = useEdgesState<Edge>([]);
    const [showAddPopover, setShowAddPopover] = useState(false);
    const [newNodeName, setNewNodeName] = useState('');
    const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);
    const inputRef = useRef<HTMLInputElement>(null);

    const nodeTypes = useMemo(() => ({service: ServiceNode}), []);

    const selectedNode = useMemo(
        () => nodes.find((n) => n.id === selectedNodeId) ?? null,
        [nodes, selectedNodeId],
    );

    useEffect(() => {
        ListNodes(project.id).then(async (saved) => {
            if (!saved || saved.length === 0) return;
            const flowNodes = await Promise.all(
                saved.map(async (n) => {
                    let status = 'stopped';
                    try {
                        const dep = await GetActiveDeployment(n.id);
                        if (dep?.status) {
                            status = serviceStatusFromDeployment(dep.status);
                        }
                    } catch { /* no active deployment */ }
                    return {
                        id: n.id,
                        type: 'service' as const,
                        position: {x: n.x, y: n.y},
                        data: {label: n.label, status},
                    };
                }),
            );
            setNodes(flowNodes);
        });
    }, [project.id, setNodes]);

    useEffect(() => {
        const unsubscribe = EventsOn('deploy:status', (payload: any) => {
            const nodeId: string = payload.nodeId;
            const deployStatus: string = payload.event?.status;
            if (!nodeId || !deployStatus) return;
            const uiStatus = serviceStatusFromDeployment(deployStatus);
            setNodes((prev) =>
                prev.map((n) =>
                    n.id === nodeId ? {...n, data: {...n.data, status: uiStatus}} : n,
                ),
            );
        });
        return unsubscribe;
    }, [setNodes]);

    const handleNodesChange = useCallback(
        (changes: NodeChange[]) => {
            onNodesChange(changes);

            for (const change of changes) {
                if (change.type === 'position' && change.dragging === false && change.position) {
                    const node = nodes.find((n) => n.id === change.id);
                    const label = (node?.data?.label as string) || change.id;
                    UpdateNode(change.id, change.position.x, change.position.y, label);
                }
                if (change.type === 'remove') {
                    DeleteNode(change.id).then(() => onServicesChanged?.());
                    if (selectedNodeId === change.id) setSelectedNodeId(null);
                }
            }
        },
        [onNodesChange, nodes, onServicesChanged, selectedNodeId],
    );

    const addNode = useCallback(() => {
        const id = generateId();
        const label = newNodeName.trim() || id;
        const x = 120 + Math.random() * 300;
        const y = 140 + Math.random() * 200;

        CreateNode(id, label, project.id, x, y).then(() => {
            const node: Node = {
                id,
                type: 'service',
                position: {x, y},
                data: {label, status: 'stopped'},
            };
            setNodes((prev) => [...prev, node]);
            onServicesChanged?.();
        });

        setNewNodeName('');
        setShowAddPopover(false);
    }, [newNodeName, onServicesChanged, setNodes, project.id]);

    const renameNode = useCallback((nodeId: string, newLabel: string) => {
        setNodes((prev) =>
            prev.map((n) =>
                n.id === nodeId ? {...n, data: {...n.data, label: newLabel}} : n,
            ),
        );
        const node = nodes.find((n) => n.id === nodeId);
        UpdateNode(nodeId, node?.position.x ?? 0, node?.position.y ?? 0, newLabel).then(() => onServicesChanged?.());
    }, [onServicesChanged, setNodes, nodes]);

    const openPopover = () => {
        setShowAddPopover(true);
        setNewNodeName('');
        setTimeout(() => inputRef.current?.focus(), 0);
    };

    return (
        <div className="project-canvas-layout">
            <div className="project-canvas" aria-label={`${project.name} canvas`}>
                <div className="canvas-workspace-label" aria-hidden="true">
                    <span className="canvas-kicker">Local topology</span>
                    <strong>{project.name}</strong>
                    <span className="canvas-path">{project.path}</span>
                </div>

                <div className="canvas-add-wrapper">
                    <button className="btn btn-primary canvas-add-btn" onClick={openPopover}>
                        <PlusCircle size={15}/> Add Service
                    </button>
                    {showAddPopover && (
                        <div className="canvas-add-popover">
                            <div className="canvas-add-popover-header">
                                <span>New service</span>
                                <button className="dialog-close" onClick={() => setShowAddPopover(false)}>
                                    <X size={14}/>
                                </button>
                            </div>
                            <div className="canvas-add-popover-body">
                                <input
                                    ref={inputRef}
                                    className="input"
                                    value={newNodeName}
                                    onChange={(e) => setNewNodeName(e.target.value)}
                                    placeholder="Service name (optional)"
                                    onKeyDown={(e) => {
                                        if (e.key === 'Enter') addNode();
                                        if (e.key === 'Escape') setShowAddPopover(false);
                                    }}
                                />
                                <button className="btn btn-primary" onClick={addNode}>
                                    Add
                                </button>
                            </div>
                        </div>
                    )}
                </div>

                <ReactFlow
                    colorMode="dark"
                    nodes={nodes}
                    edges={edges}
                    onNodesChange={handleNodesChange}
                    onEdgesChange={onEdgesChange}
                    onNodeClick={(_e, node) => setSelectedNodeId(node.id)}
                    onPaneClick={() => setSelectedNodeId(null)}
                    nodeTypes={nodeTypes}
                    fitView
                    minZoom={0.4}
                    maxZoom={1.6}
                    proOptions={{hideAttribution: true}}
                >
                    <Background variant={BackgroundVariant.Dots} gap={22} size={1}/>
                    <CanvasControls/>
                </ReactFlow>
            </div>

            {selectedNode && (
                <ResizablePanel side="right" defaultWidth={380} minWidth={300} maxWidth={640} storageKey="draft:node-detail-width">
                    <NodeDetailPanel
                        nodeId={selectedNode.id}
                        nodeLabel={(selectedNode.data.label as string) || selectedNode.id}
                        projectId={project.id}
                        projectPath={project.path}
                        onClose={() => setSelectedNodeId(null)}
                        onRename={renameNode}
                        onServicesChanged={onServicesChanged}
                    />
                </ResizablePanel>
            )}
        </div>
    );
}
