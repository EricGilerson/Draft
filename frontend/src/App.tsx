import {useCallback, useEffect, useMemo, useRef, useState} from 'react';
import './App.css';
import {GetAppSettings, GetDefaultEnvironment, ListProjectServicesSummary, ListProjects} from '../wailsjs/go/main/App';
import {EventsOn} from '../wailsjs/runtime/runtime';
import Sidebar, {NavId} from './components/Sidebar';
import DockerIndicator from './components/DockerIndicator';
import ActivityTicker from './components/ActivityTicker';
import {AppDialogProvider} from './components/AppDialogProvider';
import {BuildLogProvider} from './components/BuildLogProvider';
import CreateProjectDialog from './components/CreateProjectDialog';
import ImportConfigDialog from './components/ImportConfigDialog';
import ImportDraftPackDialog from './components/ImportDraftPackDialog';
import ProjectSettingsDialog from './components/ProjectSettingsDialog';
import {decorateProjectSummaries} from './lib/dashboardData';
import {useActivityLog} from './lib/useActivityLog';
import ProjectCanvas from './components/ProjectCanvas';
import EnvironmentSwitcher from './components/EnvironmentSwitcher';
import ProjectsView from './views/ProjectsView';
import OverviewView from './views/OverviewView';
import SettingsView from './views/SettingsView';
import TemplatesView from './views/TemplatesView';
import SecretsView from './views/SecretsView';
import VolumesView from './views/VolumesView';
import DockerView from './views/DockerView';
import RoutesView from './views/RoutesView';
import SandboxesView from './views/SandboxesView';
import {main, store} from '../wailsjs/go/models';

type VolumeFocus = {nodeId: string; target: string};
type NodeFocus = {projectId: number; nodeId: string; environmentId?: number | null};

function App() {
    const [view, setView] = useState<NavId>('overview');
    const [projects, setProjects] = useState<store.Project[]>([]);
    const [summariesByProject, setSummariesByProject] = useState<Record<number, main.ProjectServicesSummary>>({});
    const [loading, setLoading] = useState(true);
    const [dialogOpen, setDialogOpen] = useState(false);
    const [importOpen, setImportOpen] = useState(false);
    const [draftPackImportOpen, setDraftPackImportOpen] = useState(false);
    const [selectedProject, setSelectedProject] = useState<store.Project | null>(null);
    const [selectedEnvironmentId, setSelectedEnvironmentId] = useState<number | null>(null);
    const [environmentBusy, setEnvironmentBusy] = useState(false);
    const [pendingVolumeFocus, setPendingVolumeFocus] = useState<VolumeFocus | null>(null);
    const [pendingNodeFocus, setPendingNodeFocus] = useState<NodeFocus | null>(null);
    const [settingsProject, setSettingsProject] = useState<store.Project | null>(null);
    const [compactSidebar, setCompactSidebar] = useState(false);
    const [sandboxSource, setSandboxSource] = useState<{projectId: number; environmentId: number} | null>(null);
    const projectsRef = useRef<store.Project[]>([]);
    const requestedEnvironmentRef = useRef<{projectId: number; environmentId: number} | null>(null);

    const refreshProjectSummaries = useCallback(async (items: store.Project[]) => {
        if (items.length === 0) {
            setSummariesByProject({});
            return;
        }
        const entries = await Promise.all(
            items.map(async (project) => {
                const summary = await ListProjectServicesSummary(project.id).catch(() => null);
                return [project.id, summary] as const;
            }),
        );
        const next: Record<number, main.ProjectServicesSummary> = {};
        for (const [id, summary] of entries) {
            if (summary) next[id] = summary;
        }
        setSummariesByProject(next);
    }, []);

    const refreshProjects = useCallback(async () => {
        setLoading(true);
        try {
            const items = await ListProjects();
            const nextProjects = items ?? [];
            projectsRef.current = nextProjects;
            setProjects(nextProjects);
            await refreshProjectSummaries(nextProjects);
        } catch {
            projectsRef.current = [];
            setProjects([]);
            setSummariesByProject({});
        } finally {
            setLoading(false);
        }
    }, [refreshProjectSummaries]);

    useEffect(() => {
        refreshProjects();
    }, [refreshProjects]);

    useEffect(() => {
        GetAppSettings()
            .then((settings) => setCompactSidebar(!!settings?.compactSidebar))
            .catch(() => undefined);
    }, []);

    useEffect(() => {
        const unsubscribe = EventsOn('deploy:status', () => {
            refreshProjectSummaries(projectsRef.current);
        });
        return unsubscribe;
    }, [refreshProjectSummaries]);

    useEffect(() => {
        if (!selectedProject) {
            setSelectedEnvironmentId(null);
            return;
        }
        const requested = requestedEnvironmentRef.current;
        if (requested?.projectId === selectedProject.id) {
            setSelectedEnvironmentId(requested.environmentId);
            return;
        }
        let cancelled = false;
        GetDefaultEnvironment(selectedProject.id)
            .then((env) => {
                if (!cancelled) setSelectedEnvironmentId(env.id);
            })
            .catch(() => {
                if (!cancelled) setSelectedEnvironmentId(null);
            });
        return () => {
            cancelled = true;
        };
    }, [selectedProject]);

    const servicesByProject = useMemo(() => {
        const out: Record<number, main.ProjectService[]> = {};
        for (const [id, summary] of Object.entries(summariesByProject)) {
            out[Number(id)] = summary.services ?? [];
        }
        return out;
    }, [summariesByProject]);

    const summaries = useMemo(
        () => decorateProjectSummaries(projects, summariesByProject),
        [projects, summariesByProject],
    );
    const activity = useActivityLog(projects, servicesByProject);

    const handleSelectView = (next: NavId) => {
        setView(next);
        if (next === 'projects' || next === 'overview') {
            refreshProjectSummaries(projectsRef.current);
        }
        if (next !== 'projects') {
            setSelectedProject(null);
        }
    };

    const openSecretsTab = () => {
        setSelectedProject(null);
        setView('secrets');
        setSettingsProject(null);
    };

    const handleProjectCreated = (project: store.Project) => {
        setProjects((prev) => {
            const nextProjects = [project, ...prev];
            projectsRef.current = nextProjects;
            return nextProjects;
        });
        setSummariesByProject((prev) => ({
            ...prev,
            [project.id]: main.ProjectServicesSummary.createFrom({
                projectId: project.id,
                status: 'stopped',
                environments: [],
                services: [],
            }),
        }));
        setDialogOpen(false);
        setSelectedProject(project);
        setView('projects');
        refreshProjectSummaries([project, ...projectsRef.current.filter((p) => p.id !== project.id)]);
    };

    const openProject = (project: store.Project, environmentId?: number) => {
        requestedEnvironmentRef.current = environmentId
            ? {projectId: project.id, environmentId}
            : null;
        setSelectedProject(project);
        if (environmentId) setSelectedEnvironmentId(environmentId);
        setView('projects');
    };

    const revealVolume = (projectId: number, nodeId: string, target: string) => {
        const project = projectsRef.current.find((p) => p.id === projectId);
        if (!project) return;
        requestedEnvironmentRef.current = null;
        setSelectedProject(project);
        setPendingVolumeFocus({nodeId, target});
        setView('projects');
    };

    const openProjectSettings = (project: store.Project) => {
        setSettingsProject(project);
    };

    const revealNode = (projectId: number, nodeId: string, environmentId?: number) => {
        const project = projectsRef.current.find((p) => p.id === projectId);
        if (!project) return;
        requestedEnvironmentRef.current = environmentId ? {projectId, environmentId} : null;
        setSelectedProject(project);
        if (environmentId) setSelectedEnvironmentId(environmentId);
        setPendingNodeFocus({projectId, nodeId, environmentId: environmentId ?? null});
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
        setSummariesByProject((prev) => {
            const next = {...prev};
            delete next[projectId];
            return next;
        });
        setSelectedProject((cur) => (cur?.id === projectId ? null : cur));
    };

    return (
        <BuildLogProvider>
            <AppDialogProvider>
                <div className="app-shell">
                    <Sidebar active={view} onSelect={handleSelectView} compact={compactSidebar}/>
                    <div className="app-main">
                        <header className="topbar">
                            <ActivityTicker/>
                            <DockerIndicator/>
                        </header>
                        <main className="app-content">
                            <div className="app-content-glow"/>
                            <div className="app-content-scroll">
                        {selectedProject ? (
                            <div className="project-workspace">
                                <EnvironmentSwitcher
                                    projectId={selectedProject.id}
                                    selectedEnvironmentId={selectedEnvironmentId}
                                    onSelect={setSelectedEnvironmentId}
                                    onDuplicating={setEnvironmentBusy}
                                    onEnvironmentsChanged={() => refreshProjectSummaries(projectsRef.current)}
                                    onStackActionDone={() => refreshProjectSummaries(projectsRef.current)}
                                    onServicesChanged={() => refreshProjectSummaries(projectsRef.current)}
                                    onCreateSandbox={(environmentId) => setSandboxSource({projectId: selectedProject.id, environmentId})}
                                />
                                {selectedEnvironmentId && !environmentBusy && (
                                    <div className="project-workspace-canvas">
                                        <ProjectCanvas
                                            project={selectedProject}
                                            environmentId={selectedEnvironmentId}
                                            onServicesChanged={() => refreshProjectSummaries(projectsRef.current)}
                                            initialVolumeFocus={pendingVolumeFocus}
                                            onVolumeFocusApplied={() => setPendingVolumeFocus(null)}
                                            onOpenProjectSettings={() => openProjectSettings(selectedProject)}
                                            onOpenLinkedRootService={revealNode}
                                            initialSelectedNodeId={
                                                pendingNodeFocus && pendingNodeFocus.projectId === selectedProject.id
                                                    ? pendingNodeFocus.nodeId
                                                    : null
                                            }
                                            onNodeFocusApplied={() => setPendingNodeFocus(null)}
                                        />
                                    </div>
                                )}
                                {sandboxSource && (
                                    <SandboxesView
                                        dialogOnly
                                        projects={projects}
                                        initialSource={sandboxSource}
                                        onOpenSandbox={(projectId, environmentId) => {
                                            requestedEnvironmentRef.current = {projectId, environmentId};
                                            setSelectedEnvironmentId(environmentId);
                                            setSandboxSource(null);
                                        }}
                                        onReturnToSource={() => setSandboxSource(null)}
                                    />
                                )}
                            </div>
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
                                onImportDraftPack={() => setDraftPackImportOpen(true)}
                                onOpenProject={openProject}
                                onOpenProjectSettings={openProjectSettings}
                            />
                        ) : view === 'sandboxes' ? (
                            <SandboxesView
                                projects={projects}
                                initialSource={sandboxSource}
                                onReturnToSource={(projectId, environmentId) => {
                                    const project = projects.find((item) => item.id === projectId);
                                    if (!project) return;
                                    requestedEnvironmentRef.current = {projectId, environmentId};
                                    setSandboxSource(null);
                                    setSelectedProject(project);
                                }}
                                onOpenSandbox={(projectId, environmentId) => {
                                    const project = projects.find((item) => item.id === projectId);
                                    if (!project) return;
                                    requestedEnvironmentRef.current = {projectId, environmentId};
                                    setSandboxSource(null);
                                    setSelectedProject(project);
                                }}
                            />
                        ) : view === 'templates' ? (
                            <TemplatesView/>
                        ) : view === 'secrets' ? (
                            <SecretsView/>
                        ) : view === 'volumes' ? (
                            <VolumesView onRevealVolume={revealVolume}/>
                        ) : view === 'docker' ? (
                            <DockerView/>
                        ) : view === 'routes' ? (
                            <RoutesView
                                projects={projects}
                                onRevealNode={revealNode}
                            />
                        ) : (
                            <SettingsView
                                onSettingsChanged={(settings) => setCompactSidebar(!!settings.compactSidebar)}
                            />
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

                    {draftPackImportOpen && (
                        <ImportDraftPackDialog
                            onClose={() => setDraftPackImportOpen(false)}
                            onImported={(projectId, environmentId) => {
                                setDraftPackImportOpen(false);
                                refreshProjects().then(() => {
                                    const project = projectsRef.current.find((p) => p.id === projectId);
                                    if (project) {
                                        if (environmentId) {
                                            requestedEnvironmentRef.current = {projectId, environmentId};
                                            setSelectedEnvironmentId(environmentId);
                                        }
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
                            onOpenSecrets={openSecretsTab}
                        />
                    )}
                </div>
            </AppDialogProvider>
        </BuildLogProvider>
    );
}

export default App
