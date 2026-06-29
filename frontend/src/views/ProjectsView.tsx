import {ArrowRight, Boxes, ChevronRight, FolderPlus} from 'lucide-react';
import {useState} from 'react';
import {store} from '../../wailsjs/go/models';
import PageHeader from '../components/PageHeader';
import ServicePill from '../components/ServicePill';
import {ProjectSummary, projectStatusColor} from '../lib/dashboardData';
import './ProjectsView.css';

type ProjectsViewProps = {
    loading: boolean;
    projects: ProjectSummary[];
    onCreateProject: () => void;
    onOpenProject: (project: store.Project) => void;
};

export default function ProjectsView({loading, projects, onCreateProject, onOpenProject}: ProjectsViewProps) {
    const [expanded, setExpanded] = useState<number | null>(projects[0]?.project.id ?? null);

    return (
        <div className="projects-view">
            <PageHeader
                title="Projects"
                description="Manage your local Docker environments."
                action={
                    <button className="btn btn-primary" onClick={onCreateProject}>
                        <FolderPlus size={15}/> New project
                    </button>
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
                            const runningCount = project.services.filter((service) => service.status === 'running').length;
                            const stoppedCount = project.services.filter((service) => service.status === 'stopped').length;
                            const statusColor = projectStatusColor(project.status);

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
                                                <span>{project.lastActive}</span>
                                            </div>
                                        </div>
                                        <div className="project-card-badges">
                                            {runningCount > 0 && <span className="project-chip project-chip-up">{runningCount} up</span>}
                                            {stoppedCount > 0 && <span className="project-chip project-chip-down">{stoppedCount} down</span>}
                                            <ChevronRight className={'project-card-chevron' + (isOpen ? ' open' : '')} size={13}/>
                                        </div>
                                    </button>

                                    {isOpen && (
                                        <div className="project-card-body">
                                            <div className="project-card-services">
                                                {project.services.length === 0 ? (
                                                    <span className="project-card-services-empty">No services on the canvas yet.</span>
                                                ) : (
                                                    project.services.map((service) => (
                                                        <ServicePill key={service.id} service={service}/>
                                                    ))
                                                )}
                                            </div>
                                            <div className="project-card-actions">
                                                <button className="btn btn-ghost project-card-open" onClick={() => onOpenProject(project.project)}>
                                                    Open canvas <ArrowRight size={14}/>
                                                </button>
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
