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
import {FileUp, Maximize2, Minus, Plus, PlusCircle, Settings} from 'lucide-react';
import {useCallback, useEffect, useMemo, useRef, useState, type MouseEvent} from 'react';
import {EventsOn} from '../../wailsjs/runtime/runtime';
import {
    DeleteNode,
    GetDeployments,
    GetEnvironmentConnections,
    GetLinkedServiceInfo,
    GetNodeConfigStatus,
    GetNodeHealth,
    ListManagedVolumes,
    ListNodes,
    ListNodesWithReferenceIssues,
    ListServiceTemplates,
    UpdateNode,
} from '../../wailsjs/go/main/App';
import {deploy, store} from '../../wailsjs/go/models';
import ServiceNode, {type ServiceNodeVolume} from './ServiceNode';
import EnvReferenceEdge, {type EnvReferenceConn} from './EnvReferenceEdge';
import NodeDetailPanel from './NodeDetailPanel';
import VolumeDetailPanel from './VolumeDetailPanel';
import ResizablePanel from './ResizablePanel';
import CreateServiceDialog from './CreateServiceDialog';
import ImportConfigDialog from './ImportConfigDialog';
import {parseVolumeEntries, type VolumeEntry} from './VolumeEditor';
import {
    CanvasSelectionContext,
    type SelectedVolume,
} from './canvasSelection';
import './ProjectCanvas.css';

type ProjectCanvasProps = {
    project: store.Project;
    environmentId: number;
    onServicesChanged?: () => void;
    /** When set (e.g. from the global Volumes tab's "reveal on canvas"), select
     * the matching volume node once its mounts have loaded. Matched by owning
     * node id + container path since the canvas keys volumes positionally. */
    initialVolumeFocus?: {nodeId: string; target: string} | null;
    /** Called once initialVolumeFocus has been consumed so the parent can clear
     * it (a focus should fire once, not on every re-render). */
    onVolumeFocusApplied?: () => void;
    onOpenProjectSettings?: () => void;
    /** When set, select this node's detail panel once its node has loaded. Used
     * by the Routes tab's "open on canvas" action. Cleared via onNodeFocusApplied. */
    initialSelectedNodeId?: string | null;
    onNodeFocusApplied?: () => void;
};

type ServiceNodeData = {
    label: string;
    status: string;
    deploymentId?: number;
    templateId?: number;
    icon?: string;
    iconColor?: string;
    hasReferenceIssues?: boolean;
    health?: string;
    hostPort?: number;
    publicUrl?: string;
};

function volumeBasename(path: string): string {
    const trimmed = path.replace(/\/+$/, '');
    const i = trimmed.lastIndexOf('/');
    return i >= 0 ? trimmed.slice(i + 1) || trimmed : trimmed;
}

function buildServiceVolumes(
    mounts: VolumeEntry[],
    managed: deploy.ManagedVolume[],
    pending: boolean,
): ServiceNodeVolume[] {
    const managedByTarget = new Map(managed.filter((v) => v.target).map((v) => [v.target, v]));
    return mounts.map((entry, index) => {
        const managedVol = managedByTarget.get(entry.containerPath);
        return {
            index,
            containerPath: entry.containerPath,
            label: volumeBasename(entry.containerPath || 'volume'),
            isNamed: entry.type === 'volume',
            readOnly: !!entry.readOnly,
            usageBytes: managedVol?.size,
            pending,
        };
    });
}

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

/** Map deployment status → canvas node pill status. Preserve real lifecycle
 *  states so the pill shows "building" during builds, not "starting". */
function serviceStatusFromDeployment(status: string): string {
    switch (status) {
        case 'running':
        case 'building':
        case 'built':
        case 'starting':
        case 'pending':
        case 'stopped':
        case 'failed':
        case 'interrupted':
            return status;
        case 'error':
            return 'failed';
        default:
            return 'stopped';
    }
}

export default function ProjectCanvas({project, environmentId, onServicesChanged, initialVolumeFocus, onVolumeFocusApplied, onOpenProjectSettings, initialSelectedNodeId, onNodeFocusApplied}: ProjectCanvasProps) {
    const [serviceNodes, setServiceNodes, onServiceNodesChange] = useNodesState<Node<ServiceNodeData>>([]);
    const [connectionEdges, setConnectionEdges] = useEdgesState<Edge>([]);
    const [volumeMountsByNode, setVolumeMountsByNode] = useState<Record<string, VolumeEntry[]>>({});
    const [volumePendingByNode, setVolumePendingByNode] = useState<Record<string, boolean>>({});
    const [managedVolumesByNode, setManagedVolumesByNode] = useState<Record<string, deploy.ManagedVolume[]>>({});
    const [showCreate, setShowCreate] = useState(false);
    const [showImport, setShowImport] = useState(false);
    const [templates, setTemplates] = useState<store.ServiceTemplate[]>([]);
    const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);
    const [selectedVolume, setSelectedVolume] = useState<SelectedVolume | null>(null);
    const nodeClickRef = useRef(false);

    const nodeTypes = useMemo(() => ({service: ServiceNode}), []);
    const edgeTypes = useMemo(() => ({envReference: EnvReferenceEdge}), []);

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
        () => serviceNodes.find((n) => n.id === selectedNodeId) ?? null,
        [serviceNodes, selectedNodeId],
    );

    const refreshVolumeMounts = useCallback(async (nodeIds: string[]) => {
        if (nodeIds.length === 0) {
            setVolumeMountsByNode({});
            setVolumePendingByNode({});
            setManagedVolumesByNode({});
            return;
        }
        const [settingsPairs, managedPairs] = await Promise.all([
            Promise.all(nodeIds.map(async (id) => {
                try {
                    const status = await GetNodeConfigStatus(id);
                    const applied = status?.appliedSettings || {};
                    const staged = status?.stagedSettings || {};
                    const effective = {...applied, ...staged};
                    return [
                        id,
                        parseVolumeEntries(effective.volume_mounts),
                        'volume_mounts' in staged,
                    ] as const;
                } catch {
                    return [id, [] as VolumeEntry[], false] as const;
                }
            })),
            Promise.all(nodeIds.map(async (id) => {
                try {
                    const list = await ListManagedVolumes(project.id, id);
                    return [id, list ?? []] as const;
                } catch {
                    return [id, [] as deploy.ManagedVolume[]] as const;
                }
            })),
        ]);
        const mountsMap = Object.fromEntries(settingsPairs.map(([id, mounts]) => [id, mounts]));
        const pendingMap = Object.fromEntries(settingsPairs.map(([id, , pending]) => [id, pending]));
        setVolumeMountsByNode(mountsMap);
        setVolumePendingByNode(pendingMap);
        setManagedVolumesByNode(Object.fromEntries(managedPairs));
    }, [project.id]);

    const refreshNodeHealth = useCallback(async (nodeIds: string[]) => {
        if (nodeIds.length === 0) return;
        const results = await Promise.all(
            nodeIds.map(async (id) => {
                try {
                    const h = await GetNodeHealth(id);
                    return [id, h] as const;
                } catch {
                    return [id, null] as const;
                }
            }),
        );
        setServiceNodes((prev) =>
            prev.map((n) => {
                const entry = results.find(([id]) => id === n.id);
                if (!entry) return n;
                const h = entry[1];
                if (!h) return n;
                return {
                    ...n,
                    data: {
                        ...n.data,
                        health: h.dockerHealth || undefined,
                        hostPort: h.hostPort || undefined,
                        publicUrl: h.publicUrl || undefined,
                    },
                };
            }),
        );
    }, [setServiceNodes]);

    const refreshReferenceIssueNodes = useCallback(() => {
        ListNodesWithReferenceIssues(environmentId)
            .then((ids) => {
                const flagged = new Set(ids ?? []);
                setServiceNodes((prev) =>
                    prev.map((n) => ({
                        ...n,
                        data: {...n.data, hasReferenceIssues: flagged.has(n.id)},
                    })),
                );
            })
            .catch(() => {});
    }, [environmentId, setServiceNodes]);

    const selectVolume = useCallback((volume: SelectedVolume) => {
        nodeClickRef.current = true;
        setSelectedVolume(volume);
        setSelectedNodeId(null);
    }, []);

    // Apply an incoming "reveal on canvas" focus once the target node's mounts
    // have loaded. Fires once: it clears the request via onVolumeFocusApplied
    // whether or not a matching volume was found, so a stale target can't loop.
    useEffect(() => {
        if (!initialVolumeFocus) return;
        const mounts = volumeMountsByNode[initialVolumeFocus.nodeId];
        if (!mounts) return; // not loaded yet
        const index = mounts.findIndex((m) => m.containerPath === initialVolumeFocus.target);
        if (index >= 0) {
            const parentLabel = serviceNodes.find((n) => n.id === initialVolumeFocus.nodeId)?.data.label as string | undefined;
            selectVolume({parentNodeId: initialVolumeFocus.nodeId, index, parentLabel});
        }
        onVolumeFocusApplied?.();
    }, [initialVolumeFocus, volumeMountsByNode, serviceNodes, selectVolume, onVolumeFocusApplied]);

    // Apply an incoming "select this node" focus once the node has loaded. Used
    // by the Routes tab so clicking a row opens the owning project and lands on
    // the service's detail panel.
    useEffect(() => {
        if (!initialSelectedNodeId) return;
        const exists = serviceNodes.some((n) => n.id === initialSelectedNodeId);
        if (!exists) return; // not loaded yet
        setSelectedNodeId(initialSelectedNodeId);
        onNodeFocusApplied?.();
    }, [initialSelectedNodeId, serviceNodes, onNodeFocusApplied]);

    const nodes = useMemo(
        () => serviceNodes.map((n) => ({
            ...n,
            selected: n.id === selectedNodeId,
            data: {
                ...n.data,
                volumes: buildServiceVolumes(
                    volumeMountsByNode[n.id] || [],
                    managedVolumesByNode[n.id] || [],
                    volumePendingByNode[n.id] || false,
                ),
                selectedVolumeIndex: selectedVolume?.parentNodeId === n.id
                    ? selectedVolume.index
                    : undefined,
            },
        })),
        [serviceNodes, volumeMountsByNode, managedVolumesByNode, volumePendingByNode, selectedNodeId, selectedVolume],
    );

    const edges = connectionEdges;

    useEffect(() => {
        ListNodes(environmentId).then(async (saved) => {
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
                    let linkedFromEnv: string | undefined;
                    try {
                        const link = await GetLinkedServiceInfo(n.id);
                        if (link?.isLinked) {
                            linkedFromEnv = link.rootEnvName || link.rootLabel || 'linked';
                        }
                    } catch { /* not linked */ }
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
                            linkedFromEnv,
                        },
                    };
                }),
            );
            setServiceNodes(flowNodes);
            refreshVolumeMounts(flowNodes.map((n) => n.id));
            refreshNodeHealth(flowNodes.map((n) => n.id));
            refreshReferenceIssueNodes();
        });
    }, [environmentId, setServiceNodes, templates, refreshVolumeMounts, refreshNodeHealth, refreshReferenceIssueNodes]);

    // Connections are read-only edges derived from variable references
    // (@{Label.ATTR} tokens) across the project's env vars — there's no
    // drag-to-connect on the canvas. Refetched whenever the detail panel's
    // selection changes, since that's when a service's variables were just
    // edited.
    useEffect(() => {
        const nodeIds = new Set(serviceNodes.map((n) => n.id));
        const labelById = new Map(serviceNodes.map((n) => [n.id, n.data.label]));
        GetEnvironmentConnections(environmentId)
            .then((conns) => {
                // Group same-pair connections into a single edge — a pair can have
                // several vars referencing each other (or references in both
                // directions), and drawing one overlapping edge per var meant only
                // the topmost label was ever visible. The edge now shows a badge
                // (var name, or a count when there's more than one) that opens a
                // popover listing every reference between the two services.
                const pairs = new Map<string, EnvReferenceConn[]>();
                for (const c of conns || []) {
                    if (!nodeIds.has(c.sourceNodeId) || !nodeIds.has(c.targetNodeId)) continue;
                    const key = [c.sourceNodeId, c.targetNodeId].sort().join('|');
                    const list = pairs.get(key) ?? [];
                    list.push({
                        sourceNodeId: c.sourceNodeId,
                        sourceLabel: labelById.get(c.sourceNodeId) ?? c.sourceNodeId,
                        sourceKey: c.sourceKey,
                        targetNodeId: c.targetNodeId,
                        targetLabel: labelById.get(c.targetNodeId) ?? c.targetNodeId,
                        targetAttr: c.targetAttr,
                    });
                    pairs.set(key, list);
                }
                const flowEdges: Edge[] = Array.from(pairs.entries()).map(([key, connections]) => {
                    const [a, b] = key.split('|');
                    return {
                        id: `conn:${key}`,
                        source: a,
                        target: b,
                        type: 'envReference',
                        selectable: false,
                        focusable: false,
                        reconnectable: false,
                        data: {connections},
                        style: {stroke: 'var(--text-faint)', strokeDasharray: '4 3'},
                    };
                });
                setConnectionEdges(flowEdges);
            })
            .catch(() => {});
    }, [environmentId, selectedNodeId, selectedVolume, setConnectionEdges, serviceNodes]);

    useEffect(() => {
        refreshReferenceIssueNodes();
    }, [refreshReferenceIssueNodes, selectedNodeId]);

    useEffect(() => {
        const unsubscribe = EventsOn('deploy:status', (payload: any) => {
            const nodeId: string = payload.nodeId;
            const event = payload.event;
            const deployStatus: string = event?.status;
            const deploymentId: number | undefined = event?.deploymentId;
            if (!nodeId || !deployStatus) return;
            const uiStatus = serviceStatusFromDeployment(deployStatus);
            setServiceNodes((prev) =>
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
            // Refresh health/URL once the container is up or on its way up.
            if (deployStatus === 'running' || deployStatus === 'starting' || deployStatus === 'stopped' || deployStatus === 'failed') {
                refreshNodeHealth([nodeId]);
            }
        });
        return unsubscribe;
    }, [setServiceNodes, refreshNodeHealth]);

    const handleNodesChange = useCallback(
        (changes: NodeChange[]) => {
            onServiceNodesChange(changes as NodeChange<Node<ServiceNodeData>>[]);

            for (const change of changes) {
                if (change.type === 'position' && change.dragging === false && change.position) {
                    const node = serviceNodes.find((n) => n.id === change.id);
                    const label = (node?.data?.label as string) || change.id;
                    UpdateNode(change.id, change.position.x, change.position.y, label);
                }
                if (change.type === 'remove') {
                    DeleteNode(change.id).then(() => onServicesChanged?.());
                    if (selectedNodeId === change.id) setSelectedNodeId(null);
                    if (selectedVolume?.parentNodeId === change.id) setSelectedVolume(null);
                    setVolumeMountsByNode((prev) => {
                        const next = {...prev};
                        delete next[change.id];
                        return next;
                    });
                }
            }
        },
        [onServiceNodesChange, serviceNodes, onServicesChanged, selectedNodeId, selectedVolume],
    );

    const handleVolumesChanged = useCallback(() => {
        refreshVolumeMounts(serviceNodes.map((n) => n.id));
        onServicesChanged?.();
    }, [refreshVolumeMounts, serviceNodes, onServicesChanged]);

    const notifyServicesChanged = useCallback(() => {
        refreshVolumeMounts(serviceNodes.map((n) => n.id));
        refreshReferenceIssueNodes();
        onServicesChanged?.();
    }, [refreshVolumeMounts, refreshReferenceIssueNodes, serviceNodes, onServicesChanged]);

    const openCreate = () => {
        setShowCreate(true);
    };

    const handleCreated = (node: store.CanvasNode, template?: store.ServiceTemplate) => {
        // Image-mode stamps auto-deploy on the backend; show starting until SSE
        // updates the badge. Build-mode and blank nodes stay stopped.
        const initialStatus = template?.mode === 'image' ? 'starting' : 'stopped';
        const flowNode: Node<ServiceNodeData> = {
            id: node.id,
            type: 'service',
            position: {x: node.x, y: node.y},
            data: {
                label: node.label,
                status: initialStatus,
                templateId: node.templateId || undefined,
                icon: template?.icon,
                iconColor: template?.color,
            },
        };
        setServiceNodes((prev) => {
            const next = [...prev, flowNode];
            refreshVolumeMounts(next.map((n) => n.id));
            return next;
        });
        onServicesChanged?.();
        setShowCreate(false);
        setSelectedNodeId(node.id);
    };

    const handleImported = (imported: store.CanvasNode[]) => {
        if (!imported || imported.length === 0) {
            setShowImport(false);
            return;
        }
        const flowNodes: Node<ServiceNodeData>[] = imported.map((node) => ({
            id: node.id,
            type: 'service',
            position: {x: node.x, y: node.y},
            data: {
                label: node.label,
                // Image-mode imports auto-deploy on the backend; SSE will correct
                // the badge either way, so start optimistic.
                status: 'starting',
                templateId: node.templateId || undefined,
            },
        }));
        setServiceNodes((prev) => {
            const next = [...prev, ...flowNodes];
            refreshVolumeMounts(next.map((n) => n.id));
            return next;
        });
        onServicesChanged?.();
        setShowImport(false);
        setSelectedNodeId(imported[imported.length - 1].id);
    };

    const renameNode = useCallback((nodeId: string, newLabel: string) => {
        const node = serviceNodes.find((n) => n.id === nodeId);
        return UpdateNode(nodeId, node?.position.x ?? 0, node?.position.y ?? 0, newLabel).then(() => {
            setServiceNodes((prev) =>
                prev.map((n) =>
                    n.id === nodeId ? {...n, data: {...n.data, label: newLabel}} : n,
                ),
            );
            onServicesChanged?.();
        });
    }, [onServicesChanged, setServiceNodes, serviceNodes]);

    const handleServiceDeleted = useCallback((nodeId: string) => {
        setServiceNodes((prev) => prev.filter((n) => n.id !== nodeId));
        if (selectedNodeId === nodeId) setSelectedNodeId(null);
        if (selectedVolume?.parentNodeId === nodeId) setSelectedVolume(null);
        setVolumeMountsByNode((prev) => {
            const next = {...prev};
            delete next[nodeId];
            return next;
        });
        onServicesChanged?.();
    }, [onServicesChanged, selectedNodeId, selectedVolume, setServiceNodes]);

    const handleNodeClick = useCallback((e: MouseEvent, node: Node) => {
        if ((e.target as HTMLElement).closest('.service-node-volume')) return;
        nodeClickRef.current = true;
        setSelectedNodeId(node.id);
        setSelectedVolume(null);
    }, []);

    const handlePaneClick = useCallback(() => {
        requestAnimationFrame(() => {
            if (nodeClickRef.current) {
                nodeClickRef.current = false;
                return;
            }
            setSelectedNodeId(null);
            setSelectedVolume(null);
        });
    }, []);

    const selectedVolumeParentLabel = selectedVolume?.parentLabel
        || serviceNodes.find((n) => n.id === selectedVolume?.parentNodeId)?.data.label
        || 'Service';

    const canvasSelection = useMemo(
        () => ({selectVolume}),
        [selectVolume],
    );

    return (
        <div className="project-canvas-layout">
            <div className="project-canvas" aria-label={`${project.name} canvas`}>
                <div className="canvas-workspace-label" aria-hidden="true">
                    <span className="canvas-kicker">Local topology</span>
                    <strong>{project.name}</strong>
                    <span className="canvas-path">{project.path}</span>
                </div>

                <div className="canvas-add-wrapper">
                    {onOpenProjectSettings && (
                        <button
                            className="btn btn-ghost canvas-settings-btn"
                            onClick={onOpenProjectSettings}
                            title="Project settings"
                        >
                            <Settings size={15}/> Project settings
                        </button>
                    )}
                    <button className="btn btn-ghost canvas-settings-btn" onClick={() => setShowImport(true)} title="Import services from a cloud config file">
                        <FileUp size={15}/> Import config
                    </button>
                    <button className="btn btn-primary canvas-add-btn" onClick={openCreate}>
                        <PlusCircle size={15}/> Add Service
                    </button>
                </div>

                <CanvasSelectionContext.Provider value={canvasSelection}>
                <ReactFlow
                    colorMode="dark"
                    nodes={nodes}
                    edges={edges}
                    onNodesChange={handleNodesChange}
                    onEdgesChange={() => {}}
                    edgesReconnectable={false}
                    onNodeClick={handleNodeClick}
                    onPaneClick={handlePaneClick}
                    nodeTypes={nodeTypes}
                    edgeTypes={edgeTypes}
                    fitView
                    minZoom={0.4}
                    maxZoom={1.6}
                    proOptions={{hideAttribution: true}}
                >
                    <Background variant={BackgroundVariant.Dots} gap={22} size={1}/>
                    <CanvasControls/>
                </ReactFlow>
                </CanvasSelectionContext.Provider>
            </div>

            {selectedVolume && (
                <ResizablePanel side="right" defaultWidth={380} minWidth={300} maxWidth={640} storageKey="draft:volume-detail-width">
                    <VolumeDetailPanel
                        parentNodeId={selectedVolume.parentNodeId}
                        parentLabel={selectedVolumeParentLabel as string}
                        volumeIndex={selectedVolume.index}
                        projectId={project.id}
                        onClose={() => setSelectedVolume(null)}
                        onOpenService={() => {
                            setSelectedNodeId(selectedVolume.parentNodeId);
                            setSelectedVolume(null);
                        }}
                        onVolumesChanged={handleVolumesChanged}
                    />
                </ResizablePanel>
            )}

            {selectedNode && !selectedVolume && (
                <ResizablePanel side="right" defaultWidth={380} minWidth={300} maxWidth={640} storageKey="draft:node-detail-width">
                    <NodeDetailPanel
                        nodeId={selectedNode.id}
                        nodeLabel={(selectedNode.data.label as string) || selectedNode.id}
                        projectId={project.id}
                        projectPath={project.path}
                        onClose={() => setSelectedNodeId(null)}
                        onRename={renameNode}
                        onServicesChanged={notifyServicesChanged}
                        onServiceDeleted={handleServiceDeleted}
                    />
                </ResizablePanel>
            )}

            {showCreate && (
                <CreateServiceDialog
                    projectId={project.id}
                    environmentId={environmentId}
                    position={{x: 120 + Math.random() * 300, y: 140 + Math.random() * 200}}
                    onClose={() => setShowCreate(false)}
                    onCreated={handleCreated}
                />
            )}

            {showImport && (
                <ImportConfigDialog
                    mode="canvas"
                    projectId={project.id}
                    environmentId={environmentId}
                    position={{x: 120 + Math.random() * 300, y: 140 + Math.random() * 200}}
                    onClose={() => setShowImport(false)}
                    onImported={handleImported}
                />
            )}
        </div>
    );
}
