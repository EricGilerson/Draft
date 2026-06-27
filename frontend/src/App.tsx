import {useEffect, useState} from 'react';
import './App.css';
import {CheckDocker} from '../wailsjs/go/main/App';

type DockerState = 'checking' | 'running' | 'stopped';

function DockerIndicator() {
    const [state, setState] = useState<DockerState>('checking');
    const [detail, setDetail] = useState<string>('');

    useEffect(() => {
        let cancelled = false;

        const poll = async () => {
            try {
                const status = await CheckDocker();
                if (cancelled) return;
                if (status.state === 'running') {
                    setState('running');
                    setDetail(status.apiVersion ? `Docker API v${status.apiVersion}` : 'Docker running');
                } else {
                    setState('stopped');
                    setDetail(status.error || 'Docker daemon not reachable');
                }
            } catch (e) {
                if (cancelled) return;
                setState('stopped');
                setDetail(String(e));
            }
        };

        poll();
        const id = setInterval(poll, 3000);
        return () => {
            cancelled = true;
            clearInterval(id);
        };
    }, []);

    const label =
        state === 'running' ? 'Docker running'
            : state === 'stopped' ? 'Docker off'
                : 'Checking…';

    return (
        <div className={'docker-indicator ' + state} title={detail || label}>
            <span className="docker-dot"/>
            <span className="docker-label">{label}</span>
        </div>
    );
}

function App() {
    const [activeView, setActiveView] = useState<string | null>(null);

    return (
        <div id="App">
            <aside className="sidebar">
                <div className="sidebar-title">Draft</div>
                <nav className="sidebar-nav">
                    <button
                        className={"nav-item" + (activeView === 'dashboard' ? ' active' : '')}
                        onClick={() => setActiveView('dashboard')}
                    >
                        Dashboard
                    </button>
                </nav>
            </aside>
            <main className="content">
                <DockerIndicator/>
                {activeView === 'dashboard'
                    ? <h1>Hi 👋</h1>
                    : <p className="placeholder">Select an item from the panel.</p>}
            </main>
        </div>
    );
}

export default App
