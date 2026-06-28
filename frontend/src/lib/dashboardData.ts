import {store} from '../../wailsjs/go/models';

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
};

export const SERVICE_COLORS: Record<ServiceType, string> = {
    web: '#62b8ff',
    database: '#b9a0ff',
    cache: '#7bd88f',
    worker: '#f2bd4b',
};

export const STATUS_COLORS: Record<ServiceStatus, string> = {
    running: '#7bd88f',
    stopped: '#5c5749',
    error: '#ff675f',
    starting: '#f2bd4b',
};

const SERVICE_LIBRARY: Array<{type: ServiceType; name: string; image: string; port: number}> = [
    {type: 'web', name: 'web', image: 'node:20-alpine', port: 3000},
    {type: 'database', name: 'postgres', image: 'postgres:16', port: 5432},
    {type: 'cache', name: 'redis', image: 'redis:7-alpine', port: 6379},
    {type: 'worker', name: 'worker', image: 'node:20-alpine', port: 4100},
];

const SANDBOX_BRANCHES = [
    'feature/auth-flow',
    'fix/port-collision',
    'chore/env-sync',
    'feat/realtime-preview',
];

const ELAPSED_LABELS = ['2m ago', '12m ago', '48m ago', '2h ago', 'Yesterday'];

function hashString(value: string): number {
    let hash = 0;
    for (let i = 0; i < value.length; i += 1) {
        hash = (hash * 31 + value.charCodeAt(i)) >>> 0;
    }
    return hash;
}

function titleSlug(value: string) {
    return value
        .trim()
        .toLowerCase()
        .replace(/[^a-z0-9]+/g, '-')
        .replace(/^-+|-+$/g, '') || 'project';
}

function primaryLabel(project: store.Project) {
    return titleSlug(project.name).replace(/-service$/, '') || 'app';
}

function buildServiceSet(project: store.Project, hash: number): ServicePreview[] {
    const includeCache = hash % 2 === 0;
    const includeWorker = hash % 3 === 0;
    const base = SERVICE_LIBRARY.filter((service) => {
        if (service.type === 'cache') return includeCache;
        if (service.type === 'worker') return includeWorker;
        return true;
    });

    const statuses: ServiceStatus[] = ['running', 'running', 'starting', 'stopped'];
    return base.map((service, index) => {
        const portOffset = (hash % 120) + index * 17;
        const status = statuses[(hash + index) % statuses.length];
        const name = service.type === 'web' ? primaryLabel(project) : service.name;

        return {
            id: `${project.id}-${service.type}`,
            name,
            type: service.type,
            image: service.image,
            port: service.port + portOffset,
            status,
        };
    });
}

function summarizeStatus(services: ServicePreview[]): ProjectStatus {
    const running = services.filter((service) => service.status === 'running').length;
    if (running === 0) return 'stopped';
    if (running === services.length) return 'active';
    return 'partial';
}

export function decorateProjects(projects: store.Project[]): ProjectSummary[] {
    return projects.map((project, index) => {
        const hash = hashString(`${project.name}:${project.path}:${project.description}:${index}`);
        const services = buildServiceSet(project, hash);
        return {
            project,
            services,
            status: summarizeStatus(services),
            lastActive: ELAPSED_LABELS[hash % ELAPSED_LABELS.length],
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
