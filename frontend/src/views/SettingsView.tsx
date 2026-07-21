import {LoaderCircle, RefreshCw} from 'lucide-react';
import {useEffect, useState} from 'react';
import {GetAppSettings, GetAppVersion, GetLocalDomainStatus, RefreshLocalDomainStatus, SetAppSettings, SetLocalDraftDomainEnabled, SetLocalHTTPSEnabled} from '../../wailsjs/go/main/App';
import {main, networking} from '../../wailsjs/go/models';
import PageHeader from '../components/PageHeader';
import {SkeletonSettings} from '../components/Skeleton';
import './WorkspaceViews.css';

const DEFAULT_PROXY_PORT = 38473;

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
    className,
}: {
    label: string;
    description?: string;
    children: React.ReactNode;
    className?: string;
}) {
    return (
        <div className={'settings-row' + (className ? ` ${className}` : '')}>
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

type PersistPatch = {
    compactSidebar?: boolean;
    localDomainPreference?: string;
    proxyPortMode?: string;
    proxyPort?: number;
    proxyFallbackPort?: number;
};

function clampPort(value: number, fallback = DEFAULT_PROXY_PORT): number {
    if (!Number.isFinite(value) || value < 1 || value > 65535) return fallback;
    return Math.floor(value);
}

export default function SettingsView({onSettingsChanged}: SettingsViewProps) {
    const [loading, setLoading] = useState(true);
    const [saving, setSaving] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [savedFlash, setSavedFlash] = useState(false);
    const [savedNeedsRestart, setSavedNeedsRestart] = useState(false);
    const [compactSidebar, setCompactSidebar] = useState(false);
    const [localDomainPreference, setLocalDomainPreference] = useState('auto');
    const [localDraftDomainEnabled, setLocalDraftDomainEnabled] = useState(false);
    const [localDraftDomainPending, setLocalDraftDomainPending] = useState<boolean | null>(null);
    const [localHTTPSPending, setLocalHTTPSPending] = useState<boolean | null>(null);
    const [proxyPortMode, setProxyPortMode] = useState('prefer80_fallback');
    const [proxyPort, setProxyPort] = useState(DEFAULT_PROXY_PORT);
    const [proxyFallbackPort, setProxyFallbackPort] = useState(DEFAULT_PROXY_PORT);
    const [domainStatus, setDomainStatus] = useState<networking.LocalDomainStatus | null>(null);
    const [appVersion, setAppVersion] = useState<string | null>(null);

    const applySettings = (settings: main.AppSettings | null | undefined) => {
        if (!settings) return;
        setCompactSidebar(!!settings.compactSidebar);
        setLocalDomainPreference(settings.localDomainPreference || 'auto');
        setLocalDraftDomainEnabled(!!settings.localDraftDomainEnabled);
        setProxyPortMode(settings.proxyPortMode || 'prefer80_fallback');
        setProxyPort(clampPort(settings.proxyPort || DEFAULT_PROXY_PORT));
        setProxyFallbackPort(clampPort(settings.proxyFallbackPort || DEFAULT_PROXY_PORT));
    };

    const load = () => {
        setLoading(true);
        setError(null);
        Promise.all([
            GetAppSettings().catch((e) => {
                throw e;
            }),
            // Settings is the rare place that should re-probe; hot UI uses the cache.
            RefreshLocalDomainStatus().catch(() => GetLocalDomainStatus().catch(() => null)),
            GetAppVersion().catch(() => null),
        ])
            .then(([settings, status, version]) => {
                applySettings(settings);
                setDomainStatus(status);
                setAppVersion(typeof version === 'string' && version.trim() ? version.trim() : null);
            })
            .catch((e) => setError(typeof e === 'string' ? e : e?.message || 'Could not load settings'))
            .finally(() => setLoading(false));
    };

    const setLocalDraftDomain = async (enabled: boolean) => {
        setSaving(true);
        setLocalDraftDomainPending(enabled);
        setError(null);
        setSavedFlash(false);
        try {
            const status = await SetLocalDraftDomainEnabled(enabled);
            setLocalDraftDomainEnabled(!!status?.draftEnabled);
            setDomainStatus(status);
            setSavedFlash(true);
            setTimeout(() => setSavedFlash(false), 2000);
        } catch (e: any) {
            setError(typeof e === 'string' ? e : e?.message || 'Could not update local .draft domains');
            const status = await GetLocalDomainStatus().catch(() => null);
            setDomainStatus(status);
        } finally {
            setLocalDraftDomainPending(null);
            setSaving(false);
        }
    };

    const setLocalHTTPS = async (enabled: boolean) => {
        setSaving(true);
        setLocalHTTPSPending(enabled);
        setError(null);
        setSavedFlash(false);
        try {
            const status = await SetLocalHTTPSEnabled(enabled);
            setDomainStatus(status);
            setSavedFlash(true);
            setTimeout(() => setSavedFlash(false), 2000);
        } catch (e: any) {
            setError(typeof e === 'string' ? e : e?.message || 'Could not update local HTTPS');
            setDomainStatus(await GetLocalDomainStatus().catch(() => null));
        } finally {
            setLocalHTTPSPending(null);
            setSaving(false);
        }
    };

    useEffect(() => {
        load();
    }, []);

    const persist = async (next: PersistPatch) => {
        setSaving(true);
        setError(null);
        setSavedFlash(false);
        setSavedNeedsRestart(false);
        try {
            const saved = await SetAppSettings({
                compactSidebar: next.compactSidebar ?? compactSidebar,
                localDomainPreference: next.localDomainPreference ?? localDomainPreference,
                localDraftDomainEnabled,
                proxyPortMode: next.proxyPortMode ?? proxyPortMode,
                proxyPort: next.proxyPort ?? proxyPort,
                proxyFallbackPort: next.proxyFallbackPort ?? proxyFallbackPort,
            } as main.AppSettings);
            if (saved) {
                applySettings(saved);
                onSettingsChanged?.(saved);
            }
            const status = await GetLocalDomainStatus().catch(() => null);
            setDomainStatus(status);
            const proxyChanged = next.proxyPortMode !== undefined
                || next.proxyPort !== undefined
                || next.proxyFallbackPort !== undefined;
            setSavedNeedsRestart(proxyChanged);
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

    const showPrimaryPort = proxyPortMode === 'custom';
    const showFallbackPort = proxyPortMode === 'prefer80_fallback' || proxyPortMode === 'custom';

    return (
        <div className="workspace-view">
            <PageHeader
                title="Settings"
                description="App preferences for appearance and how service URLs are presented."
            />

            <div className="workspace-body workspace-narrow">
                {loading ? (
                    <SkeletonSettings sections={3} rowsPerSection={3} />
                ) : (
                    <>
                        {error && (
                            <div className="preview-banner preview-banner-error">
                                <span>{error}</span>
                            </div>
                        )}
                        {savedFlash && (
                            <div className="preview-banner">
                                <span>
                                    {savedNeedsRestart
                                        ? 'Settings saved. Proxy port changes apply the next time Draft’s daemon starts.'
                                        : 'Settings saved.'}
                                </span>
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
                                label="Local .draft domains"
                                description="Make *.draft resolve only on this computer, even offline. Enabling or disabling asks for system permission once; existing routes and .draft.resolv.sh links remain unchanged."
                                className={localDraftDomainPending !== null ? 'settings-row--pending' : undefined}
                            >
                                <Toggle
                                    checked={localDraftDomainEnabled}
                                    disabled={saving}
                                    onChange={(value) => void setLocalDraftDomain(value)}
                                />
                            </SettingsRow>
                            {localDraftDomainPending !== null && (
                                <div className="settings-local-domain-progress" role="status" aria-live="polite">
                                    <LoaderCircle size={14} className="spin"/>
                                    <span className="settings-status-value">
                                        {localDraftDomainPending
                                            ? 'Enabling local .draft domains — waiting for permission, then installing and verifying DNS…'
                                            : 'Disabling local .draft domains — removing Draft’s DNS rule…'}
                                    </span>
                                </div>
                            )}
                            {localDraftDomainEnabled && (
                                <div className="settings-local-domain-status" role="status">
                                    <span className="settings-status-value">
                                        {domainStatus?.dnsVerified ? '.draft is resolving locally' : '.draft setup needs attention'}
                                    </span>
                                    {domainStatus?.dnsAddr && (
                                        <span className="settings-status-meta">DNS {domainStatus.dnsAddr}</span>
                                    )}
                                    {domainStatus?.dnsError && (
                                        <span className="settings-status-error">{domainStatus.dnsError}</span>
                                    )}
                                </div>
                            )}
                            <SettingsRow
                                label="Local HTTPS"
                                description="Trust a Draft-only local certificate authority and serve routed HTTP services over HTTPS on this computer. Draft never exposes these services to the internet."
                                className={localHTTPSPending !== null ? 'settings-row--pending' : undefined}
                            >
                                <Toggle
                                    checked={!!domainStatus?.httpsEnabled}
                                    disabled={saving}
                                    onChange={(value) => void setLocalHTTPS(value)}
                                />
                            </SettingsRow>
                            {localHTTPSPending !== null && (
                                <div className="settings-local-domain-progress" role="status" aria-live="polite">
                                    <LoaderCircle size={14} className="spin"/>
                                    <span className="settings-status-value">
                                        {localHTTPSPending
                                            ? 'Enabling local HTTPS — waiting for permission to trust Draft’s local certificate…'
                                            : 'Disabling local HTTPS — removing Draft’s trusted certificate…'}
                                    </span>
                                </div>
                            )}
                            {domainStatus?.httpsEnabled && (
                                <div className="settings-local-domain-status" role="status">
                                    <span className="settings-status-value">
                                        {domainStatus.httpsTrusted ? 'Local HTTPS is trusted and active' : 'Local HTTPS needs attention'}
                                    </span>
                                    {domainStatus.httpsAddr && <span className="settings-status-meta">HTTPS {domainStatus.httpsAddr}</span>}
                                    {domainStatus.httpsError && <span className="settings-status-error">{domainStatus.httpsError}</span>}
                                </div>
                            )}
                            <SettingsRow
                                label="Proxy listen port"
                                description="How Draft picks the reverse-proxy port. Port 80 lets URLs omit :port; the default keeps a fixed fallback when 80 is taken so URLs stay predictable. Applies on daemon restart."
                            >
                                <select
                                    className="input select-styled settings-url-preference-select"
                                    value={proxyPortMode}
                                    disabled={saving}
                                    onChange={(e) => {
                                        const value = e.target.value;
                                        setProxyPortMode(value);
                                        void persist({proxyPortMode: value});
                                    }}
                                >
                                    <option value="prefer80_fallback">Prefer 80, then custom fallback</option>
                                    <option value="prefer80">Prefer 80, then random</option>
                                    <option value="custom">Custom port (+ fallback)</option>
                                </select>
                            </SettingsRow>
                            {showPrimaryPort && (
                                <SettingsRow
                                    label="Custom proxy port"
                                    description="Tried first in custom mode. Use a rarely claimed loopback port."
                                >
                                    <input
                                        className="input settings-port-input"
                                        type="number"
                                        min={1}
                                        max={65535}
                                        value={proxyPort}
                                        disabled={saving}
                                        onChange={(e) => setProxyPort(clampPort(Number(e.target.value)))}
                                        onBlur={() => void persist({proxyPort: clampPort(proxyPort)})}
                                    />
                                </SettingsRow>
                            )}
                            {showFallbackPort && (
                                <SettingsRow
                                    label="Fallback proxy port"
                                    description="Tried after 80 (or after the custom port). If this is busy too, Draft uses a random port."
                                >
                                    <input
                                        className="input settings-port-input"
                                        type="number"
                                        min={1}
                                        max={65535}
                                        value={proxyFallbackPort}
                                        disabled={saving}
                                        onChange={(e) => setProxyFallbackPort(clampPort(Number(e.target.value)))}
                                        onBlur={() => void persist({proxyFallbackPort: clampPort(proxyFallbackPort)})}
                                    />
                                </SettingsRow>
                            )}
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

                        <SettingsSection title="About">
                            <SettingsRow
                                label="Version"
                                description="Installed Draft build."
                            >
                                <span className="settings-status-value settings-version-value">
                                    {appVersion ?? 'Unknown'}
                                </span>
                            </SettingsRow>
                        </SettingsSection>
                    </>
                )}
            </div>
        </div>
    );
}
