import {useEffect, useMemo, useState} from 'react';
import {FlaskConical, LayoutDashboard} from 'lucide-react';
import './App.css';
import {ListProjects} from '../wailsjs/go/main/App';
import Sidebar, {NavId} from './components/Sidebar';
import DockerIndicator from './components/DockerIndicator';
import CreateProjectDialog from './components/CreateProjectDialog';
import EmptyState from './components/EmptyState';
import {decorateProjects} from './lib/dashboardData';
import ProjectCanvas from './components/ProjectCanvas';
import ProjectsView from './views/ProjectsView';
import SettingsView from './views/SettingsView';
import {store} from '../wailsjs/go/models';

function App() {
    const [view, setView] = useState<NavId>('overview');
    const [projects, setProjects] = useState<store.Project[]>([]);
    const [loading, setLoading] = useState(true);
    const [dialogOpen, setDialogOpen] = useState(false);
    const [selectedProject, setSelectedProject] = useState<store.Project | null>(null);

    useEffect(() => {
        ListProjects()
            .then((items) => setProjects(items ?? []))
            .catch(() => setProjects([]))
            .finally(() => setLoading(false));
    }, []);

    const summaries = useMemo(() => decorateProjects(projects), [projects]);
    const handleSelectView = (next: NavId) => {
        setView(next);
        if (next !== 'projects') {
            setSelectedProject(null);
        }
    };

    const handleProjectCreated = (project: store.Project) => {
        setProjects((prev) => [project, ...prev]);
        setDialogOpen(false);
        setSelectedProject(project);
        setView('projects');
    };

    const openProject = (project: store.Project) => {
        setSelectedProject(project);
        setView('projects');
    };

    return (
        <div className="app-shell">
            <Sidebar active={view} onSelect={handleSelectView}/>
            <div className="app-main">
                <header className="topbar">
                    <DockerIndicator/>
                </header>
                <main className="app-content">
                    <div className="app-content-glow"/>
                    <div className="app-content-scroll">
                        {selectedProject ? (
                            <ProjectCanvas
                                project={selectedProject}
                            />
                        ) : view === 'overview' ? (
                            <div className="view-center">
                                <EmptyState
                                    icon={LayoutDashboard}
                                    title="Overview"
                                    description="Dashboard UI will come back once the underlying behavior exists."
                                    chip="Coming soon"
                                />
                            </div>
                        ) : view === 'projects' ? (
                            <ProjectsView
                                loading={loading}
                                projects={summaries}
                                onCreateProject={() => setDialogOpen(true)}
                                onOpenProject={openProject}
                            />
                        ) : view === 'sandboxes' ? (
                            <div className="view-center">
                                <EmptyState
                                    icon={FlaskConical}
                                    title="Sandboxes"
                                    description="Sandbox UI is intentionally blank until the feature is implemented."
                                    chip="Coming soon"
                                />
                            </div>
                        ) : (
                            <SettingsView/>
                        )}
                    </div>
                </main>
            </div>

            {dialogOpen && (
                <CreateProjectDialog
                    onClose={() => setDialogOpen(false)}
                    onCreated={handleProjectCreated}
                />
            )}
        </div>
    );
}

export default App
