import {createContext, useContext, useEffect, useRef, useState, type ReactNode} from 'react';
import {GetBuildLog} from '../../wailsjs/go/main/App';
import {EventsOn} from '../../wailsjs/runtime/runtime';

type PendingAction = 'stopping' | 'restarting' | 'deploying' | null;

type UploadProgress = {
    percent: number;
    sentBytes: number;
    totalBytes: number;
    indeterminate: boolean;
} | null;

type NodeBuildState = {
    lines: string[];
    deploying: boolean;
    version: number;
    pendingAction: PendingAction;
    uploadProgress: UploadProgress;
};

const DEFAULT_STATE: NodeBuildState = {lines: [], deploying: false, version: 0, pendingAction: null, uploadProgress: null};
const MAX_NODE_ENTRIES = 48;

type BuildLogStore = {
    nodes: Map<string, NodeBuildState>;
    listeners: Map<string, Set<() => void>>;
    setPendingAction: (nodeId: string, action: PendingAction) => void;
};

const BuildLogContext = createContext<BuildLogStore>({
    nodes: new Map(),
    listeners: new Map(),
    setPendingAction: () => {},
});

export function useBuildLog(nodeId: string): NodeBuildState & {setPendingAction: (action: PendingAction) => void} {
    const store = useContext(BuildLogContext);
    const [, rerender] = useState(0);

    useEffect(() => {
        const listener = () => rerender(n => n + 1);
        let set = store.listeners.get(nodeId);
        if (!set) {
            set = new Set();
            store.listeners.set(nodeId, set);
        }
        set.add(listener);
        return () => {
            set!.delete(listener);
            if (set!.size === 0) {
                store.listeners.delete(nodeId);
            }
        };
    }, [store, nodeId]);

    const state = store.nodes.get(nodeId) ?? DEFAULT_STATE;
    return {...state, setPendingAction: (action: PendingAction) => store.setPendingAction(nodeId, action)};
}

export function BuildLogProvider({children}: {children: ReactNode}) {
    const storeRef = useRef<BuildLogStore>({
        nodes: new Map(),
        listeners: new Map(),
        setPendingAction: () => {},
    });
    const pendingLinesRef = useRef(new Map<string, string[]>());
    const flushScheduledRef = useRef(false);

    const notify = (nodeId: string) => {
        storeRef.current.listeners.get(nodeId)?.forEach(fn => fn());
    };

    const getOrCreate = (nodeId: string): NodeBuildState => {
        let s = storeRef.current.nodes.get(nodeId);
        if (!s) {
            s = {lines: [], deploying: false, version: 0, pendingAction: null, uploadProgress: null};
            storeRef.current.nodes.set(nodeId, s);
            pruneIdleNodes();
        }
        return s;
    };

    const pruneIdleNodes = () => {
        const nodes = storeRef.current.nodes;
        if (nodes.size <= MAX_NODE_ENTRIES) return;
        for (const [id, state] of nodes) {
            if (nodes.size <= MAX_NODE_ENTRIES) break;
            if (state.deploying) continue;
            if ((storeRef.current.listeners.get(id)?.size ?? 0) > 0) continue;
            nodes.delete(id);
            pendingLinesRef.current.delete(id);
        }
    };

    const setPendingAction = (nodeId: string, action: PendingAction) => {
        const s = getOrCreate(nodeId);
        s.pendingAction = action;
        notify(nodeId);
    };

    storeRef.current.setPendingAction = setPendingAction;

    const flushPendingLines = () => {
        flushScheduledRef.current = false;
        const pending = pendingLinesRef.current;
        if (pending.size === 0) return;
        pendingLinesRef.current = new Map();
        for (const [nodeId, lines] of pending) {
            if (lines.length === 0) continue;
            const s = getOrCreate(nodeId);
            s.lines.push(...lines);
            if (s.lines.length > 2000) {
                s.lines = s.lines.slice(-1500);
            }
            notify(nodeId);
        }
    };

    const queueBuildLine = (nodeId: string, line: string) => {
        let bucket = pendingLinesRef.current.get(nodeId);
        if (!bucket) {
            bucket = [];
            pendingLinesRef.current.set(nodeId, bucket);
        }
        bucket.push(line);
        if (!flushScheduledRef.current) {
            flushScheduledRef.current = true;
            requestAnimationFrame(flushPendingLines);
        }
    };

    useEffect(() => {
        const unsubStatus = EventsOn('deploy:status', (payload: any) => {
            const nodeId: string = payload.nodeId;
            const ev = payload.event;
            const s = getOrCreate(nodeId);
            s.deploying = ev.status === 'building' || ev.status === 'starting';
            if (!s.deploying) {
                s.pendingAction = null;
            }
            s.version++;
            if (ev.status === 'building') {
                pendingLinesRef.current.delete(nodeId);
                s.lines = [];
                s.uploadProgress = null;
            }
            notify(nodeId);
            if ((ev.status === 'building' || ev.status === 'starting') && ev.deploymentId) {
                GetBuildLog(ev.deploymentId).then(log => {
                    const current = getOrCreate(nodeId);
                    if (current.lines.length > 0) return;
                    const lines = log.split('\n').filter(Boolean).map(line => {
                        try {
                            const obj = JSON.parse(line);
                            return (obj.error || obj.stream || obj.status || line).replace(/\n$/, '');
                        } catch {
                            return line;
                        }
                    });
                    current.lines = lines.slice(-1500);
                    notify(nodeId);
                }).catch(() => {});
            }
        });

        const unsubBuild = EventsOn('build:log', (payload: any) => {
            const nodeId: string = payload.nodeId;
            const logLine = payload.line;
            queueBuildLine(nodeId, logLine.line);
        });

        const unsubUpload = EventsOn('deploy:upload-progress', (payload: any) => {
            const nodeId: string = payload.nodeId;
            const s = getOrCreate(nodeId);
            if (payload.done) {
                s.uploadProgress = null;
            } else {
                s.uploadProgress = {
                    percent: payload.percent ?? 0,
                    sentBytes: payload.sentBytes ?? 0,
                    totalBytes: payload.totalBytes ?? 0,
                    indeterminate: !!payload.indeterminate,
                };
            }
            notify(nodeId);
        });

        return () => { unsubStatus(); unsubBuild(); unsubUpload(); };
    }, []);

    return (
        <BuildLogContext.Provider value={storeRef.current}>
            {children}
        </BuildLogContext.Provider>
    );
}
