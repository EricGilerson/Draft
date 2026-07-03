import {useCallback, useEffect, useMemo, useRef, useState} from 'react';
import {FlaskConical, LayoutDashboard} from 'lucide-react';
import './App.css';
import {ListProjects, ListProjectServices} from '../wailsjs/go/main/App';
import {EventsOn} from '../wailsjs/runtime/runtime';
import Sidebar, {NavId} from './components/Sidebar';
import DockerIndicator from './components/DockerIndicator';
import ActivityTicker from './components/ActivityTicker';
import {BuildLogProvider} from './components/BuildLogProvider';
import CreateProjectDialog from './components/CreateProjectDialog';
import EmptyState from './components/EmptyState';
import {decorateProjects} from './lib/dashboardData';
import ProjectCanvas from './components/ProjectCanvas';
import ProjectsView from './views/ProjectsView';
import SettingsView from './views/SettingsView';
import TemplatesView from './views/TemplatesView';
import {main, store} from '../wailsjs/go/models';

function App() {
    const [view, setView] = useState<NavId>('overview');
    const [projects, setProjects] = useState<store.Project[]>([]);
    const [servicesByProject, setServicesByProject] = useState<Record<number, main.ProjectService[]>>({});
    const [loading, setLoading] = useState(true);
    const [dialogOpen, setDialogOpen] = useState(false);
    const [selectedProject, setSelectedProject] = useState<store.Project | null>(null);
    const projectsRef = useRef<store.Project[]>([]);

    const refreshProjectServices = useCallback(async (items: store.Project[]) => {
        if (items.length === 0) {
            setServicesByProject({});
            return;
        }
        const entries = await Promise.all(
            items.map(async (project) => {
                const services = await ListProjectServices(project.id).catch(() => []);
                return [project.id, services ?? []] as const;
            }),
        );
        setServicesByProject(Object.fromEntries(entries));
    }, []);

    const refreshProjects = useCallback(async () => {
        setLoading(true);
        try {
            const items = await ListProjects();
            const nextProjects = items ?? [];
            projectsRef.current = nextProjects;
            setProjects(nextProjects);
            await refreshProjectServices(nextProjects);
        } catch {
            projectsRef.current = [];
            setProjects([]);
            setServicesByProject({});
        } finally {
            setLoading(false);
        }
    }, [refreshProjectServices]);

    useEffect(() => {
        refreshProjects();
    }, [refreshProjects]);

    useEffect(() => {
        const unsubscribe = EventsOn('deploy:status', () => {
            refreshProjectServices(projectsRef.current);
        });
        return unsubscribe;
    }, [refreshProjectServices]);

    const summaries = useMemo(() => decorateProjects(projects, servicesByProject), [projects, servicesByProject]);
    const handleSelectView = (next: NavId) => {
        setView(next);
        if (next === 'projects') {
            refreshProjectServices(projectsRef.current);
        }
        if (next !== 'projects') {
            setSelectedProject(null);
        }
    };

    const handleProjectCreated = (project: store.Project) => {
        setProjects((prev) => {
            const nextProjects = [project, ...prev];
            projectsRef.current = nextProjects;
            return nextProjects;
        });
        setServicesByProject((prev) => ({...prev, [project.id]: []}));
        setDialogOpen(false);
        setSelectedProject(project);
        setView('projects');
    };

    const openProject = (project: store.Project) => {
        setSelectedProject(project);
        setView('projects');
    };

    return (
        <BuildLogProvider>
        <div className="app-shell">
            <Sidebar active={view} onSelect={handleSelectView}/>
            <div className="app-main">
                <header className="topbar">
                    <ActivityTicker/>
                    <DockerIndicator/>
                </header>
                <main className="app-content">
                    <div className="app-content-glow"/>
                    <div className="app-content-scroll">
                        {selectedProject ? (
                            <ProjectCanvas
                                project={selectedProject}
                                onServicesChanged={() => refreshProjectServices(projectsRef.current)}
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
                        ) : view === 'templates' ? (
                            <TemplatesView/>
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
        </BuildLogProvider>
    );
}

export default App
