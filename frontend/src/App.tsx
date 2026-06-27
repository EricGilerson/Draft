import {useEffect, useState} from 'react';
import './App.css';
import {CheckDocker} from '../wailsjs/go/main/App';
import {EventsOn} from '../wailsjs/runtime/runtime';
import {dockerwatch} from '../wailsjs/go/models';

type DockerState = 'checking' | 'running' | 'stopped';

function DockerIndicator() {
    const [state, setState] = useState<DockerState>('checking');
    const [detail, setDetail] = useState<string>('');

    useEffect(() => {
        const apply = (status?: dockerwatch.DaemonStatus) => {
            if (!status || !status.state) return;
            if (status.state === 'running') {
                setState('running');
                setDetail(status.apiVersion ? `Docker API v${status.apiVersion}` : 'Docker running');
            } else {
                setState('stopped');
                setDetail(status.error || 'Docker daemon not reachable');
            }
        };

        // Subscribe first so no change is missed, then fetch the current value
        // once. After that the backend pushes updates only when state changes —
        // no polling.
        const unsubscribe = EventsOn('docker:status', (status: dockerwatch.DaemonStatus) => apply(status));
        CheckDocker()
            .then(apply)
            .catch((e) => {
                setState('stopped');
                setDetail(String(e));
            });

        return () => {
            unsubscribe();
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
