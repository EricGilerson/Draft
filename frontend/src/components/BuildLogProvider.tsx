import {createContext, useContext, useEffect, useRef, useState, type ReactNode} from 'react';
import {EventsOn} from '../../wailsjs/runtime/runtime';

type NodeBuildState = {
    lines: string[];
    deploying: boolean;
    version: number;
};

const DEFAULT_STATE: NodeBuildState = {lines: [], deploying: false, version: 0};

type BuildLogStore = {
    nodes: Map<string, NodeBuildState>;
    listeners: Set<() => void>;
};

const BuildLogContext = createContext<BuildLogStore>({
    nodes: new Map(),
    listeners: new Set(),
});

export function useBuildLog(nodeId: string): NodeBuildState {
    const store = useContext(BuildLogContext);
    const [, rerender] = useState(0);

    useEffect(() => {
        const listener = () => rerender(n => n + 1);
        store.listeners.add(listener);
        return () => { store.listeners.delete(listener); };
    }, [store]);

    return store.nodes.get(nodeId) ?? DEFAULT_STATE;
}

export function BuildLogProvider({children}: {children: ReactNode}) {
    const storeRef = useRef<BuildLogStore>({
        nodes: new Map(),
        listeners: new Set(),
    });

    const notify = () => {
        storeRef.current.listeners.forEach(fn => fn());
    };

    const getOrCreate = (nodeId: string): NodeBuildState => {
        let s = storeRef.current.nodes.get(nodeId);
        if (!s) {
            s = {lines: [], deploying: false, version: 0};
            storeRef.current.nodes.set(nodeId, s);
        }
        return s;
    };

    useEffect(() => {
        const unsubStatus = EventsOn('deploy:status', (payload: any) => {
            const nodeId: string = payload.nodeId;
            const ev = payload.event;
            const s = getOrCreate(nodeId);
            s.deploying = ev.status === 'building' || ev.status === 'starting';
            s.version++;
            if (ev.status === 'building') {
                s.lines = [];
            }
            notify();
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

        return () => { unsubStatus(); unsubBuild(); };
    }, []);

    return (
        <BuildLogContext.Provider value={storeRef.current}>
            {children}
        </BuildLogContext.Provider>
    );
}
