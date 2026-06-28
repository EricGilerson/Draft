import {useState} from 'react';
import './App.css';
import Sidebar, {NavId} from './components/Sidebar';
import DockerIndicator from './components/DockerIndicator';

const VIEW_TITLES: Record<NavId, string> = {
    overview: 'Overview',
    projects: 'Projects',
    sandboxes: 'Sandboxes',
    settings: 'Settings',
};

function App() {
    const [view, setView] = useState<NavId>('overview');

    return (
        <div className="app-shell">
            <Sidebar active={view} onSelect={setView}/>
            <div className="app-main">
                <header className="topbar">
                    <h1 className="topbar-title">{VIEW_TITLES[view]}</h1>
                    <DockerIndicator/>
                </header>
                <main className="app-content">
                    <p className="placeholder">{VIEW_TITLES[view]} — coming soon.</p>
                </main>
            </div>
        </div>
    );
}

export default App
