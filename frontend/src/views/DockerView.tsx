import {
    Box,
    Container as ContainerIcon,
    HardDrive,
    Layers,
    Network,
    Play,
    RefreshCw,
    Search,
    Square,
    RotateCw,
    Trash2,
} from 'lucide-react';
import {useCallback, useEffect, useMemo, useRef, useState} from 'react';
import {
    GetDockerDiskUsage,
    ListDockerContainers,
    StartDockerContainer,
    StopDockerContainer,
    RestartDockerContainer,
    RemoveDockerContainer,
    ListDockerImages,
    RemoveDockerImage,
    ListDockerNetworks,
    RemoveDockerNetwork,
    ListAllDockerVolumes,
    RemoveDockerVolume,
    PruneDocker,
} from '../../wailsjs/go/main/App';
import {deploy, types} from '../../wailsjs/go/models';
import {EventsOn} from '../../wailsjs/runtime/runtime';
import PageHeader from '../components/PageHeader';
import {formatBytes} from '../components/VolumeEditor';
import './DockerView.css';

type ResourceTab = 'containers' | 'images' | 'volumes' | 'networks';

function formatAge(input: string | number): string {
    const then = typeof input === 'number' ? input * 1000 : new Date(input).getTime();
    if (!Number.isFinite(then) || then <= 0) return '—';
    const secs = Math.max(0, (Date.now() - then) / 1000);
    if (secs < 60) return 'just now';
    const mins = secs / 60;
    if (mins < 60) return `${Math.floor(mins)}m ago`;
    const hours = mins / 60;
    if (hours < 24) return `${Math.floor(hours)}h ago`;
    const days = hours / 24;
    if (days < 30) return `${Math.floor(days)}d ago`;
    const months = days / 30;
    if (months < 12) return `${Math.floor(months)}mo ago`;
    return `${Math.floor(months / 12)}y ago`;
}

function shortId(id: string): string {
    return (id || '').replace(/^sha256:/, '').slice(0, 12);
}

function formatPorts(ports: deploy.ContainerSummary['ports']): string {
    if (!ports || ports.length === 0) return '—';
    return ports
        .map((p) => {
            const host = p.PublicPort ? `${p.IP || '0.0.0.0'}:${p.PublicPort}→` : '';
            return `${host}${p.PrivatePort}/${p.Type}`;
        })
        .join(', ');
}

export default function DockerView() {
    const [tab, setTab] = useState<ResourceTab>('containers');
    const [du, setDu] = useState<types.DiskUsage | null>(null);
    const [containers, setContainers] = useState<deploy.ContainerSummary[]>([]);
    const [images, setImages] = useState<deploy.ImageSummary[]>([]);
    const [volumes, setVolumes] = useState<deploy.VolumeOverview[]>([]);
    const [networks, setNetworks] = useState<deploy.NetworkSummary[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState('');
    const [search, setSearch] = useState('');
    const [draftOnly, setDraftOnly] = useState(true);
    const [lastAction, setLastAction] = useState('');
    const [busy, setBusy] = useState(false);
    const refreshTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

    const refresh = useCallback(() => {
        setLoading(true);
        setError('');
        Promise.all([
            GetDockerDiskUsage(),
            ListDockerContainers(),
            ListDockerImages(),
            ListAllDockerVolumes(),
            ListDockerNetworks(),
        ])
            .then(([diskUsage, c, i, v, n]) => {
                setDu(diskUsage);
                setContainers(c ?? []);
                setImages(i ?? []);
                setVolumes(v ?? []);
                setNetworks(n ?? []);
            })
            .catch((e) => setError(typeof e === 'string' ? e : e?.message || 'Failed to load Docker resources'))
            .finally(() => setLoading(false));
    }, []);

    useEffect(() => {
        refresh();
    }, [refresh]);

    // Live updates: dockerwatch already streams every container/image/network/
    // volume lifecycle event to the frontend as docker:activity/docker:status.
    // Debounce the refetch so a burst of events (e.g. a build finishing) doesn't
    // trigger a refresh per event.
    useEffect(() => {
        const scheduleRefresh = () => {
            if (refreshTimer.current) clearTimeout(refreshTimer.current);
            refreshTimer.current = setTimeout(refresh, 500);
        };
        const unsub1 = EventsOn('docker:activity', scheduleRefresh);
        const unsub2 = EventsOn('docker:status', scheduleRefresh);
        return () => {
            unsub1();
            unsub2();
            if (refreshTimer.current) clearTimeout(refreshTimer.current);
        };
    }, [refresh]);

    const summary = useMemo(() => {
        if (!du) return null;
        const imgs = du.Images ?? [];
        const vols = du.Volumes ?? [];
        const cache = du.BuildCache ?? [];
        const imagesTotal = imgs.reduce((s, i) => s + (i.Size || 0), 0);
        const imagesReclaimable = imgs.reduce((s, i) => s + (i.Containers === 0 ? i.Size || 0 : 0), 0);
        const volumesTotal = vols.reduce((s, v) => s + (v.UsageData?.Size || 0), 0);
        const volumesReclaimable = vols.reduce((s, v) => s + ((v.UsageData?.RefCount || 0) === 0 ? v.UsageData?.Size || 0 : 0), 0);
        const buildCacheTotal = cache.reduce((s, c) => s + (c.Size || 0), 0);
        const buildCacheReclaimable = cache.reduce((s, c) => s + (!c.InUse ? c.Size || 0 : 0), 0);
        // container.Summary's SizeRw/SizeRootFs aren't available on du.Containers
        // (Docker SDK struct quirk), so the containers total comes from our own
        // ContainerSummary list, which we already fetch for the Containers tab.
        const containersTotal = containers.reduce((s, c) => s + (c.sizeRw || 0) + (c.sizeRootFs || 0), 0);
        return {
            imagesTotal,
            imagesReclaimable,
            volumesTotal,
            volumesReclaimable,
            buildCacheTotal,
            buildCacheReclaimable,
            containersTotal,
            total: imagesTotal + volumesTotal + buildCacheTotal + containersTotal,
        };
    }, [du, containers]);

    const runAction = useCallback(async (fn: () => Promise<unknown>, onDone?: () => void) => {
        setBusy(true);
        try {
            await fn();
            onDone?.();
            refresh();
        } catch (e) {
            alert(String(e));
        } finally {
            setBusy(false);
        }
    }, [refresh]);

    const handleRemoveContainer = (c: deploy.ContainerSummary) => {
        const label = c.names?.[0]?.replace(/^\//, '') || shortId(c.id);
        if (!confirm(`Remove container "${label}"?${c.state === 'running' ? ' It is currently running.' : ''}`)) return;
        runAction(() => RemoveDockerContainer(c.id, c.state === 'running'));
    };

    const handleRemoveImage = (i: deploy.ImageSummary) => {
        const label = i.repoTags?.[0] || shortId(i.id);
        if (!confirm(`Remove image "${label}"?${i.containers > 0 ? ` It is used by ${i.containers} container(s).` : ''}`)) return;
        runAction(() => RemoveDockerImage(i.id, i.containers > 0));
    };

    const handleRemoveVolume = (v: deploy.VolumeOverview) => {
        if (!confirm(`Delete volume "${v.name}"? This permanently removes its data (${formatBytes(v.size)}).`)) return;
        runAction(() => RemoveDockerVolume(v.name, v.refCount > 0));
    };

    const handleRemoveNetwork = (n: deploy.NetworkSummary) => {
        if (!confirm(`Remove network "${n.name}"?`)) return;
        runAction(() => RemoveDockerNetwork(n.id));
    };

    const handlePrune = async (resource: string, label: string) => {
        const scope = draftOnly ? 'Draft-managed' : 'all';
        if (!confirm(`Remove unused ${scope} ${label}? This cannot be undone.`)) return;
        setBusy(true);
        try {
            const report = await PruneDocker(resource, draftOnly);
            setLastAction(`Freed ${formatBytes(report.spaceReclaimed || 0)} from ${label}${report.removed?.length ? ` (${report.removed.length} removed)` : ''}.`);
            refresh();
        } catch (e) {
            alert(String(e));
        } finally {
            setBusy(false);
        }
    };

    const q = search.trim().toLowerCase();
    const filteredContainers = useMemo(
        () => containers.filter((c) => !q || `${c.names?.join(' ')} ${c.image} ${c.status}`.toLowerCase().includes(q)),
        [containers, q],
    );
    const filteredImages = useMemo(
        () => images.filter((i) => !q || `${i.repoTags?.join(' ')} ${i.id}`.toLowerCase().includes(q)),
        [images, q],
    );
    const filteredVolumes = useMemo(
        () => volumes.filter((v) => !q || `${v.name} ${v.target} ${v.nodeLabel}`.toLowerCase().includes(q)),
        [volumes, q],
    );
    const filteredNetworks = useMemo(
        () => networks.filter((n) => !q || `${n.name} ${n.driver}`.toLowerCase().includes(q)),
        [networks, q],
    );

    const tabs: {id: ResourceTab; label: string; icon: typeof Box; count: number}[] = [
        {id: 'containers', label: 'Containers', icon: ContainerIcon, count: containers.length},
        {id: 'images', label: 'Images', icon: Layers, count: images.length},
        {id: 'volumes', label: 'Volumes', icon: HardDrive, count: volumes.length},
        {id: 'networks', label: 'Networks', icon: Network, count: networks.length},
    ];

    return (
        <div className="docker-view">
            <PageHeader
                title="Docker"
                description="Every container, image, volume, and network on this daemon — not just Draft's. See where disk space is going and clean it up."
                action={
                    <button className="btn btn-ghost" onClick={refresh} disabled={loading} title="Refresh">
                        <RefreshCw size={15} className={loading ? 'docker-spin' : ''}/> Refresh
                    </button>
                }
            />

            <div className="docker-layout">
                {error && <p className="form-error">{error}</p>}

                <div className="docker-summary">
                    <div className="docker-summary-total">
                        <span className="docker-summary-total-value">{summary ? formatBytes(summary.total) : '—'}</span>
                        <span className="docker-summary-total-label">Total Docker storage</span>
                        {lastAction && <span className="docker-summary-note">{lastAction}</span>}
                    </div>
                    <div className="docker-summary-grid">
                        <div className="docker-summary-cell">
                            <span className="docker-summary-value">{summary ? formatBytes(summary.containersTotal) : '—'}</span>
                            <span className="docker-summary-label">Containers</span>
                        </div>
                        <div className="docker-summary-cell">
                            <span className="docker-summary-value">{summary ? formatBytes(summary.imagesTotal) : '—'}</span>
                            <span className="docker-summary-label">Images · {summary ? formatBytes(summary.imagesReclaimable) : '—'} reclaimable</span>
                        </div>
                        <div className="docker-summary-cell">
                            <span className="docker-summary-value">{summary ? formatBytes(summary.volumesTotal) : '—'}</span>
                            <span className="docker-summary-label">Volumes · {summary ? formatBytes(summary.volumesReclaimable) : '—'} reclaimable</span>
                        </div>
                        <div className="docker-summary-cell">
                            <span className="docker-summary-value">{summary ? formatBytes(summary.buildCacheTotal) : '—'}</span>
                            <span className="docker-summary-label">Build cache · {summary ? formatBytes(summary.buildCacheReclaimable) : '—'} reclaimable</span>
                        </div>
                    </div>
                </div>

                <div className="docker-cleanup">
                    <label className="docker-scope-toggle">
                        <input type="checkbox" checked={draftOnly} onChange={(e) => setDraftOnly(e.target.checked)}/>
                        Draft-managed only
                    </label>
                    <button className="btn btn-ghost" disabled={busy} onClick={() => handlePrune('containers', 'stopped containers')}>
                        <Trash2 size={13}/> Stopped containers
                    </button>
                    <button className="btn btn-ghost" disabled={busy} onClick={() => handlePrune('images', 'images')}>
                        <Trash2 size={13}/> Unused images
                    </button>
                    <button className="btn btn-ghost" disabled={busy} onClick={() => handlePrune('volumes', 'volumes')}>
                        <Trash2 size={13}/> Unused volumes
                    </button>
                    <button className="btn btn-ghost" disabled={busy} onClick={() => handlePrune('networks', 'networks')}>
                        <Trash2 size={13}/> Unused networks
                    </button>
                    <button className="btn btn-ghost" disabled={busy} onClick={() => handlePrune('buildcache', 'build cache')} title="Build cache has no Draft-only scope — this always clears everything">
                        <Trash2 size={13}/> Build cache
                    </button>
                </div>

                <div className="docker-tabs">
                    {tabs.map((t) => {
                        const Icon = t.icon;
                        return (
                            <button
                                key={t.id}
                                type="button"
                                className={'docker-tab' + (tab === t.id ? ' active' : '')}
                                onClick={() => setTab(t.id)}
                            >
                                <Icon size={14}/> {t.label} <span className="docker-tab-count">{t.count}</span>
                            </button>
                        );
                    })}
                </div>

                <div className="docker-search">
                    <Search size={14}/>
                    <input
                        className="input"
                        placeholder="Search…"
                        value={search}
                        onChange={(e) => setSearch(e.target.value)}
                    />
                </div>

                {loading ? (
                    <div className="docker-empty">Loading…</div>
                ) : tab === 'containers' ? (
                    filteredContainers.length === 0 ? (
                        <div className="docker-empty">No containers found.</div>
                    ) : (
                        <div className="docker-table-wrap">
                            <table className="docker-table">
                                <thead>
                                    <tr>
                                        <th>Name</th>
                                        <th>Image</th>
                                        <th>Status</th>
                                        <th>Ports</th>
                                        <th className="docker-col-num">Size</th>
                                        <th>Created</th>
                                        <th>Owner</th>
                                        <th className="docker-col-actions"/>
                                    </tr>
                                </thead>
                                <tbody>
                                    {filteredContainers.map((c) => (
                                        <tr key={c.id}>
                                            <td><span className="docker-name">{c.names?.[0]?.replace(/^\//, '') || shortId(c.id)}</span></td>
                                            <td><code className="docker-mono">{c.image}</code></td>
                                            <td>
                                                <span className={`docker-status docker-status--${c.state === 'running' ? 'up' : 'down'}`}>{c.status}</span>
                                            </td>
                                            <td className="docker-mono-small">{formatPorts(c.ports)}</td>
                                            <td className="docker-col-num">{formatBytes((c.sizeRw || 0) + (c.sizeRootFs || 0))}</td>
                                            <td>{formatAge(c.created)}</td>
                                            <td>
                                                {c.managed ? (
                                                    <span className="docker-badge docker-badge--managed" title={c.projectName}>
                                                        {c.nodeLabel || c.projectName || 'Draft'}
                                                    </span>
                                                ) : (
                                                    <span className="docker-badge docker-badge--external">External</span>
                                                )}
                                            </td>
                                            <td className="docker-col-actions">
                                                <div className="docker-row-actions">
                                                    {c.state === 'running' ? (
                                                        <>
                                                            <button className="btn btn-ghost docker-icon-btn" title="Stop" disabled={busy} onClick={() => runAction(() => StopDockerContainer(c.id))}>
                                                                <Square size={14}/>
                                                            </button>
                                                            <button className="btn btn-ghost docker-icon-btn" title="Restart" disabled={busy} onClick={() => runAction(() => RestartDockerContainer(c.id))}>
                                                                <RotateCw size={14}/>
                                                            </button>
                                                        </>
                                                    ) : (
                                                        <button className="btn btn-ghost docker-icon-btn" title="Start" disabled={busy} onClick={() => runAction(() => StartDockerContainer(c.id))}>
                                                            <Play size={14}/>
                                                        </button>
                                                    )}
                                                    <button className="btn btn-ghost docker-icon-btn docker-icon-btn--danger" title="Remove" disabled={busy} onClick={() => handleRemoveContainer(c)}>
                                                        <Trash2 size={14}/>
                                                    </button>
                                                </div>
                                            </td>
                                        </tr>
                                    ))}
                                </tbody>
                            </table>
                        </div>
                    )
                ) : tab === 'images' ? (
                    filteredImages.length === 0 ? (
                        <div className="docker-empty">No images found.</div>
                    ) : (
                        <div className="docker-table-wrap">
                            <table className="docker-table">
                                <thead>
                                    <tr>
                                        <th>Repository:Tag</th>
                                        <th>Image ID</th>
                                        <th className="docker-col-num">Size</th>
                                        <th className="docker-col-num">Shared</th>
                                        <th className="docker-col-num">In use</th>
                                        <th>Created</th>
                                        <th>Owner</th>
                                        <th className="docker-col-actions"/>
                                    </tr>
                                </thead>
                                <tbody>
                                    {filteredImages.map((i) => (
                                        <tr key={i.id}>
                                            <td>
                                                {i.repoTags && i.repoTags.length > 0 ? (
                                                    <span className="docker-name">{i.repoTags[0]}</span>
                                                ) : (
                                                    <span className="docker-badge docker-badge--external">dangling</span>
                                                )}
                                            </td>
                                            <td><code className="docker-mono">{shortId(i.id)}</code></td>
                                            <td className="docker-col-num">{formatBytes(i.size)}</td>
                                            <td className="docker-col-num">{formatBytes(i.sharedSize)}</td>
                                            <td className="docker-col-num">{i.containers}</td>
                                            <td>{formatAge(i.created)}</td>
                                            <td>
                                                {i.managed ? (
                                                    <span className="docker-badge docker-badge--managed">Draft build</span>
                                                ) : (
                                                    <span className="docker-badge docker-badge--external">External</span>
                                                )}
                                            </td>
                                            <td className="docker-col-actions">
                                                <button className="btn btn-ghost docker-icon-btn docker-icon-btn--danger" title="Remove" disabled={busy} onClick={() => handleRemoveImage(i)}>
                                                    <Trash2 size={14}/>
                                                </button>
                                            </td>
                                        </tr>
                                    ))}
                                </tbody>
                            </table>
                        </div>
                    )
                ) : tab === 'volumes' ? (
                    filteredVolumes.length === 0 ? (
                        <div className="docker-empty">No volumes found.</div>
                    ) : (
                        <div className="docker-table-wrap">
                            <table className="docker-table">
                                <thead>
                                    <tr>
                                        <th>Name</th>
                                        <th>Driver</th>
                                        <th className="docker-col-num">Size</th>
                                        <th className="docker-col-num">Refs</th>
                                        <th>Owner</th>
                                        <th>Created</th>
                                        <th className="docker-col-actions"/>
                                    </tr>
                                </thead>
                                <tbody>
                                    {filteredVolumes.map((v) => (
                                        <tr key={v.name}>
                                            <td><span className="docker-name docker-mono">{v.name}</span></td>
                                            <td>{v.driver || 'local'}</td>
                                            <td className="docker-col-num">{formatBytes(v.size)}</td>
                                            <td className="docker-col-num">{v.refCount}</td>
                                            <td>
                                                {v.managed ? (
                                                    <span className={`docker-badge ${v.orphaned ? 'docker-badge--warn' : 'docker-badge--managed'}`}>
                                                        {v.orphaned ? 'Orphaned' : v.nodeLabel || 'Draft'}
                                                    </span>
                                                ) : (
                                                    <span className="docker-badge docker-badge--external">External</span>
                                                )}
                                            </td>
                                            <td>{formatAge(v.createdAt)}</td>
                                            <td className="docker-col-actions">
                                                <button className="btn btn-ghost docker-icon-btn docker-icon-btn--danger" title="Remove" disabled={busy} onClick={() => handleRemoveVolume(v)}>
                                                    <Trash2 size={14}/>
                                                </button>
                                            </td>
                                        </tr>
                                    ))}
                                </tbody>
                            </table>
                        </div>
                    )
                ) : filteredNetworks.length === 0 ? (
                    <div className="docker-empty">No networks found.</div>
                ) : (
                    <div className="docker-table-wrap">
                        <table className="docker-table">
                            <thead>
                                <tr>
                                    <th>Name</th>
                                    <th>Driver</th>
                                    <th>Scope</th>
                                    <th className="docker-col-num">Containers</th>
                                    <th>Owner</th>
                                    <th className="docker-col-actions"/>
                                </tr>
                            </thead>
                            <tbody>
                                {filteredNetworks.map((n) => (
                                    <tr key={n.id}>
                                        <td><span className="docker-name">{n.name}</span></td>
                                        <td>{n.driver}</td>
                                        <td>{n.scope}</td>
                                        <td className="docker-col-num">{n.containers}</td>
                                        <td>
                                            {n.managed ? (
                                                <span className="docker-badge docker-badge--managed">Draft</span>
                                            ) : (
                                                <span className="docker-badge docker-badge--external">External</span>
                                            )}
                                        </td>
                                        <td className="docker-col-actions">
                                            <button
                                                className="btn btn-ghost docker-icon-btn docker-icon-btn--danger"
                                                title="Remove"
                                                disabled={busy || n.name === 'bridge' || n.name === 'host' || n.name === 'none'}
                                                onClick={() => handleRemoveNetwork(n)}
                                            >
                                                <Trash2 size={14}/>
                                            </button>
                                        </td>
                                    </tr>
                                ))}
                            </tbody>
                        </table>
                    </div>
                )}
            </div>
        </div>
    );
}
