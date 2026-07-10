import {RefreshCw} from 'lucide-react';
import {useEffect, useState} from 'react';
import {GetAppSettings, GetLocalDomainStatus, SetAppSettings} from '../../wailsjs/go/main/App';
import {main, networking} from '../../wailsjs/go/models';
import PageHeader from '../components/PageHeader';
import './WorkspaceViews.css';

function Toggle({checked, onChange, disabled}: {checked: boolean; onChange: (value: boolean) => void; disabled?: boolean}) {
    return (
        <button
            type="button"
            className={'toggle' + (checked ? ' checked' : '')}
            role="switch"
            aria-checked={checked}
            disabled={disabled}
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

type SettingsViewProps = {
    onSettingsChanged?: (settings: main.AppSettings) => void;
};

export default function SettingsView({onSettingsChanged}: SettingsViewProps) {
    const [loading, setLoading] = useState(true);
    const [saving, setSaving] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [savedFlash, setSavedFlash] = useState(false);
    const [compactSidebar, setCompactSidebar] = useState(false);
    const [localDomainPreference, setLocalDomainPreference] = useState('auto');
    const [domainStatus, setDomainStatus] = useState<networking.LocalDomainStatus | null>(null);

    const load = () => {
        setLoading(true);
        setError(null);
        Promise.all([
            GetAppSettings().catch((e) => {
                throw e;
            }),
            GetLocalDomainStatus().catch(() => null),
        ])
            .then(([settings, status]) => {
                setCompactSidebar(!!settings?.compactSidebar);
                setLocalDomainPreference(settings?.localDomainPreference || 'auto');
                setDomainStatus(status);
            })
            .catch((e) => setError(typeof e === 'string' ? e : e?.message || 'Could not load settings'))
            .finally(() => setLoading(false));
    };

    useEffect(() => {
        load();
    }, []);

    const persist = async (next: {compactSidebar?: boolean; localDomainPreference?: string}) => {
        setSaving(true);
        setError(null);
        setSavedFlash(false);
        try {
            const saved = await SetAppSettings({
                compactSidebar: next.compactSidebar ?? compactSidebar,
                localDomainPreference: next.localDomainPreference ?? localDomainPreference,
            } as main.AppSettings);
            if (saved) {
                setCompactSidebar(!!saved.compactSidebar);
                setLocalDomainPreference(saved.localDomainPreference || 'auto');
                onSettingsChanged?.(saved);
            }
            const status = await GetLocalDomainStatus().catch(() => null);
            setDomainStatus(status);
            setSavedFlash(true);
            setTimeout(() => setSavedFlash(false), 2000);
        } catch (e: any) {
            setError(typeof e === 'string' ? e : e?.message || 'Could not save settings');
        } finally {
            setSaving(false);
        }
    };

    const modeLabel = domainStatus?.mode === 'public-hostname-port'
        ? 'Public hostname + proxy port'
        : 'Localhost + host port';

    return (
        <div className="workspace-view">
            <PageHeader
                title="Settings"
                description="App preferences for appearance and how service URLs are presented."
            />

            <div className="workspace-body workspace-narrow">
                {loading ? (
                    <div className="panel-empty">Loading settings…</div>
                ) : (
                    <>
                        {error && (
                            <div className="preview-banner preview-banner-error">
                                <span>{error}</span>
                            </div>
                        )}
                        {savedFlash && (
                            <div className="preview-banner">
                                <span>Settings saved.</span>
                            </div>
                        )}

                        <SettingsSection title="Appearance">
                            <SettingsRow
                                label="Compact sidebar"
                                description="Tighten nav spacing for denser workspaces."
                            >
                                <Toggle
                                    checked={compactSidebar}
                                    disabled={saving}
                                    onChange={(value) => {
                                        setCompactSidebar(value);
                                        void persist({compactSidebar: value});
                                    }}
                                />
                            </SettingsRow>
                        </SettingsSection>

                        <SettingsSection title="Local networking">
                            <SettingsRow
                                label="URL preference"
                                description="Choose how Draft presents service URLs. Auto follows whether the local reverse proxy is available."
                            >
                                <select
                                    className="input select-styled settings-url-preference-select"
                                    value={localDomainPreference}
                                    disabled={saving}
                                    onChange={(e) => {
                                        const value = e.target.value;
                                        setLocalDomainPreference(value);
                                        void persist({localDomainPreference: value});
                                    }}
                                >
                                    <option value="auto">Auto</option>
                                    <option value="public-hostname-port">Prefer public hostname</option>
                                    <option value="localhost-port">Prefer localhost port</option>
                                </select>
                            </SettingsRow>
                            <SettingsRow
                                label="Effective mode"
                                description="What the app is using right now after preference + proxy availability."
                            >
                                <div className="settings-status-block">
                                    <span className="settings-status-value">{modeLabel}</span>
                                    {domainStatus?.proxyAddr && (
                                        <span className="settings-status-meta">Proxy {domainStatus.proxyAddr}</span>
                                    )}
                                    {domainStatus?.hostsError && (
                                        <span className="settings-status-error">{domainStatus.hostsError}</span>
                                    )}
                                    <button type="button" className="btn btn-ghost" onClick={load} disabled={loading || saving}>
                                        <RefreshCw size={14}/> Refresh status
                                    </button>
                                </div>
                            </SettingsRow>
                        </SettingsSection>
                    </>
                )}
            </div>
        </div>
    );
}
