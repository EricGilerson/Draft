import {useState} from 'react';
import './App.css';

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
                {activeView === 'dashboard'
                    ? <h1>Hi 👋</h1>
                    : <p className="placeholder">Select an item from the panel.</p>}
            </main>
        </div>
    );
}

export default App
