import {ArrowRight, Boxes, FolderPlus, FlaskConical, Layers3} from 'lucide-react';
import {store} from '../../wailsjs/go/models';
import PageHeader from '../components/PageHeader';
import ServicePill from '../components/ServicePill';
import {ActivityPreview, ProjectSummary, SandboxPreview, SERVICE_COLORS, STATUS_COLORS} from '../lib/dashboardData';
import './WorkspaceViews.css';

type OverviewViewProps = {
    loading: boolean;
    projects: ProjectSummary[];
    sandboxes: SandboxPreview[];
    activity: ActivityPreview[];
    onCreateProject: () => void;
    onOpenProject: (project: store.Project) => void;
};

const ACTIVITY_COLORS: Record<ActivityPreview['type'], string> = {
    start: STATUS_COLORS.running,
    stop: STATUS_COLORS.stopped,
    port: STATUS_COLORS.starting,
    env: SERVICE_COLORS.web,
    sandbox: SERVICE_COLORS.worker,
    error: STATUS_COLORS.error,
};

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
    sandboxes,
    activity,
    onCreateProject,
    onOpenProject,
}: OverviewViewProps) {
    const runningServices = projects.flatMap((project) => project.services).filter((service) => service.status === 'running').length;
    const totalServices = projects.flatMap((project) => project.services).length;
    const activeProjects = projects.filter((project) => project.status !== 'stopped').length;
    const liveSandboxes = sandboxes.filter((sandbox) => sandbox.status === 'running').length;
    const highlighted = projects[0];

    return (
        <div className="workspace-view">
            <PageHeader
                title="Overview"
                description="A calmer control surface for local environments, ports, and upcoming sandboxes."
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
                        <div className="metric-label">Services</div>
                        <div className="metric-value">{loading ? '...' : totalServices}</div>
                        <div className="metric-subtle">{runningServices} running</div>
                    </div>
                    <div className="metric-card">
                        <div className="metric-label">Sandboxes</div>
                        <div className="metric-value">{sandboxes.length}</div>
                        <div className="metric-subtle">{liveSandboxes} live preview</div>
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
                        <CanvasPreview project={highlighted}/>
                    </section>

                    <section className="panel">
                        <div className="panel-header">
                            <div>
                                <h2 className="panel-title">Project focus</h2>
                                <p className="panel-description">The most relevant environments should stay one click from the canvas.</p>
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
                                    <button
                                        key={project.project.id}
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
                                            <div className="list-row-subtle">{project.project.path}</div>
                                        </div>
                                        <div className="list-row-pills">
                                            {project.services.slice(0, 2).map((service) => (
                                                <ServicePill key={service.id} service={service}/>
                                            ))}
                                        </div>
                                    </button>
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
                                <p className="panel-description">This is currently a frontend preview fed from project metadata.</p>
                            </div>
                        </div>
                        <div className="activity-list">
                            {activity.map((item) => (
                                <div key={item.id} className="activity-row">
                                    <span className="activity-dot" style={{background: ACTIVITY_COLORS[item.type]}}/>
                                    <span className="activity-time">{item.time}</span>
                                    <span className="activity-project">{item.project}</span>
                                    <span className="activity-message">{item.message}</span>
                                </div>
                            ))}
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
