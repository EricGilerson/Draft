import {ChevronDown, RefreshCw} from 'lucide-react';
import {useState} from 'react';
import PageHeader from '../components/PageHeader';
import './WorkspaceViews.css';

function Toggle({checked, onChange}: {checked: boolean; onChange: (value: boolean) => void}) {
    return (
        <button
            type="button"
            className={'toggle' + (checked ? ' checked' : '')}
            role="switch"
            aria-checked={checked}
            onClick={() => onChange(!checked)}
        >
            <span className="toggle-thumb"/>
        </button>
    );
}

function SettingsSection({title, children}: {title: string; children: React.ReactNode}) {
    return (
        <section className="settings-section">
            <div className="settings-section-title">{title}</div>
            <div className="settings-section-body">{children}</div>
        </section>
    );
}

function SettingsRow({
    label,
    description,
    children,
}: {
    label: string;
    description?: string;
    children: React.ReactNode;
}) {
    return (
        <div className="settings-row">
            <div className="settings-row-copy">
                <div className="settings-row-label">{label}</div>
                {description && <div className="settings-row-description">{description}</div>}
            </div>
            <div className="settings-row-control">{children}</div>
        </div>
    );
}

export default function SettingsView() {
    // These values are intentionally local-only until the backend exposes a
    // persistent settings contract.
    const [launchAtLogin, setLaunchAtLogin] = useState(true);
    const [autoSyncEnv, setAutoSyncEnv] = useState(true);
    const [compactSidebar, setCompactSidebar] = useState(false);

    return (
        <div className="workspace-view">
            <PageHeader
                title="Settings"
                description="Prepared UI for app preferences, Docker behavior, and appearance. Controls are not persisted yet."
            />

            <div className="workspace-body workspace-narrow">
                <div className="preview-banner">
                    <RefreshCw size={14}/>
                    <span>This screen is a design pass only for now. The controls are present, but they do not save to the backend yet.</span>
                </div>

                <SettingsSection title="General">
                    <SettingsRow label="App name" description="Display name shown in the shell">
                        <input className="settings-input" value="Draft" readOnly/>
                    </SettingsRow>
                    <SettingsRow label="Launch at login" description="Start Draft automatically after sign-in">
                        <Toggle checked={launchAtLogin} onChange={setLaunchAtLogin}/>
                    </SettingsRow>
                    <SettingsRow label="Auto-sync .env" description="Apply resolved variables when topology changes">
                        <Toggle checked={autoSyncEnv} onChange={setAutoSyncEnv}/>
                    </SettingsRow>
                </SettingsSection>

                <SettingsSection title="Docker">
                    <SettingsRow label="Socket path">
                        <input className="settings-input settings-input-mono" value="/var/run/docker.sock" readOnly/>
                    </SettingsRow>
                    <SettingsRow label="Connection timeout" description="Retry threshold before the desktop app reports Docker as unavailable">
                        <input className="settings-input settings-input-mono settings-input-short" value="5s" readOnly/>
                    </SettingsRow>
                    <SettingsRow label="Connectivity check">
                        <button className="btn btn-ghost">
                            <RefreshCw size={14}/> Run test
                        </button>
                    </SettingsRow>
                </SettingsSection>

                <SettingsSection title="Ports">
                    <SettingsRow label="Reserved range" description="Draft allocates host ports from this window first">
                        <div className="range-group">
                            <input className="settings-input settings-input-mono settings-input-short" value="3000" readOnly/>
                            <span className="range-divider">-</span>
                            <input className="settings-input settings-input-mono settings-input-short" value="9999" readOnly/>
                        </div>
                    </SettingsRow>
                    <SettingsRow label="Conflict strategy" description="Default handling when a desired host port is taken">
                        <div className="settings-select-wrap">
                            <select className="settings-select" defaultValue="Auto-reassign">
                                <option>Auto-reassign</option>
                                <option>Warn and skip</option>
                                <option>Fail loudly</option>
                            </select>
                            <ChevronDown size={14} className="settings-select-icon" aria-hidden="true"/>
                        </div>
                    </SettingsRow>
                </SettingsSection>

                <SettingsSection title="Appearance">
                    <SettingsRow label="Compact sidebar" description="Tighten nav spacing for denser workspaces">
                        <Toggle checked={compactSidebar} onChange={setCompactSidebar}/>
                    </SettingsRow>
                </SettingsSection>
            </div>
        </div>
    );
}
