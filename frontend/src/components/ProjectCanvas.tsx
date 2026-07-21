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
import {CheckCircle2, ChevronRight, Circle, FileUp, LoaderCircle, Maximize2, Minus, MoreHorizontal, Package, PanelRightClose, PanelRightOpen, Play, Plus, PlusCircle, RotateCcw, Settings, XCircle} from 'lucide-react';
import {useCallback, useEffect, useMemo, useRef, useState, type MouseEvent} from 'react';
import {EventsOn} from '../../wailsjs/runtime/runtime';
import {
    DeleteNode,
    GetActiveDeployment,
    GetEnvironmentConnections,
    GetLinkedServiceInfo,
    GetNodeConfigStatus,
    GetNodeHealth,
    GetSandboxTestRun,
    ListSandboxes,
    ListSandboxTestRuns,
    ListManagedVolumes,
    ListNodes,
    ListNodesWithReferenceIssues,
    ListServiceTemplates,
    StartTestingSandbox,
    UpdateNode,
} from '../../wailsjs/go/main/App';
import {deploy, store} from '../../wailsjs/go/models';
import ServiceNode, {type ServiceNodeVolume} from './ServiceNode';
import EnvReferenceEdge, {type EnvReferenceConn} from './EnvReferenceEdge';
import NodeDetailPanel from './NodeDetailPanel';
import VolumeDetailPanel from './VolumeDetailPanel';
import ResizablePanel from './ResizablePanel';
import CreateServiceDialog from './CreateServiceDialog';
import ExportDraftPackDialog from './ExportDraftPackDialog';
import ImportConfigDialog from './ImportConfigDialog';
import ImportDraftPackDialog from './ImportDraftPackDialog';
import {parseVolumeEntries, type VolumeEntry} from './VolumeEditor';
import {
    CanvasSelectionContext,
    type SelectedVolume,
} from './canvasSelection';
import {Skeleton, SkeletonBlock} from './Skeleton';
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
    onOpenLinkedRootService?: (projectId: number, rootNodeId: string, rootEnvironmentId: number) => void;
    /** When set, select this node's detail panel once its node has loaded. Used
     * by the Routes tab's "open on canvas" action. Cleared via onNodeFocusApplied. */
    initialSelectedNodeId?: string | null;
    onNodeFocusApplied?: () => void;
    /** A newly launched test run stays attached to its sandbox canvas until dismissed. */
    sandboxTestRunId?: number | null;
    onDismissSandboxTestRun?: () => void;
    onSandboxTestRunStarted?: (runId: number) => void;
    onOpenSandbox?: (projectId: number, environmentId: number) => void;
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
    /** Linked (virtualized) service badge: e.g. "Main" */
    linkedFromEnv?: string;
    /** Root node id when this canvas node is a linked alias. */
    linkedRootNodeId?: string;
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

function CanvasControls({environmentId, serviceCount}: {environmentId: number; serviceCount: number}) {
    const {zoomIn, zoomOut, fitView, getNodes} = useReactFlow();
    const fittedForEnv = useRef<number | null>(null);

    const showAllServices = useCallback((animate: boolean) => {
        const nodes = getNodes().filter((n) => n.type === 'service' || !n.type);
        if (nodes.length === 0) return;
        // Zoom out just enough to frame every service, with extra room around
        // the cluster so nothing sits under the chrome or flush to the edge.
        void fitView({
            nodes,
            padding: 0.28,
            duration: animate ? 280 : 0,
            minZoom: 0.1,
            maxZoom: 1.2,
        });
    }, [fitView, getNodes]);

    // Nodes load async after mount / env switch, so the ReactFlow `fitView`
    // prop often runs on an empty graph. Re-frame once services are present.
    useEffect(() => {
        if (environmentId <= 0 || serviceCount === 0) {
            if (serviceCount === 0) fittedForEnv.current = null;
            return;
        }
        if (fittedForEnv.current === environmentId) return;
        fittedForEnv.current = environmentId;
        // Wait until React Flow has painted + measured nodes before framing.
        let cancelled = false;
        const id = requestAnimationFrame(() => {
            requestAnimationFrame(() => {
                if (!cancelled) showAllServices(false);
            });
        });
        return () => {
            cancelled = true;
            cancelAnimationFrame(id);
        };
    }, [environmentId, serviceCount, showAllServices]);

    return (
        <Controls
            position="bottom-left"
            showZoom={false}
            showFitView={false}
            showInteractive={false}
            className="canvas-controls"
        >
            <ControlButton className="canvas-control-button" title="Zoom in" aria-label="Zoom in" onClick={() => zoomIn()}>
                <Plus size={15}/>
            </ControlButton>
            <ControlButton className="canvas-control-button" title="Zoom out" aria-label="Zoom out" onClick={() => zoomOut()}>
                <Minus size={15}/>
            </ControlButton>
            <ControlButton
                className="canvas-control-button"
                title="Show all services"
                aria-label="Show all services"
                onClick={() => showAllServices(true)}
            >
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

export default function ProjectCanvas({project, environmentId, onServicesChanged, initialVolumeFocus, onVolumeFocusApplied, onOpenProjectSettings, onOpenLinkedRootService, initialSelectedNodeId, onNodeFocusApplied, sandboxTestRunId, onDismissSandboxTestRun, onSandboxTestRunStarted, onOpenSandbox}: ProjectCanvasProps) {
    const [serviceNodes, setServiceNodes, onServiceNodesChange] = useNodesState<Node<ServiceNodeData>>([]);
    const [connectionEdges, setConnectionEdges] = useEdgesState<Edge>([]);
    const [volumeMountsByNode, setVolumeMountsByNode] = useState<Record<string, VolumeEntry[]>>({});
    const [volumePendingByNode, setVolumePendingByNode] = useState<Record<string, boolean>>({});
    const [managedVolumesByNode, setManagedVolumesByNode] = useState<Record<string, deploy.ManagedVolume[]>>({});
    const [showCreate, setShowCreate] = useState(false);
    const [showImport, setShowImport] = useState(false);
    const [showDraftPackImport, setShowDraftPackImport] = useState(false);
    const [showDraftPackExport, setShowDraftPackExport] = useState(false);
    const [toolbarMenuOpen, setToolbarMenuOpen] = useState(false);
    const [templates, setTemplates] = useState<store.ServiceTemplate[]>([]);
    const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);
    const [selectedVolume, setSelectedVolume] = useState<SelectedVolume | null>(null);
    const [canvasLoading, setCanvasLoading] = useState(true);
    const [sandboxTestRun, setSandboxTestRun] = useState<deploy.SandboxTestRunResult | null>(null);
    const [canvasSandboxTestRunId, setCanvasSandboxTestRunId] = useState<number | null>(null);
    const [dismissedSandboxTestRunId, setDismissedSandboxTestRunId] = useState<number | null>(null);
    const [sandboxTestPanelOpen, setSandboxTestPanelOpen] = useState(() => localStorage.getItem('draft:sandbox-test-panel') !== 'closed');
    const [sandboxTestAction, setSandboxTestAction] = useState<'steps' | 'fresh' | null>(null);
    const nodeClickRef = useRef(false);
    const toolbarMenuRef = useRef<HTMLDivElement | null>(null);
    const canvasLoadGen = useRef(0);
    const templatesRef = useRef(templates);
    templatesRef.current = templates;

    useEffect(() => {
        localStorage.setItem('draft:sandbox-test-panel', sandboxTestPanelOpen ? 'open' : 'closed');
    }, [sandboxTestPanelOpen]);

    useEffect(() => {
        if (sandboxTestRunId) {
            setCanvasSandboxTestRunId(null);
            return;
        }
        let cancelled = false;
        Promise.all([ListSandboxes(project.id), ListSandboxTestRuns(project.id, 50)])
            .then(([sandboxes, runs]) => {
                if (cancelled) return;
                const sandbox = (sandboxes ?? []).find((item) => item.environmentId === environmentId && item.purpose === 'test');
                const run = sandbox && (runs ?? []).find((item) => item.sandboxId === sandbox.id);
                setCanvasSandboxTestRunId(run?.id === dismissedSandboxTestRunId ? null : (run?.id ?? null));
            })
            .catch(() => { if (!cancelled) setCanvasSandboxTestRunId(null); });
        return () => { cancelled = true; };
    }, [sandboxTestRunId, project.id, environmentId, dismissedSandboxTestRunId]);

    const visibleSandboxTestRunId = sandboxTestRunId ?? canvasSandboxTestRunId;

    useEffect(() => {
        if (!visibleSandboxTestRunId) {
            setSandboxTestRun(null);
            return;
        }
        let cancelled = false;
        let timer: ReturnType<typeof setTimeout> | undefined;
        const load = async () => {
            try {
                const next = await GetSandboxTestRun(visibleSandboxTestRunId);
                if (cancelled) return;
                setSandboxTestRun(next);
                if (next.run.status === 'running') {
                    timer = setTimeout(() => { void load(); }, 750);
                }
            } catch {
                if (!cancelled) timer = setTimeout(() => { void load(); }, 1500);
            }
        };
        void load();
        return () => { cancelled = true; if (timer) clearTimeout(timer); };
    }, [visibleSandboxTestRunId]);

    useEffect(() => {
        if (!visibleSandboxTestRunId) return;
        return EventsOn('sandbox:test-progress', (payload: any) => {
            if (payload?.runId !== visibleSandboxTestRunId) return;
            setSandboxTestRun((current) => current ? deploy.SandboxTestRunResult.createFrom({
                ...current,
                run: payload.run,
                steps: payload.steps ?? current.steps,
            }) : current);
        });
    }, [visibleSandboxTestRunId]);

    useEffect(() => {
        if (!toolbarMenuOpen) return;
        const onDoc = (e: globalThis.MouseEvent) => {
            // Avoid `as Node` — @xyflow/react's Node type shadows the DOM Node.
            const target = e.target;
            if (!(target instanceof Element)) return;
            if (toolbarMenuRef.current && !toolbarMenuRef.current.contains(target)) {
                setToolbarMenuOpen(false);
            }
        };
        document.addEventListener('mousedown', onDoc);
        return () => document.removeEventListener('mousedown', onDoc);
    }, [toolbarMenuOpen]);

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
                if (!h) {
                    // Health miss: leave real statuses alone, but clear the
                    // first-paint "loading" placeholder so nodes don't stick.
                    if (n.data.status !== 'loading') return n;
                    return {...n, data: {...n.data, status: 'stopped'}};
                }
                // GetNodeHealth mirrors root status for linked aliases, so this is
                // the durable source of truth on load — not just live SSE.
                const status = h.status
                    ? serviceStatusFromDeployment(h.status)
                    : n.data.status === 'loading' ? 'stopped' : n.data.status;
                return {
                    ...n,
                    data: {
                        ...n.data,
                        status,
                        health: h.dockerHealth || undefined,
                        hostPort: h.hostPort || undefined,
                        publicUrl: h.publicUrl || undefined,
                    },
                };
            }),
        );
    }, [setServiceNodes]);

    const refreshLinkedServiceState = useCallback(async (nodeIds: string[]) => {
        if (nodeIds.length === 0) return;
        const results = await Promise.all(
            nodeIds.map(async (id) => {
                try {
                    const link = await GetLinkedServiceInfo(id);
                    return [id, {
                        linkedFromEnv: link?.isLinked ? (link.rootEnvName || link.rootLabel || 'linked') : undefined,
                        linkedRootNodeId: link?.isLinked ? (link.rootNodeId || undefined) : undefined,
                    }] as const;
                } catch {
                    return [id, {linkedFromEnv: undefined, linkedRootNodeId: undefined}] as const;
                }
            }),
        );
        const byId = new Map(results);
        setServiceNodes((prev) =>
            prev.map((n) => {
                const next = byId.get(n.id);
                if (!next) return n;
                return {
                    ...n,
                    data: {
                        ...n.data,
                        linkedFromEnv: next.linkedFromEnv,
                        linkedRootNodeId: next.linkedRootNodeId,
                        deploymentId: next.linkedRootNodeId ? undefined : n.data.deploymentId,
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

    const refreshActiveDeployments = useCallback(async (nodeIds: string[]) => {
        if (nodeIds.length === 0) return;
        const results = await Promise.all(
            nodeIds.map(async (id) => {
                try {
                    const dep = await GetActiveDeployment(id);
                    return [id, dep?.id] as const;
                } catch {
                    return [id, undefined] as const;
                }
            }),
        );
        const byId = new Map(results);
        setServiceNodes((prev) =>
            prev.map((n) => {
                if (n.data.linkedRootNodeId) return n;
                const deploymentId = byId.get(n.id);
                if (typeof deploymentId !== 'number') return n;
                return {...n, data: {...n.data, deploymentId}};
            }),
        );
    }, [setServiceNodes]);

    const reloadCanvasNodes = useCallback(async () => {
        const gen = ++canvasLoadGen.current;
        setCanvasLoading(true);
        // Clear immediately so env switches don't flash the previous topology.
        setServiceNodes([]);
        setConnectionEdges([]);
        setVolumeMountsByNode({});
        setVolumePendingByNode({});
        setManagedVolumesByNode({});

        try {
            const saved = await ListNodes(environmentId);
            if (gen !== canvasLoadGen.current) return;

            if (!saved || saved.length === 0) {
                setServiceNodes([]);
                setCanvasLoading(false);
                return;
            }

            // Templates for icons — fetch once if the list isn't ready yet so
            // the first structural paint still has brand marks.
            let tpls = templatesRef.current;
            if (tpls.length === 0) {
                try {
                    tpls = await ListServiceTemplates();
                    if (gen !== canvasLoadGen.current) return;
                    setTemplates(tpls ?? []);
                } catch { /* leave icons blank */ }
            }
            const tplMap = new Map<number, store.ServiceTemplate>();
            for (const t of tpls) tplMap.set(t.id, t);

            // Fast first paint: positions + labels + icons. Health, links, and
            // deployment ids enrich in the background so the canvas never sits
            // empty while per-node RPCs fan out.
            const flowNodes: Node<ServiceNodeData>[] = saved.map((n) => {
                const tpl = n.templateId ? tplMap.get(n.templateId) : undefined;
                return {
                    id: n.id,
                    type: 'service' as const,
                    position: {x: n.x, y: n.y},
                    data: {
                        label: n.label,
                        status: 'loading',
                        templateId: n.templateId || undefined,
                        icon: tpl?.icon,
                        iconColor: tpl?.color,
                    },
                };
            });
            setServiceNodes(flowNodes);
            setCanvasLoading(false);

            const nodeIds = flowNodes.map((n) => n.id);
            void refreshVolumeMounts(nodeIds);
            void refreshLinkedServiceState(nodeIds).then(() => {
                if (gen !== canvasLoadGen.current) return;
                void refreshActiveDeployments(nodeIds);
            });
            void refreshNodeHealth(nodeIds);
            refreshReferenceIssueNodes();
        } catch {
            if (gen === canvasLoadGen.current) {
                setServiceNodes([]);
                setCanvasLoading(false);
            }
        }
    }, [
        environmentId,
        setServiceNodes,
        setConnectionEdges,
        refreshVolumeMounts,
        refreshLinkedServiceState,
        refreshActiveDeployments,
        refreshNodeHealth,
        refreshReferenceIssueNodes,
    ]);

    useEffect(() => {
        setSelectedNodeId(null);
        setSelectedVolume(null);
    }, [environmentId]);

    useEffect(() => {
        void reloadCanvasNodes();
    }, [reloadCanvasNodes]);

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
            // Match the event node itself, plus any linked aliases whose root
            // is this node (root status lives on Main; aliases sit on other envs).
            const affectedIds: string[] = [];
            setServiceNodes((prev) => {
                const next = prev.map((n) => {
                    const isSelf = n.id === nodeId;
                    const isAliasOfRoot = n.data.linkedRootNodeId === nodeId;
                    if (!isSelf && !isAliasOfRoot) return n;
                    affectedIds.push(n.id);
                    if (isAliasOfRoot && !isSelf) {
                        // Aliases have no local deployment id; only mirror status.
                        return {...n, data: {...n.data, status: uiStatus}};
                    }
                    const currentId = typeof n.data?.deploymentId === 'number' ? n.data.deploymentId : undefined;
                    if (typeof deploymentId === 'number' && typeof currentId === 'number' && deploymentId < currentId) {
                        return n;
                    }
                    return {
                        ...n,
                        data: {...n.data, status: uiStatus, deploymentId: deploymentId ?? currentId},
                    };
                });
                return next;
            });
            // Refresh health/URL once the container is up or on its way up.
            if (deployStatus === 'running' || deployStatus === 'starting' || deployStatus === 'stopped' || deployStatus === 'failed') {
                // Always include the event node; aliases get health from root via API.
                const ids = affectedIds.length > 0 ? affectedIds : [nodeId];
                refreshNodeHealth(ids);
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
        const nodeIds = serviceNodes.map((n) => n.id);
        void refreshLinkedServiceState(nodeIds);
        void refreshNodeHealth(nodeIds);
        refreshVolumeMounts(nodeIds);
        refreshReferenceIssueNodes();
        onServicesChanged?.();
    }, [refreshLinkedServiceState, refreshNodeHealth, refreshVolumeMounts, refreshReferenceIssueNodes, serviceNodes, onServicesChanged]);

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
    const hasSandboxTestRun = sandboxTestRun?.sandbox?.environmentId === environmentId;
    const rerunSandboxTest = async (mode: 'steps' | 'fresh') => {
        if (!sandboxTestRun?.sandbox || sandboxTestAction) return;
        setSandboxTestAction(mode);
        try {
            let savedPlan: Record<string, unknown> = {purpose: 'test'};
            try { savedPlan = {...JSON.parse(sandboxTestRun.run.planJson || '{}'), purpose: 'test'}; } catch { /* use the safe test default */ }
            const result = await StartTestingSandbox(deploy.SandboxTestRunRequest.createFrom({
                name: sandboxTestRun.run.name,
                sourceEnvironmentId: sandboxTestRun.sandbox.sourceEnvironmentId,
                profileId: sandboxTestRun.sandbox.profileId || undefined,
                sandboxId: sandboxTestRun.sandbox.id,
                plan: deploy.SandboxPlan.createFrom(savedPlan),
                mode,
            }));
            setSandboxTestRun(result);
            setSandboxTestPanelOpen(true);
            onSandboxTestRunStarted?.(result.run.id);
            if (mode === 'fresh' && result.sandbox) {
                onOpenSandbox?.(result.sandbox.projectId, result.sandbox.environmentId);
            }
        } finally {
            setSandboxTestAction(null);
        }
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
                    {hasSandboxTestRun && (
                        <button
                            className="btn btn-ghost canvas-test-monitor-toggle"
                            onClick={() => setSandboxTestPanelOpen((open) => !open)}
                            title={sandboxTestPanelOpen ? 'Close test monitor' : 'Open test monitor'}
                        >
                            {sandboxTestPanelOpen ? <PanelRightClose size={15}/> : <PanelRightOpen size={15}/>}
                            <span className="canvas-btn-label">Test run</span>
                        </button>
                    )}
                    {onOpenProjectSettings && (
                        <button
                            className="btn btn-ghost canvas-settings-btn"
                            onClick={onOpenProjectSettings}
                            title="Project settings"
                        >
                            <Settings size={15}/>
                            <span className="canvas-btn-label">Settings</span>
                        </button>
                    )}
                    <div className="canvas-toolbar-menu-wrap" ref={toolbarMenuRef}>
                        <button
                            type="button"
                            className="btn btn-ghost canvas-settings-btn canvas-toolbar-more"
                            onClick={() => setToolbarMenuOpen((open) => !open)}
                            title="Import & export"
                            aria-expanded={toolbarMenuOpen}
                            aria-haspopup="menu"
                        >
                            <MoreHorizontal size={15}/>
                            <span className="canvas-btn-label">More</span>
                        </button>
                        {toolbarMenuOpen && (
                            <div className="canvas-toolbar-menu" role="menu">
                                <button
                                    type="button"
                                    role="menuitem"
                                    onClick={() => {
                                        setToolbarMenuOpen(false);
                                        setShowDraftPackImport(true);
                                    }}
                                >
                                    <Package size={14}/> Import Draft pack…
                                </button>
                                <button
                                    type="button"
                                    role="menuitem"
                                    onClick={() => {
                                        setToolbarMenuOpen(false);
                                        setShowDraftPackExport(true);
                                    }}
                                >
                                    <Package size={14}/> Export Draft pack…
                                </button>
                                <button
                                    type="button"
                                    role="menuitem"
                                    onClick={() => {
                                        setToolbarMenuOpen(false);
                                        setShowImport(true);
                                    }}
                                >
                                    <FileUp size={14}/> Import cloud config…
                                </button>
                            </div>
                        )}
                    </div>
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
                    fitViewOptions={{padding: 0.28}}
                    minZoom={0.1}
                    maxZoom={1.6}
                    proOptions={{hideAttribution: true}}
                    nodesDraggable={!canvasLoading}
                    nodesConnectable={false}
                    elementsSelectable={!canvasLoading}
                >
                    <Background variant={BackgroundVariant.Dots} gap={22} size={1}/>
                    <CanvasControls environmentId={environmentId} serviceCount={serviceNodes.length}/>
                </ReactFlow>
                </CanvasSelectionContext.Provider>

                {canvasLoading && (
                    <SkeletonBlock className="canvas-loading" label="Loading services">
                        <div className="canvas-loading-cluster" aria-hidden="true">
                            <div className="canvas-loading-node canvas-loading-node--a">
                                <Skeleton width={28} height={28} />
                                <div className="skel-col">
                                    <Skeleton width={88} height={12} />
                                    <Skeleton width={52} height={8} variant="pill" />
                                </div>
                            </div>
                            <div className="canvas-loading-node canvas-loading-node--b">
                                <Skeleton width={28} height={28} />
                                <div className="skel-col">
                                    <Skeleton width={72} height={12} />
                                    <Skeleton width={44} height={8} variant="pill" />
                                </div>
                            </div>
                            <div className="canvas-loading-node canvas-loading-node--c">
                                <Skeleton width={28} height={28} />
                                <div className="skel-col">
                                    <Skeleton width={96} height={12} />
                                    <Skeleton width={48} height={8} variant="pill" />
                                </div>
                            </div>
                            <span className="canvas-loading-link canvas-loading-link--ab" />
                            <span className="canvas-loading-link canvas-loading-link--ac" />
                        </div>
                        <p className="canvas-loading-label">Loading services…</p>
                    </SkeletonBlock>
                )}
            </div>

            {hasSandboxTestRun && sandboxTestPanelOpen && (
                <ResizablePanel side="right" defaultWidth={350} minWidth={300} maxWidth={560} storageKey="draft:sandbox-test-monitor-width" className="sandbox-test-run-resizable">
                <aside className="sandbox-test-run-panel" aria-live="polite">
                    <div className="sandbox-test-run-panel-head">
                        <div>
                            <span className="sandbox-test-run-kicker">Test run monitor</span>
                            <strong>{sandboxTestRun.run.name}</strong>
                        </div>
                        <div className={`sandbox-test-run-state sandbox-test-run-state--${sandboxTestRun.run.status}`}>
                            {sandboxTestRun.run.status === 'running' ? <LoaderCircle size={14}/> : sandboxTestRun.run.status === 'passed' ? <CheckCircle2 size={14}/> : <XCircle size={14}/>}
                            {sandboxTestRun.run.status === 'running' ? 'Running' : sandboxTestRun.run.status}
                        </div>
                    </div>
                    <p className="sandbox-test-run-summary">
                        {sandboxTestRun.run.status === 'running'
                            ? (sandboxTestRun.steps.some((step) => (step as deploy.SandboxTestStepResult & {status?: string}).status === 'running') ? 'Executing test step' : 'Building and starting services')
                            : sandboxTestRun.run.error || 'Test run complete'}
                    </p>
                    <div className="sandbox-test-run-steps">
                        {sandboxTestRun.steps.map((step, index) => {
                            const state = (step as deploy.SandboxTestStepResult & {status?: string}).status || (step.error || step.exitCode !== 0 ? 'failed' : 'passed');
                            const Icon = state === 'running' ? LoaderCircle : state === 'passed' ? CheckCircle2 : state === 'failed' ? XCircle : Circle;
                            return <div className={`sandbox-test-run-step sandbox-test-run-step--${state}`} key={`${step.name}-${index}`}>
                                <Icon size={14}/>
                                <span>{step.name || step.serviceLabel}</span>
                                <small>{step.serviceLabel}</small>
                                {state === 'passed' || state === 'failed' ? <em>{step.exitCode >= 0 ? `exit ${step.exitCode} · ` : 'exit unavailable · '}{step.durationMs}ms</em> : <em>{state}</em>}
                                {step.output && <pre>{step.output.replace(/[\u0000-\u0008\u000B\u000C\u000E-\u001F\u007F]/g, '')}</pre>}
                                {step.error && <p>{step.error}</p>}
                            </div>;
                        })}
                    </div>
                    <div className="sandbox-test-run-panel-actions">
                        <button className="btn btn-ghost" disabled={sandboxTestAction !== null} onClick={() => void rerunSandboxTest('steps')}><Play size={14}/>{sandboxTestAction === 'steps' ? 'Starting…' : 'Re-run steps'}</button>
                        <button className="btn btn-primary" disabled={sandboxTestAction !== null} onClick={() => void rerunSandboxTest('fresh')}><RotateCcw size={14}/>{sandboxTestAction === 'fresh' ? 'Starting…' : 'Rerun fresh'}</button>
                        <button className="btn btn-ghost" onClick={() => setSandboxTestPanelOpen(false)}><ChevronRight size={14}/> Close monitor</button>
                        {sandboxTestRun.run.status !== 'running' && <button className="icon-button" onClick={() => { setDismissedSandboxTestRunId(sandboxTestRun.run.id); setCanvasSandboxTestRunId(null); onDismissSandboxTestRun?.(); }} aria-label="Dismiss test run">×</button>}
                    </div>
                </aside>
                </ResizablePanel>
            )}

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
                        environmentId={environmentId}
                        onClose={() => setSelectedNodeId(null)}
                        onRename={renameNode}
                        onServicesChanged={notifyServicesChanged}
                        onServiceDeleted={handleServiceDeleted}
                        onOpenRootService={onOpenLinkedRootService}
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

            {showDraftPackImport && (
                <ImportDraftPackDialog
                    projectId={project.id}
                    environmentId={environmentId}
                    onClose={() => setShowDraftPackImport(false)}
                    onImported={() => {
                        setShowDraftPackImport(false);
                        void reloadCanvasNodes();
                        notifyServicesChanged();
                    }}
                />
            )}

            {showDraftPackExport && (
                <ExportDraftPackDialog
                    scope="project"
                    label={project.name}
                    projectId={project.id}
                    onClose={() => setShowDraftPackExport(false)}
                />
            )}
        </div>
    );
}
