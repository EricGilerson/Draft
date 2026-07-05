import {
    Background,
    BackgroundVariant,
    ControlButton,
    Controls,
    type Node,
    type Edge,
    ReactFlow,
    useReactFlow,
    useNodesState,
    useEdgesState,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import {Maximize2, Minus, Plus, PlusCircle} from 'lucide-react';
import {useCallback, useEffect, useMemo, useState} from 'react';
import {EventsOn} from '../../wailsjs/runtime/runtime';
import {DeleteNode, GetDeployments, GetProjectConnections, ListNodes, ListServiceTemplates, UpdateNode} from '../../wailsjs/go/main/App';
import {store} from '../../wailsjs/go/models';
import ServiceNode from './ServiceNode';
import NodeDetailPanel from './NodeDetailPanel';
import ResizablePanel from './ResizablePanel';
import CreateServiceDialog from './CreateServiceDialog';
import './ProjectCanvas.css';

type ProjectCanvasProps = {
    project: store.Project;
    onServicesChanged?: () => void;
};

type ServiceNodeData = {
    label: string;
    status: string;
    deploymentId?: number;
    templateId?: number;
    icon?: string;
    iconColor?: string;
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

export default function ProjectCanvas({project, onServicesChanged}: ProjectCanvasProps) {
    const [nodes, setNodes, onNodesChange] = useNodesState<Node<ServiceNodeData>>([]);
    const [edges, setEdges, onEdgesChange] = useEdgesState<Edge>([]);
    const [showCreate, setShowCreate] = useState(false);
    const [templates, setTemplates] = useState<store.ServiceTemplate[]>([]);
    const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);

    const nodeTypes = useMemo(() => ({service: ServiceNode}), []);

    const templateById = useMemo(() => {
        const m = new Map<number, store.ServiceTemplate>();
        for (const t of templates) m.set(t.id, t);
        return m;
    }, [templates]);

    useEffect(() => {
        ListServiceTemplates()
            .then((list) => setTemplates(list ?? []))
            .catch(() => {});
    }, []);

    const selectedNode = useMemo(
        () => nodes.find((n) => n.id === selectedNodeId) ?? null,
        [nodes, selectedNodeId],
    );

    useEffect(() => {
        ListNodes(project.id).then(async (saved) => {
            if (!saved || saved.length === 0) return;
            // Make sure templates are loaded so we can attach icon metadata to
            // nodes created from a template. If the templates list isn't ready
            // yet, fetch it once more so the first paint has icons.
            let tpls = templates;
            if (tpls.length === 0) {
                try {
                    tpls = await ListServiceTemplates();
                    setTemplates(tpls ?? []);
                } catch { /* leave icons blank */ }
            }
            const tplMap = new Map<number, store.ServiceTemplate>();
            for (const t of tpls) tplMap.set(t.id, t);
            const flowNodes = await Promise.all(
                saved.map(async (n) => {
                    let status = 'stopped';
                    let deploymentId: number | undefined;
                    try {
                        const deps = await GetDeployments(n.id);
                        const dep = deps?.[0];
                        if (dep?.status) {
                            status = serviceStatusFromDeployment(dep.status);
                            deploymentId = dep.id;
                        }
                    } catch { /* no deployment history */ }
                    const tpl = n.templateId ? tplMap.get(n.templateId) : undefined;
                    return {
                        id: n.id,
                        type: 'service' as const,
                        position: {x: n.x, y: n.y},
                        data: {
                            label: n.label,
                            status,
                            deploymentId,
                            templateId: n.templateId || undefined,
                            icon: tpl?.icon,
                            iconColor: tpl?.color,
                        },
                    };
                }),
            );
            setNodes(flowNodes);
        });
    }, [project.id, setNodes, templates]);

    // Connections are read-only edges derived from variable references
    // (@{Label.ATTR} tokens) across the project's env vars — there's no
    // drag-to-connect on the canvas. Refetched whenever the detail panel's
    // selection changes, since that's when a service's variables were just
    // edited.
    useEffect(() => {
        GetProjectConnections(project.id)
            .then((conns) => {
                const flowEdges: Edge[] = (conns || []).map((c) => ({
                    id: `${c.sourceNodeId}:${c.sourceKey}->${c.targetNodeId}`,
                    source: c.sourceNodeId,
                    target: c.targetNodeId,
                    label: c.sourceKey,
                    selectable: false,
                    focusable: false,
                    reconnectable: false,
                    style: {stroke: 'var(--text-faint)', strokeDasharray: '4 3'},
                    labelStyle: {fill: 'var(--text-faint)', fontSize: 10},
                }));
                setEdges(flowEdges);
            })
            .catch(() => {});
    }, [project.id, selectedNodeId, setEdges]);

    useEffect(() => {
        const unsubscribe = EventsOn('deploy:status', (payload: any) => {
            const nodeId: string = payload.nodeId;
            const event = payload.event;
            const deployStatus: string = event?.status;
            const deploymentId: number | undefined = event?.deploymentId;
            if (!nodeId || !deployStatus) return;
            const uiStatus = serviceStatusFromDeployment(deployStatus);
            setNodes((prev) =>
                prev.map((n) =>
                    n.id === nodeId
                        ? {
                            ...n,
                            data: (() => {
                                const currentId = typeof n.data?.deploymentId === 'number' ? n.data.deploymentId : undefined;
                                if (typeof deploymentId === 'number' && typeof currentId === 'number' && deploymentId < currentId) {
                                    return n.data;
                                }
                                return {...n.data, status: uiStatus, deploymentId: deploymentId ?? currentId};
                            })(),
                        }
                        : n,
                ),
            );
        });
        return unsubscribe;
    }, [setNodes]);

    const handleNodesChange = useCallback(
        (changes: Parameters<typeof onNodesChange>[0]) => {
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

    const openCreate = () => {
        setShowCreate(true);
    };

    const handleCreated = (node: store.CanvasNode, template?: store.ServiceTemplate) => {
        const flowNode: Node<ServiceNodeData> = {
            id: node.id,
            type: 'service',
            position: {x: node.x, y: node.y},
            data: {
                label: node.label,
                status: 'stopped',
                templateId: node.templateId || undefined,
                icon: template?.icon,
                iconColor: template?.color,
            },
        };
        setNodes((prev) => [...prev, flowNode]);
        onServicesChanged?.();
        setShowCreate(false);
        setSelectedNodeId(node.id);
    };

    const renameNode = useCallback((nodeId: string, newLabel: string) => {
        const node = nodes.find((n) => n.id === nodeId);
        return UpdateNode(nodeId, node?.position.x ?? 0, node?.position.y ?? 0, newLabel).then(() => {
            setNodes((prev) =>
                prev.map((n) =>
                    n.id === nodeId ? {...n, data: {...n.data, label: newLabel}} : n,
                ),
            );
            onServicesChanged?.();
        });
    }, [onServicesChanged, setNodes, nodes]);

    return (
        <div className="project-canvas-layout">
            <div className="project-canvas" aria-label={`${project.name} canvas`}>
                <div className="canvas-workspace-label" aria-hidden="true">
                    <span className="canvas-kicker">Local topology</span>
                    <strong>{project.name}</strong>
                    <span className="canvas-path">{project.path}</span>
                </div>

                <div className="canvas-add-wrapper">
                    <button className="btn btn-primary canvas-add-btn" onClick={openCreate}>
                        <PlusCircle size={15}/> Add Service
                    </button>
                </div>

                <ReactFlow
                    colorMode="dark"
                    nodes={nodes}
                    edges={edges}
                    onNodesChange={handleNodesChange}
                    onEdgesChange={onEdgesChange}
                    edgesReconnectable={false}
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

            {showCreate && (
                <CreateServiceDialog
                    projectId={project.id}
                    position={{x: 120 + Math.random() * 300, y: 140 + Math.random() * 200}}
                    onClose={() => setShowCreate(false)}
                    onCreated={handleCreated}
                />
            )}
        </div>
    );
}
