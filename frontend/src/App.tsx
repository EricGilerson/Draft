import {useState} from 'react';
import type {ComponentType} from 'react';
import type {LucideProps} from 'lucide-react';
import {LayoutDashboard, Boxes, FlaskConical, Settings} from 'lucide-react';
import './App.css';
import Sidebar, {NavId} from './components/Sidebar';
import DockerIndicator from './components/DockerIndicator';
import EmptyState from './components/EmptyState';
import ProjectsView from './views/ProjectsView';

type ViewMeta = {
    title: string;
    icon: ComponentType<LucideProps>;
    description: string;
};

const VIEWS: Record<NavId, ViewMeta> = {
    overview: {
        title: 'Overview',
        icon: LayoutDashboard,
        description: 'Your environments and services at a glance. Connect a project to get started.',
    },
    projects: {
        title: 'Projects',
        icon: Boxes,
        description: 'No projects yet. Add a local project to start managing its services and ports.',
    },
    sandboxes: {
        title: 'Sandboxes',
        icon: FlaskConical,
        description: 'Spin up ephemeral, throwaway environments. Fork one to create your first sandbox.',
    },
    settings: {
        title: 'Settings',
        icon: Settings,
        description: 'App preferences and configuration will live here.',
    },
};

function App() {
    const [view, setView] = useState<NavId>('overview');
    const meta = VIEWS[view];

    return (
        <div className="app-shell">
            <Sidebar active={view} onSelect={setView}/>
            <div className="app-main">
                <header className="topbar">
                    <h1 className="topbar-title">{meta.title}</h1>
                    <DockerIndicator/>
                </header>
                <main className="app-content">
                    {view === 'projects' ? (
                        <ProjectsView/>
                    ) : (
                        <div className="view-center">
                            <EmptyState
                                icon={meta.icon}
                                title={meta.title}
                                description={meta.description}
                                chip="Coming soon"
                            />
                        </div>
                    )}
                </main>
            </div>
        </div>
    );
}

export default App
