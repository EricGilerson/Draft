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

type BuildLogStore = {
    nodes: Map<string, NodeBuildState>;
    listeners: Set<() => void>;
    setPendingAction: (nodeId: string, action: PendingAction) => void;
};

const BuildLogContext = createContext<BuildLogStore>({
    nodes: new Map(),
    listeners: new Set(),
    setPendingAction: () => {},
});

export function useBuildLog(nodeId: string): NodeBuildState & {setPendingAction: (action: PendingAction) => void} {
    const store = useContext(BuildLogContext);
    const [, rerender] = useState(0);

    useEffect(() => {
        const listener = () => rerender(n => n + 1);
        store.listeners.add(listener);
        return () => { store.listeners.delete(listener); };
    }, [store]);

    const state = store.nodes.get(nodeId) ?? DEFAULT_STATE;
    return {...state, setPendingAction: (action: PendingAction) => store.setPendingAction(nodeId, action)};
}

export function BuildLogProvider({children}: {children: ReactNode}) {
    const storeRef = useRef<BuildLogStore>({
        nodes: new Map(),
        listeners: new Set(),
        setPendingAction: () => {},
    });

    const notify = () => {
        storeRef.current.listeners.forEach(fn => fn());
    };

    const getOrCreate = (nodeId: string): NodeBuildState => {
        let s = storeRef.current.nodes.get(nodeId);
        if (!s) {
            s = {lines: [], deploying: false, version: 0, pendingAction: null, uploadProgress: null};
            storeRef.current.nodes.set(nodeId, s);
        }
        return s;
    };

    const setPendingAction = (nodeId: string, action: PendingAction) => {
        const s = getOrCreate(nodeId);
        s.pendingAction = action;
        notify();
    };

    storeRef.current.setPendingAction = setPendingAction;

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
                s.lines = [];
                s.uploadProgress = null;
            }
            notify();
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
                    notify();
                }).catch(() => {});
            }
        });

        const unsubBuild = EventsOn('build:log', (payload: any) => {
            const nodeId: string = payload.nodeId;
            const logLine = payload.line;
            const s = getOrCreate(nodeId);
            s.lines = [...s.lines, logLine.line];
            if (s.lines.length > 2000) {
                s.lines = s.lines.slice(-1500);
            }
            notify();
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
            notify();
        });

        return () => { unsubStatus(); unsubBuild(); unsubUpload(); };
    }, []);

    return (
        <BuildLogContext.Provider value={storeRef.current}>
            {children}
        </BuildLogContext.Provider>
    );
}
