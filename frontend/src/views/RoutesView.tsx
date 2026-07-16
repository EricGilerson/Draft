import {ExternalLink, Globe, Network, RefreshCw, Route as RouteIcon, Search, Server} from 'lucide-react';
import {useCallback, useEffect, useMemo, useState} from 'react';
import {ListRoutes} from '../../wailsjs/go/main/App';
import {main, store} from '../../wailsjs/go/models';
import PageHeader from '../components/PageHeader';
import {SkeletonBlock, SkeletonStats, SkeletonTable} from '../components/Skeleton';
import './RoutesView.css';

type RoutesViewProps = {
    projects: store.Project[];
    /** Open the owning project's canvas and select the service a route points at. */
    onRevealNode?: (projectId: number, nodeId: string) => void;
};

type ProtocolFilter = 'all' | 'http' | 'tcp';
type ScopeFilter = 'all' | 'public' | 'internal';

const PUBLIC_SUFFIX = '.draft.resolv.sh';
const INTERNAL_SUFFIX = '.draft.local';

function isPublicHost(hostname: string): boolean {
    return hostname.endsWith(PUBLIC_SUFFIX);
}

function isInternalHost(hostname: string): boolean {
    return hostname.endsWith(INTERNAL_SUFFIX);
}

function protocolBadgeClass(protocol: string): string {
    if (protocol === 'tcp') return 'routes-protocol routes-protocol--tcp';
    return 'routes-protocol routes-protocol--http';
}

// RoutesView is the read-only v1 ingress surface: every Route Draft has
// registered across all projects, filterable by project / protocol / scope.
// Routes are created implicitly at deploy time and removed when a service is
// stopped, so this view reflects live Docker state rather than a hand-edited
// config. v2 will add custom ingress rules on top of this same table.
export default function RoutesView({projects, onRevealNode}: RoutesViewProps) {
    const [routes, setRoutes] = useState<main.RouteRow[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState('');
    const [search, setSearch] = useState('');
    const [projectFilter, setProjectFilter] = useState<string>('all');
    const [protocolFilter, setProtocolFilter] = useState<ProtocolFilter>('all');
    const [scopeFilter, setScopeFilter] = useState<ScopeFilter>('all');

    const refresh = useCallback(() => {
        setError('');
        ListRoutes(null)
            .then((list) => setRoutes(list ?? []))
            .catch((e) => setError(typeof e === 'string' ? e : e?.message || 'Failed to load routes'))
            .finally(() => setLoading(false));
    }, []);

    useEffect(() => {
        refresh();
    }, [refresh]);

    const projectName = (projectId: number): string => {
        const p = projects.find((proj) => proj.id === projectId);
        return p?.name || `Project ${projectId}`;
    };

    const projectOptions = useMemo(() => {
        const seen = new Map<string, string>();
        for (const r of routes) {
            const key = String(r.projectId || 0);
            if (!seen.has(key)) seen.set(key, projectName(r.projectId));
        }
        return [...seen.entries()].sort((a, b) => a[1].localeCompare(b[1]));
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [routes, projects]);

    const stats = useMemo(() => {
        let http = 0;
        let tcp = 0;
        let pub = 0;
        for (const r of routes) {
            if (r.protocol === 'tcp') tcp++;
            else http++;
            if (isPublicHost(r.hostname)) pub++;
        }
        return {total: routes.length, http, tcp, public: pub, internal: routes.length - pub};
    }, [routes]);

    const filtered = useMemo(() => {
        const q = search.trim().toLowerCase();
        return routes.filter((r) => {
            if (projectFilter !== 'all' && String(r.projectId || 0) !== projectFilter) return false;
            if (protocolFilter !== 'all' && r.protocol !== protocolFilter) return false;
            if (scopeFilter === 'public' && !isPublicHost(r.hostname)) return false;
            if (scopeFilter === 'internal' && !isInternalHost(r.hostname)) return false;
            if (q) {
                const hay = `${r.hostname} ${r.serviceName} ${r.projectName} ${r.targetHost}:${r.targetPort}`.toLowerCase();
                if (!hay.includes(q)) return false;
            }
            return true;
        });
    }, [routes, search, projectFilter, protocolFilter, scopeFilter]);

    return (
        <div className="routes-view">
            <PageHeader
                title="Routes"
                description="Every hostname and port Draft is routing across all projects — the local ingress layer. HTTP routes are proxied; TCP routes (databases) resolve to a stable localhost port."
                action={
                    <button className="btn btn-ghost" onClick={refresh} disabled={loading} title="Refresh">
                        <RefreshCw size={15} className={loading ? 'routes-spin' : ''}/> Refresh
                    </button>
                }
            />

            <div className="routes-layout">
                {error && <p className="form-error">{error}</p>}

                {loading ? (
                    <SkeletonBlock label="Loading routes" className="routes-skel">
                        <SkeletonStats count={4} />
                        <div style={{height: 12}} />
                        <SkeletonTable
                            rows={8}
                            columns={['1.4fr', '72px', '1fr', '1fr', '1.2fr', '80px', '72px', '48px']}
                        />
                    </SkeletonBlock>
                ) : (
                    <>
                        <div className="routes-stats">
                            <div className="routes-stat">
                                <span className="routes-stat-value">{stats.total}</span>
                                <span className="routes-stat-label">Routes</span>
                            </div>
                            <div className="routes-stat">
                                <span className="routes-stat-value">{stats.http}</span>
                                <span className="routes-stat-label">HTTP (proxied)</span>
                            </div>
                            <div className="routes-stat">
                                <span className="routes-stat-value">{stats.tcp}</span>
                                <span className="routes-stat-label">TCP (databases)</span>
                            </div>
                            <div className="routes-stat">
                                <span className="routes-stat-value">{stats.public}</span>
                                <span className="routes-stat-label">Public hostnames</span>
                            </div>
                        </div>

                        <div className="routes-toolbar">
                            <div className="routes-search">
                                <Search size={14}/>
                                <input
                                    className="input"
                                    placeholder="Search hostname, service, or project…"
                                    value={search}
                                    onChange={(e) => setSearch(e.target.value)}
                                />
                            </div>
                            <select className="input select-styled routes-select" value={projectFilter} onChange={(e) => setProjectFilter(e.target.value)}>
                                <option value="all">All projects</option>
                                {projectOptions.map(([id, label]) => (
                                    <option key={id} value={id}>{label}</option>
                                ))}
                            </select>
                            <select className="input select-styled routes-select" value={protocolFilter} onChange={(e) => setProtocolFilter(e.target.value as ProtocolFilter)}>
                                <option value="all">All protocols</option>
                                <option value="http">HTTP</option>
                                <option value="tcp">TCP</option>
                            </select>
                            <select className="input select-styled routes-select" value={scopeFilter} onChange={(e) => setScopeFilter(e.target.value as ScopeFilter)}>
                                <option value="all">All scopes</option>
                                <option value="public">Public</option>
                                <option value="internal">Internal</option>
                            </select>
                        </div>

                        {routes.length === 0 ? (
                            <div className="routes-empty">
                                <RouteIcon size={20}/>
                                <div className="routes-empty-copy">
                                    <strong>No routes yet.</strong>
                                    <span>Deploy a service and its hostname will appear here once it's been registered.</span>
                                </div>
                            </div>
                        ) : filtered.length === 0 ? (
                            <div className="routes-empty">
                                <Search size={20}/>
                                <div className="routes-empty-copy">
                                    <strong>No routes match your filters.</strong>
                                    <span>Try clearing the search or widening the protocol/scope filter.</span>
                                </div>
                            </div>
                        ) : (
                            <div className="routes-table-wrap">
                                <table className="routes-table">
                                    <thead>
                                        <tr>
                                            <th>Hostname</th>
                                            <th>Protocol</th>
                                            <th>Project</th>
                                            <th>Service</th>
                                            <th>Target</th>
                                            <th className="routes-col-num">Host port</th>
                                            <th>Scope</th>
                                            <th className="routes-col-actions"/>
                                        </tr>
                                    </thead>
                                    <tbody>
                                        {filtered.map((r) => {
                                            const pub = isPublicHost(r.hostname);
                                            const isTCP = r.protocol === 'tcp';
                                            const target = `${r.targetHost}:${r.targetPort}`;
                                            return (
                                                <tr key={r.hostname}>
                                                    <td>
                                                        <code className="routes-hostname" title={r.hostname}>{r.hostname}</code>
                                                    </td>
                                                    <td>
                                                        <span className={protocolBadgeClass(r.protocol)}>
                                                            {r.protocol?.toUpperCase() || 'HTTP'}
                                                        </span>
                                                    </td>
                                                    <td>
                                                        <span className="routes-project">{r.projectName || projectName(r.projectId)}</span>
                                                    </td>
                                                    <td>
                                                        <span className="routes-service">{r.serviceName || r.nodeId}</span>
                                                    </td>
                                                    <td>
                                                        <code className="routes-target">{target}</code>
                                                    </td>
                                                    <td className="routes-col-num">
                                                        {isTCP ? (
                                                            <code className="routes-hostport">localhost:{r.hostPort || '—'}</code>
                                                        ) : (
                                                            <span className="routes-muted">proxied</span>
                                                        )}
                                                    </td>
                                                    <td>
                                                        <span className={`routes-scope routes-scope--${pub ? 'public' : 'internal'}`}>
                                                            {pub ? <Globe size={12}/> : <Network size={12}/>}
                                                            {pub ? 'Public' : 'Internal'}
                                                        </span>
                                                    </td>
                                                    <td className="routes-col-actions">
                                                        <div className="routes-row-actions">
                                                            {onRevealNode && (
                                                                <button
                                                                    className="btn btn-ghost routes-icon-btn"
                                                                    onClick={() => onRevealNode(r.projectId, r.nodeId)}
                                                                    title="Open on canvas"
                                                                >
                                                                    <ExternalLink size={14}/>
                                                                </button>
                                                            )}
                                                            {isTCP ? (
                                                                <Server size={14} className="routes-kind-icon" aria-label="TCP service"/>
                                                            ) : (
                                                                <RouteIcon size={14} className="routes-kind-icon" aria-label="HTTP service"/>
                                                            )}
                                                        </div>
                                                    </td>
                                                </tr>
                                            );
                                        })}
                                    </tbody>
                                </table>
                            </div>
                        )}
                    </>
                )}
            </div>
        </div>
    );
}
