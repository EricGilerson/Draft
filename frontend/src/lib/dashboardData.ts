import {main, store} from '../../wailsjs/go/models';

export type ServiceType = 'web' | 'database' | 'cache' | 'worker';
export type ServiceStatus = 'running' | 'stopped' | 'error' | 'starting';
export type ProjectStatus = 'active' | 'partial' | 'stopped';

export type ServicePreview = {
    id: string;
    name: string;
    type: ServiceType;
    image: string;
    port?: number;
    status: ServiceStatus;
};

export type ProjectSummary = {
    project: store.Project;
    status: ProjectStatus;
    lastActive: string;
    services: ServicePreview[];
};

export type SandboxPreview = {
    id: string;
    branch: string;
    forkedFrom: string;
    status: ServiceStatus;
    createdAt: string;
    services: ServicePreview[];
    previewOnly?: boolean;
};

export type ActivityPreview = {
    id: string;
    time: string;
    project: string;
    message: string;
    type: 'start' | 'stop' | 'port' | 'env' | 'sandbox' | 'error';
    ts?: number;
};

export const SERVICE_COLORS: Record<ServiceType, string> = {
    web: '#8c9690',
    database: '#b8a06a',
    cache: '#68bd78',
    worker: '#c08b75',
};

export const STATUS_COLORS: Record<ServiceStatus, string> = {
    running: '#68bd78',
    stopped: '#5c5749',
    error: '#ef6f68',
    starting: '#d8b75c',
};

const SANDBOX_BRANCHES = [
    'feature/auth-flow',
    'fix/port-collision',
    'chore/env-sync',
    'feat/realtime-preview',
];

const ELAPSED_LABELS = ['2m ago', '12m ago', '48m ago', '2h ago', 'Yesterday'];

function coerceServiceType(type: string): ServiceType {
    if (type === 'database' || type === 'cache' || type === 'worker') return type;
    return 'web';
}

function coerceServiceStatus(status: string): ServiceStatus {
    if (status === 'running' || status === 'starting' || status === 'error') return status;
    return 'stopped';
}

function asDate(value: any): Date | null {
    if (!value) return null;
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? null : date;
}

function relativeLabel(value: any): string {
    const date = asDate(value);
    if (!date) return 'No activity yet';
    const seconds = Math.max(0, Math.floor((Date.now() - date.getTime()) / 1000));
    if (seconds < 60) return 'Just now';
    const minutes = Math.floor(seconds / 60);
    if (minutes < 60) return `${minutes}m ago`;
    const hours = Math.floor(minutes / 60);
    if (hours < 24) return `${hours}h ago`;
    const days = Math.floor(hours / 24);
    if (days === 1) return 'Yesterday';
    return `${days}d ago`;
}

function servicePreview(service: main.ProjectService): ServicePreview {
    return {
        id: service.id,
        name: service.name || service.id,
        type: coerceServiceType(service.type),
        image: service.image || service.dockerfile || 'unconfigured',
        port: service.port || undefined,
        status: coerceServiceStatus(service.status),
    };
}

function summarizeStatus(services: ServicePreview[]): ProjectStatus {
    if (services.length === 0) return 'stopped';
    const running = services.filter((service) => service.status === 'running').length;
    if (running === 0) return 'stopped';
    if (running === services.length) return 'active';
    return 'partial';
}

export function decorateProjects(projects: store.Project[], servicesByProject: Record<number, main.ProjectService[]> = {}): ProjectSummary[] {
    return projects.map((project) => {
        const rawServices = servicesByProject[project.id] ?? [];
        const services = rawServices.map(servicePreview);
        const latestServiceUpdate = rawServices
            .map((service) => asDate(service.updatedAt))
            .filter((date): date is Date => Boolean(date))
            .sort((a, b) => b.getTime() - a.getTime())[0];
        return {
            project,
            services,
            status: summarizeStatus(services),
            lastActive: services.length === 0 ? 'No services' : relativeLabel(latestServiceUpdate ?? project.updatedAt),
        };
    });
}

export function buildSandboxPreviews(projects: ProjectSummary[]): SandboxPreview[] {
    if (projects.length === 0) {
        return [
            {
                id: 'preview-sandbox',
                branch: 'feature/local-preview',
                forkedFrom: 'sample-project',
                status: 'starting',
                createdAt: 'Design preview',
                previewOnly: true,
                services: [
                    {id: 'preview-web', name: 'web', type: 'web', image: 'node:20-alpine', port: 3100, status: 'starting'},
                    {id: 'preview-db', name: 'postgres', type: 'database', image: 'postgres:16', port: 5532, status: 'starting'},
                ],
            },
        ];
    }

    return projects.slice(0, 3).map((project, index) => ({
        id: `sandbox-${project.project.id}`,
        branch: SANDBOX_BRANCHES[index % SANDBOX_BRANCHES.length],
        forkedFrom: project.project.name,
        status: index === 0 ? 'running' : index === 1 ? 'stopped' : 'starting',
        createdAt: ELAPSED_LABELS[(index + 1) % ELAPSED_LABELS.length],
        services: project.services.map((service, serviceIndex) => ({
            ...service,
            id: `sandbox-${project.project.id}-${service.type}`,
            port: service.port ? service.port + 100 + serviceIndex * 11 : undefined,
            status: index === 2 ? 'starting' : serviceIndex === 0 ? 'running' : service.status,
        })),
    }));
}

export function buildActivity(projects: ProjectSummary[], sandboxes: SandboxPreview[]): ActivityPreview[] {
    if (projects.length === 0) {
        return [
            {
                id: 'activity-preview',
                time: 'Now',
                project: 'Draft',
                message: 'Create a project to start tracking services, ports, and sandbox activity.',
                type: 'env',
            },
        ];
    }

    const items: ActivityPreview[] = [];
    projects.slice(0, 3).forEach((project, index) => {
        const primary = project.services[0];
        if (primary?.port) {
            items.push({
                id: `activity-start-${project.project.id}`,
                time: ELAPSED_LABELS[index % ELAPSED_LABELS.length],
                project: project.project.name,
                message: `${primary.name}:${primary.port} ${primary.status === 'running' ? 'ready' : 'staged for start'}`,
                type: primary.status === 'running' ? 'start' : 'port',
            });
        }
        items.push({
            id: `activity-env-${project.project.id}`,
            time: ELAPSED_LABELS[(index + 2) % ELAPSED_LABELS.length],
            project: project.project.name,
            message: `.env targets resolved for ${project.services.length} services`,
            type: 'env',
        });
    });

    sandboxes.slice(0, 2).forEach((sandbox, index) => {
        items.push({
            id: `activity-sandbox-${sandbox.id}`,
            time: ELAPSED_LABELS[(index + 1) % ELAPSED_LABELS.length],
            project: sandbox.forkedFrom,
            message: `Sandbox "${sandbox.branch}" ${sandbox.status === 'running' ? 'available' : 'prepared'}`,
            type: 'sandbox',
        });
    });

    return items.slice(0, 6);
}

export function projectStatusColor(status: ProjectStatus) {
    if (status === 'active') return STATUS_COLORS.running;
    if (status === 'partial') return STATUS_COLORS.starting;
    return STATUS_COLORS.stopped;
}
