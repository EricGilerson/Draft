import {ArrowRight, Boxes, FolderPlus, FlaskConical, Layers3} from 'lucide-react';
import {store} from '../../wailsjs/go/models';
import PageHeader from '../components/PageHeader';
import ServicePill from '../components/ServicePill';
import {ActivityPreview, ProjectSummary, SERVICE_COLORS, STATUS_COLORS} from '../lib/dashboardData';
import './WorkspaceViews.css';

type OverviewViewProps = {
    loading: boolean;
    projects: ProjectSummary[];
    activity: ActivityPreview[];
    onCreateProject: () => void;
    onOpenProject: (project: store.Project, environmentId?: number) => void;
};

const ACTIVITY_COLORS: Record<ActivityPreview['type'], string> = {
    start: STATUS_COLORS.running,
    stop: STATUS_COLORS.stopped,
    port: STATUS_COLORS.starting,
    env: SERVICE_COLORS.web,
    sandbox: SERVICE_COLORS.worker,
    error: STATUS_COLORS.error,
};

function formatRelative(ts?: number): string | null {
    if (!ts) return null;
    const seconds = Math.max(0, Math.floor((Date.now() - ts) / 1000));
    if (seconds < 5) return 'Just now';
    if (seconds < 60) return `${seconds}s ago`;
    const minutes = Math.floor(seconds / 60);
    if (minutes < 60) return `${minutes}m ago`;
    const hours = Math.floor(minutes / 60);
    if (hours < 24) return `${hours}h ago`;
    const days = Math.floor(hours / 24);
    if (days === 1) return 'Yesterday';
    return `${days}d ago`;
}

function CanvasPreview({project}: {project?: ProjectSummary}) {
    const services = project?.services ?? [
        {id: 'preview-web', name: 'web', type: 'web', image: '', port: 3000, status: 'running'},
        {id: 'preview-db', name: 'postgres', type: 'database', image: '', port: 5432, status: 'running'},
        {id: 'preview-cache', name: 'redis', type: 'cache', image: '', port: 6379, status: 'running'},
    ];

    const layout = services.slice(0, 3).map((service, index) => {
        if (index === 0) return {service, x: 38, y: 106};
        if (index === 1) return {service, x: 328, y: 48};
        return {service, x: 328, y: 168};
    });

    const primary = layout[0];

    return (
        <div className="canvas-preview">
            <div className="canvas-preview-glow"/>
            <svg className="canvas-preview-edges" viewBox="0 0 520 280" preserveAspectRatio="none" aria-hidden="true">
                {layout.slice(1).map(({service}, index) => {
                    const color = SERVICE_COLORS[service.type];
                    const endY = index === 0 ? 79 : 199;
                    const d = `M 186 137 C 252 137, 280 ${endY}, 328 ${endY}`;
                    return (
                        <g key={service.id}>
                            <path d={d} fill="none" stroke={color} strokeWidth="14" opacity="0.05"/>
                            <path d={d} fill="none" stroke={color} strokeWidth="1.7" opacity="0.35" strokeDasharray="6 5"/>
                        </g>
                    );
                })}
            </svg>

            {layout.map(({service, x, y}) => {
                const color = SERVICE_COLORS[service.type];
                return (
                    <div
                        key={service.id}
                        className="canvas-preview-node"
                        style={{
                            left: x,
                            top: y,
                            borderTopColor: color,
                            background: `linear-gradient(160deg, ${color}14 0%, rgba(18, 19, 15, 0.96) 58%)`,
                        }}
                    >
                        <div className="canvas-preview-node-title">
                            <span className="canvas-preview-node-dot" style={{background: color}}/>
                            <span>{service.name}</span>
                        </div>
                        <div className="canvas-preview-node-meta">
                            <span>:{service.port}</span>
                            <span
                                className="canvas-preview-node-status"
                                style={{background: STATUS_COLORS[service.status]}}
                            />
                        </div>
                    </div>
                );
            })}

            <div className="canvas-preview-label">
                <span>{project ? project.project.name : 'Canvas preview'}</span>
                {primary && <span>{primary.service.image || 'manifest-derived topology'}</span>}
            </div>
        </div>
    );
}

export default function OverviewView({
    loading,
    projects,
    activity,
    onCreateProject,
    onOpenProject,
}: OverviewViewProps) {
    const runningServices = projects.flatMap((project) => project.services).filter((service) => service.status === 'running').length;
    const totalServices = projects.flatMap((project) => project.services).length;
    const totalEnvironments = projects.reduce((n, project) => n + (project.environmentCount || project.environments.length), 0);
    const activeProjects = projects.filter((project) => project.status !== 'stopped').length;
    const highlighted = projects[0];
    const highlightedServices =
        highlighted?.environments.find((e) => e.isDefault)?.services
        ?? highlighted?.services
        ?? [];

    return (
        <div className="workspace-view">
            <PageHeader
                title="Overview"
                description="A calmer control surface for local environments, ports, and deployments."
                action={
                    <button className="btn btn-primary" onClick={onCreateProject}>
                        <FolderPlus size={15}/> Add project
                    </button>
                }
            />

            <div className="workspace-body">
                <div className="stats-grid">
                    <div className="metric-card">
                        <div className="metric-label">Projects</div>
                        <div className="metric-value">{loading ? '...' : projects.length}</div>
                        <div className="metric-subtle">{activeProjects} active right now</div>
                    </div>
                    <div className="metric-card">
                        <div className="metric-label">Environments</div>
                        <div className="metric-value">{loading ? '...' : totalEnvironments}</div>
                        <div className="metric-subtle">Across all projects</div>
                    </div>
                    <div className="metric-card">
                        <div className="metric-label">Services</div>
                        <div className="metric-value">{loading ? '...' : totalServices}</div>
                        <div className="metric-subtle">{runningServices} running</div>
                    </div>
                </div>

                <div className="overview-grid">
                    <section className="panel panel-emphasis">
                        <div className="panel-header">
                            <div>
                                <h2 className="panel-title">Canvas direction</h2>
                                <p className="panel-description">
                                    {highlighted
                                        ? `Using ${highlighted.project.name} as the current styling reference for topology views.`
                                        : 'The canvas styling is ready; connect a project to drive it with real topology data later.'}
                                </p>
                            </div>
                            {highlighted && (
                                <button className="btn btn-ghost" onClick={() => onOpenProject(highlighted.project)}>
                                    Open canvas <ArrowRight size={14}/>
                                </button>
                            )}
                        </div>
                        <CanvasPreview project={highlighted ? {...highlighted, services: highlightedServices} : undefined}/>
                    </section>

                    <section className="panel">
                        <div className="panel-header">
                            <div>
                                <h2 className="panel-title">Project focus</h2>
                                <p className="panel-description">Environments stay one click from the canvas.</p>
                            </div>
                        </div>
                        <div className="stack-list">
                            {projects.length === 0 ? (
                                <div className="panel-empty">
                                    <Boxes size={18}/>
                                    <span>No projects yet. Add one to populate the dashboard.</span>
                                </div>
                            ) : (
                                projects.slice(0, 3).map((project) => (
                                    <div key={project.project.id} className="overview-project-block">
                                        <button
                                            className="list-row-button"
                                            onClick={() => onOpenProject(project.project)}
                                        >
                                            <div className="list-row-main">
                                                <div className="list-row-title">
                                                    <span
                                                        className="status-dot"
                                                        style={{background: project.status === 'active' ? STATUS_COLORS.running : project.status === 'partial' ? STATUS_COLORS.starting : STATUS_COLORS.stopped}}
                                                    />
                                                    <span>{project.project.name}</span>
                                                </div>
                                                <div className="list-row-subtle">
                                                    {project.environmentCount || project.environments.length} env
                                                    {(project.environmentCount || project.environments.length) === 1 ? '' : 's'}
                                                    {' · '}
                                                    {project.services.length} services
                                                </div>
                                            </div>
                                        </button>
                                        <div className="overview-env-chips">
                                            {project.environments.map((env) => (
                                                <button
                                                    key={env.id || env.slug}
                                                    type="button"
                                                    className="overview-env-chip"
                                                    onClick={() => onOpenProject(project.project, env.id || undefined)}
                                                >
                                                    <span
                                                        className="status-dot"
                                                        style={{background: env.status === 'active' ? STATUS_COLORS.running : env.status === 'partial' ? STATUS_COLORS.starting : STATUS_COLORS.stopped}}
                                                    />
                                                    <span>{env.name}</span>
                                                    <span className="overview-env-chip-meta">
                                                        {env.running}↑ {env.stopped}↓
                                                    </span>
                                                </button>
                                            ))}
                                        </div>
                                        <div className="list-row-pills overview-service-pills">
                                            {project.services.slice(0, 4).map((service) => (
                                                <ServicePill key={service.id} service={service}/>
                                            ))}
                                        </div>
                                    </div>
                                ))
                            )}
                        </div>
                    </section>
                </div>

                <div className="overview-grid overview-grid-bottom">
                    <section className="panel">
                        <div className="panel-header">
                            <div>
                                <h2 className="panel-title">Recent activity</h2>
                                <p className="panel-description">Live deploy and Docker events from this machine.</p>
                            </div>
                        </div>
                        <div className="activity-list">
                            {activity.length === 0 ? (
                                <div className="panel-empty">
                                    <Boxes size={18}/>
                                    <span>No activity yet. Deploy a service to populate the feed.</span>
                                </div>
                            ) : (
                                activity.map((item) => (
                                    <div key={item.id} className="activity-row">
                                        <span className="activity-dot" style={{background: ACTIVITY_COLORS[item.type]}}/>
                                        <span className="activity-time">{formatRelative(item.ts) ?? item.time}</span>
                                        <span className="activity-project">{item.project}</span>
                                        <span className="activity-message">{item.message}</span>
                                    </div>
                                ))
                            )}
                        </div>
                    </section>

                    <section className="panel">
                        <div className="panel-header">
                            <div>
                                <h2 className="panel-title">What changed in the redesign</h2>
                                <p className="panel-description">The global shell now matches the Figma language across current and planned surfaces.</p>
                            </div>
                        </div>
                        <div className="callout-list">
                            <div className="callout-row">
                                <Layers3 size={15}/>
                                <span>Denser panels, stronger hierarchy, and a quieter chrome.</span>
                            </div>
                            <div className="callout-row">
                                <Boxes size={15}/>
                                <span>Projects now open into a styled topology workspace instead of an empty flow grid.</span>
                            </div>
                            <div className="callout-row">
                                <FlaskConical size={15}/>
                                <span>Sandboxes and settings are designed now, with explicit UI-only scaffolding where the backend is not ready.</span>
                            </div>
                        </div>
                    </section>
                </div>
            </div>
        </div>
    );
}
