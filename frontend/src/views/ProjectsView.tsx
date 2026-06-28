import {useEffect, useState} from 'react';
import {Boxes, Plus} from 'lucide-react';
import {ListProjects} from '../../wailsjs/go/main/App';
import {store} from '../../wailsjs/go/models';
import EmptyState from '../components/EmptyState';
import CreateProjectDialog from '../components/CreateProjectDialog';
import ProjectCanvas from '../components/ProjectCanvas';
import './ProjectsView.css';

export default function ProjectsView() {
    const [projects, setProjects] = useState<store.Project[]>([]);
    const [loading, setLoading] = useState(true);
    const [dialogOpen, setDialogOpen] = useState(false);
    const [selected, setSelected] = useState<store.Project | null>(null);

    useEffect(() => {
        ListProjects()
            .then((ps) => setProjects(ps ?? []))
            .catch(() => setProjects([]))
            .finally(() => setLoading(false));
    }, []);

    const handleCreated = (project: store.Project) => {
        setProjects((prev) => [project, ...prev]);
        setDialogOpen(false);
        setSelected(project);
    };

    if (selected) {
        return <ProjectCanvas project={selected} onBack={() => setSelected(null)}/>;
    }

    return (
        <div className="projects-view">
            {loading ? (
                <div className="projects-loading">Loading…</div>
            ) : projects.length === 0 ? (
                <div className="projects-empty">
                    <EmptyState
                        icon={Boxes}
                        title="No projects yet"
                        description="Add a local project to start managing its services and ports."
                    />
                    <button className="btn btn-primary" onClick={() => setDialogOpen(true)}>
                        <Plus size={16}/> Create Project
                    </button>
                </div>
            ) : (
                <div className="projects-list">
                    <div className="projects-toolbar">
                        <span className="projects-count">
                            {projects.length} project{projects.length === 1 ? '' : 's'}
                        </span>
                        <button className="btn btn-primary" onClick={() => setDialogOpen(true)}>
                            <Plus size={16}/> New Project
                        </button>
                    </div>
                    <div className="projects-grid">
                        {projects.map((p) => (
                            <button key={p.id} className="project-card" onClick={() => setSelected(p)}>
                                <span className="project-card-name">{p.name}</span>
                                <span className="project-card-path">{p.path}</span>
                                {p.description && (
                                    <span className="project-card-desc">{p.description}</span>
                                )}
                            </button>
                        ))}
                    </div>
                </div>
            )}

            {dialogOpen && (
                <CreateProjectDialog
                    onClose={() => setDialogOpen(false)}
                    onCreated={handleCreated}
                />
            )}
        </div>
    );
}
