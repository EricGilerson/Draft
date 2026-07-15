import {ArrowRight, Boxes, ChevronRight, FileUp, FolderPlus, Package, Settings} from 'lucide-react';
import {useState} from 'react';
import {store} from '../../wailsjs/go/models';
import PageHeader from '../components/PageHeader';
import ServicePill from '../components/ServicePill';
import {EnvironmentSummary, ProjectSummary, projectStatusColor} from '../lib/dashboardData';
import './ProjectsView.css';

type ProjectsViewProps = {
    loading: boolean;
    projects: ProjectSummary[];
    onCreateProject: () => void;
    onImportProject?: () => void;
    onImportDraftPack?: () => void;
    onOpenProject: (project: store.Project, environmentId?: number) => void;
    onOpenProjectSettings?: (project: store.Project) => void;
};

function envStatusLine(env: EnvironmentSummary): string {
    const parts: string[] = [];
    if (env.running > 0) parts.push(`${env.running} up`);
    if (env.building > 0) parts.push(`${env.building} building`);
    if (env.failed > 0) parts.push(`${env.failed} failed`);
    if (env.stopped > 0) parts.push(`${env.stopped} down`);
    if (parts.length === 0) return env.services.length === 0 ? 'No services' : 'Idle';
    return parts.join(' · ');
}

export default function ProjectsView({loading, projects, onCreateProject, onImportProject, onImportDraftPack, onOpenProject, onOpenProjectSettings}: ProjectsViewProps) {
    const [expanded, setExpanded] = useState<number | null>(projects[0]?.project.id ?? null);

    return (
        <div className="projects-view">
            <PageHeader
                title="Projects"
                description="Manage your local Docker environments."
                action={
                    <div className="projects-header-actions">
                        {onImportDraftPack && (
                            <button className="btn btn-ghost" onClick={onImportDraftPack}>
                                <Package size={15}/> Import pack
                            </button>
                        )}
                        {onImportProject && (
                            <button className="btn btn-ghost" onClick={onImportProject}>
                                <FileUp size={15}/> Import config
                            </button>
                        )}
                        <button className="btn btn-primary" onClick={onCreateProject}>
                            <FolderPlus size={15}/> New project
                        </button>
                    </div>
                }
            />

            <div className="projects-layout">
                {loading ? (
                    <div className="projects-loading">Loading projects...</div>
                ) : projects.length === 0 ? (
                    <div className="projects-empty">
                        <Boxes size={20}/>
                        <div className="projects-empty-copy">
                            <strong>No projects yet.</strong>
                            <span>Add a local project to start shaping environments, ports, and canvas topology.</span>
                        </div>
                    </div>
                ) : (
                    <div className="projects-stack">
                        {projects.map((project) => {
                            const isOpen = expanded === project.project.id;
                            const totalRunning = project.services.filter((service) => service.status === 'running').length;
                            const totalStopped = project.services.filter((service) => service.status === 'stopped').length;
                            const statusColor = projectStatusColor(project.status);
                            const envCount = project.environmentCount || project.environments.length;

                            return (
                                <article key={project.project.id} className="project-card">
                                    <button
                                        className="project-card-header"
                                        onClick={() => setExpanded(isOpen ? null : project.project.id)}
                                    >
                                        <span
                                            className="project-card-status"
                                            style={{
                                                background: statusColor,
                                            }}
                                        />
                                        <div className="project-card-copy">
                                            <div className="project-card-title-row">
                                                <h2 className="project-card-name">{project.project.name}</h2>
                                                {project.project.description && (
                                                    <span className="project-card-inline-desc">{project.project.description}</span>
                                                )}
                                            </div>
                                            <div className="project-card-meta">
                                                <span>{project.services.length} services</span>
                                                <span className="project-meta-divider">·</span>
                                                <span>{envCount} env{envCount === 1 ? '' : 's'}</span>
                                                <span className="project-meta-divider">·</span>
                                                <span>{project.lastActive}</span>
                                            </div>
                                        </div>
                                        <div className="project-card-badges">
                                            {totalRunning > 0 && <span className="project-chip project-chip-up">{totalRunning} up</span>}
                                            {totalStopped > 0 && <span className="project-chip project-chip-down">{totalStopped} down</span>}
                                            <ChevronRight className={'project-card-chevron' + (isOpen ? ' open' : '')} size={13}/>
                                        </div>
                                    </button>

                                    {isOpen && (
                                        <div className="project-card-body">
                                            <div className="project-env-list">
                                                {project.environments.length === 0 ? (
                                                    <span className="project-card-services-empty">No environments yet.</span>
                                                ) : (
                                                    project.environments.map((env) => (
                                                        <div key={env.id || env.slug} className="project-env-row">
                                                            <button
                                                                type="button"
                                                                className="project-env-row-main"
                                                                onClick={() => onOpenProject(project.project, env.id || undefined)}
                                                            >
                                                                <span
                                                                    className="project-env-status"
                                                                    style={{background: projectStatusColor(env.status)}}
                                                                />
                                                                <div className="project-env-copy">
                                                                    <div className="project-env-title">
                                                                        <strong>{env.name}</strong>
                                                                        {env.isDefault && (
                                                                            <span className="project-env-default">default</span>
                                                                        )}
                                                                    </div>
                                                                    <div className="project-env-meta">{envStatusLine(env)}</div>
                                                                </div>
                                                                <div className="project-env-pills">
                                                                    {env.services.length === 0 ? (
                                                                        <span className="project-card-services-empty">Empty</span>
                                                                    ) : (
                                                                        env.services.slice(0, 6).map((service) => (
                                                                            <ServicePill key={service.id} service={service}/>
                                                                        ))
                                                                    )}
                                                                </div>
                                                                <span className="project-env-open">
                                                                    Open <ArrowRight size={12}/>
                                                                </span>
                                                            </button>
                                                        </div>
                                                    ))
                                                )}
                                            </div>
                                            <div className="project-card-actions">
                                                <button className="btn btn-ghost project-card-open" onClick={() => onOpenProject(project.project)}>
                                                    Open canvas <ArrowRight size={14}/>
                                                </button>
                                                {onOpenProjectSettings && (
                                                    <button
                                                        className="btn btn-ghost project-card-settings"
                                                        onClick={(e) => { e.stopPropagation(); onOpenProjectSettings(project.project); }}
                                                        title="Project settings"
                                                    >
                                                        <Settings size={14}/> Settings
                                                    </button>
                                                )}
                                            </div>
                                            <p className="project-card-path">{project.project.path}</p>
                                        </div>
                                    )}
                                </article>
                            );
                        })}
                    </div>
                )}
            </div>
        </div>
    );
}
