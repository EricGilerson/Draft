import {useEffect, useRef, useState} from 'react';
import {EventsOn} from '../wailsjs/runtime/runtime';
import {main, store} from '../wailsjs/go/models';
import {summarize as summarizeDocker} from '../components/ActivityTicker';
import {ActivityPreview} from './dashboardData';

const MAX_ENTRIES = 60;

type DockerEvent = {
    type: string;
    action: string;
    actor: string;
    name: string;
    image: string;
};

type DeployPayload = {
    nodeId: string;
    event?: {
        deploymentId?: number;
        status?: string;
        hostname?: string;
        hostPort?: number;
        error?: string;
    };
};

type NodeRef = {projectName: string; serviceName: string};

function resolveNode(
    projects: store.Project[],
    servicesByProject: Record<number, main.ProjectService[]>,
    nodeId: string,
): NodeRef | null {
    if (!nodeId) return null;
    for (const project of projects) {
        const services = servicesByProject[project.id] ?? [];
        const svc = services.find((s) => s.id === nodeId);
        if (svc) {
            return {projectName: project.name, serviceName: svc.name || svc.id};
        }
    }
    return null;
}

function mapDeployType(status: string): ActivityPreview['type'] {
    switch (status) {
        case 'running':
        case 'starting':
        case 'built':
            return 'start';
        case 'failed':
        case 'interrupted':
            return 'error';
        case 'stopped':
            return 'stop';
        case 'building':
            return 'port';
        default:
            return 'env';
    }
}

function mapDeployMessage(status: string, serviceName: string, errMsg?: string): string | null {
    switch (status) {
        case 'building':
            return `${serviceName} building`;
        case 'built':
            return `${serviceName} image built`;
        case 'starting':
            return `${serviceName} starting`;
        case 'running':
            return `${serviceName} deployed (running)`;
        case 'stopped':
            return `${serviceName} stopped`;
        case 'failed':
            return errMsg ? `${serviceName} deploy failed: ${errMsg}` : `${serviceName} deploy failed`;
        case 'interrupted':
            return `${serviceName} deploy cancelled`;
        default:
            return null;
    }
}

function mapDockerType(ev: DockerEvent): ActivityPreview['type'] {
    if (ev.type === 'container') {
        if (ev.action === 'start' || ev.action === 'restart' || ev.action === 'unpause') return 'start';
        if (ev.action === 'kill') return 'error';
        if (ev.action === 'stop' || ev.action === 'die' || ev.action === 'destroy' || ev.action === 'pause') return 'stop';
        return 'env';
    }
    if (ev.type === 'image') {
        if (ev.action === 'pull' || ev.action === 'build' || ev.action === 'tag') return 'port';
        return 'stop';
    }
    return 'env';
}

function uniqueId(prefix: string): string {
    return `${prefix}-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
}

/**
 * useActivityLog captures deploy:status and docker:activity SSE events into a
 * ring buffer that persists for the lifetime of the app (the hook stays mounted
 * at the App root), so the Overview activity feed survives view switches.
 */
export function useActivityLog(
    projects: store.Project[],
    servicesByProject: Record<number, main.ProjectService[]>,
): ActivityPreview[] {
    const [entries, setEntries] = useState<ActivityPreview[]>([]);
    const projectsRef = useRef(projects);
    const servicesRef = useRef(servicesByProject);
    projectsRef.current = projects;
    servicesRef.current = servicesByProject;

    useEffect(() => {
        const add = (entry: ActivityPreview) => {
            setEntries((prev) => [entry, ...prev].slice(0, MAX_ENTRIES));
        };

        const unsubDeploy = EventsOn('deploy:status', (payload: DeployPayload) => {
            const nodeId = payload?.nodeId;
            const ev = payload?.event ?? {};
            const status = ev.status ?? '';
            const resolved = resolveNode(projectsRef.current, servicesRef.current, nodeId);
            const projectName = resolved?.projectName ?? 'Draft';
            const serviceName = resolved?.serviceName ?? 'service';
            const message = mapDeployMessage(status, serviceName, ev.error);
            if (!message) return;
            add({
                id: uniqueId('deploy'),
                ts: Date.now(),
                time: 'Just now',
                project: projectName,
                message,
                type: mapDeployType(status),
            });
        });

        const unsubDocker = EventsOn('docker:activity', (ev: DockerEvent) => {
            const text = summarizeDocker(ev);
            if (!text) return;
            add({
                id: uniqueId('docker'),
                ts: Date.now(),
                time: 'Just now',
                project: 'Docker',
                message: text,
                type: mapDockerType(ev),
            });
        });

        return () => {
            unsubDeploy();
            unsubDocker();
        };
    }, []);

    return entries;
}
