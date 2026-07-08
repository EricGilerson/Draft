import {useCallback, useEffect, useMemo, useRef, useState} from 'react';
import {FlaskConical} from 'lucide-react';
import './App.css';
import {ListProjects, ListProjectServices} from '../wailsjs/go/main/App';
import {EventsOn} from '../wailsjs/runtime/runtime';
import Sidebar, {NavId} from './components/Sidebar';
import DockerIndicator from './components/DockerIndicator';
import ActivityTicker from './components/ActivityTicker';
import {BuildLogProvider} from './components/BuildLogProvider';
import CreateProjectDialog from './components/CreateProjectDialog';
import ImportConfigDialog from './components/ImportConfigDialog';
import ProjectSettingsDialog from './components/ProjectSettingsDialog';
import EmptyState from './components/EmptyState';
import {decorateProjects} from './lib/dashboardData';
import {useActivityLog} from './lib/useActivityLog';
import ProjectCanvas from './components/ProjectCanvas';
import ProjectsView from './views/ProjectsView';
import OverviewView from './views/OverviewView';
import SettingsView from './views/SettingsView';
import TemplatesView from './views/TemplatesView';
import VolumesView from './views/VolumesView';
import RoutesView from './views/RoutesView';
import {main, store} from '../wailsjs/go/models';

type VolumeFocus = {nodeId: string; target: string};
type NodeFocus = {projectId: number; nodeId: string};

function App() {
    const [view, setView] = useState<NavId>('overview');
    const [projects, setProjects] = useState<store.Project[]>([]);
    const [servicesByProject, setServicesByProject] = useState<Record<number, main.ProjectService[]>>({});
    const [loading, setLoading] = useState(true);
    const [dialogOpen, setDialogOpen] = useState(false);
    const [importOpen, setImportOpen] = useState(false);
    const [selectedProject, setSelectedProject] = useState<store.Project | null>(null);
    const [pendingVolumeFocus, setPendingVolumeFocus] = useState<VolumeFocus | null>(null);
    const [pendingNodeFocus, setPendingNodeFocus] = useState<NodeFocus | null>(null);
    const [settingsProject, setSettingsProject] = useState<store.Project | null>(null);
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
    const activity = useActivityLog(projects, servicesByProject);
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

    const revealVolume = (projectId: number, nodeId: string, target: string) => {
        const project = projectsRef.current.find((p) => p.id === projectId);
        if (!project) return;
        setSelectedProject(project);
        setPendingVolumeFocus({nodeId, target});
        setView('projects');
    };

    const openProjectSettings = (project: store.Project) => {
        setSettingsProject(project);
    };

    const revealNode = (projectId: number, nodeId: string) => {
        const project = projectsRef.current.find((p) => p.id === projectId);
        if (!project) return;
        setSelectedProject(project);
        setPendingNodeFocus({projectId, nodeId});
        setView('projects');
    };

    const handleProjectUpdated = () => {
        refreshProjects().then(() => {
            if (settingsProject) {
                const updated = projectsRef.current.find((p) => p.id === settingsProject.id);
                if (updated) {
                    setSettingsProject(updated);
                    setSelectedProject((cur) => (cur?.id === updated.id ? updated : cur));
                }
            }
        });
    };

    const handleProjectDeleted = (projectId: number) => {
        setProjects((prev) => {
            const nextProjects = prev.filter((p) => p.id !== projectId);
            projectsRef.current = nextProjects;
            return nextProjects;
        });
        setServicesByProject((prev) => {
            const next = {...prev};
            delete next[projectId];
            return next;
        });
        setSelectedProject((cur) => (cur?.id === projectId ? null : cur));
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
                                initialVolumeFocus={pendingVolumeFocus}
                                onVolumeFocusApplied={() => setPendingVolumeFocus(null)}
                                onOpenProjectSettings={() => openProjectSettings(selectedProject)}
                                initialSelectedNodeId={
                                    pendingNodeFocus && pendingNodeFocus.projectId === selectedProject.id
                                        ? pendingNodeFocus.nodeId
                                        : null
                                }
                                onNodeFocusApplied={() => setPendingNodeFocus(null)}
                            />
                        ) : view === 'overview' ? (
                            <OverviewView
                                loading={loading}
                                projects={summaries}
                                activity={activity}
                                onCreateProject={() => setDialogOpen(true)}
                                onOpenProject={openProject}
                            />
                        ) : view === 'projects' ? (
                            <ProjectsView
                                loading={loading}
                                projects={summaries}
                                onCreateProject={() => setDialogOpen(true)}
                                onImportProject={() => setImportOpen(true)}
                                onOpenProject={openProject}
                                onOpenProjectSettings={openProjectSettings}
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
                        ) : view === 'volumes' ? (
                            <VolumesView onRevealVolume={revealVolume}/>
                        ) : view === 'routes' ? (
                            <RoutesView
                                projects={projects}
                                onRevealNode={revealNode}
                            />
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

            {importOpen && (
                <ImportConfigDialog
                    mode="project"
                    onClose={() => setImportOpen(false)}
                    onImported={(projectId) => {
                        setImportOpen(false);
                        refreshProjects().then(() => {
                            const project = projectsRef.current.find((p) => p.id === projectId);
                            if (project) {
                                setSelectedProject(project);
                                setView('projects');
                            }
                        });
                    }}
                />
            )}

            {settingsProject && (
                <ProjectSettingsDialog
                    project={settingsProject}
                    onClose={() => setSettingsProject(null)}
                    onProjectUpdated={handleProjectUpdated}
                    onProjectDeleted={handleProjectDeleted}
                />
            )}
        </div>
        </BuildLogProvider>
    );
}

export default App
