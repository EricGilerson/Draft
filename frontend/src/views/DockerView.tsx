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
import Dialog from '../components/Dialog';
import PageHeader from '../components/PageHeader';
import {formatBytes} from '../components/VolumeEditor';
import './DockerView.css';

type ResourceTab = 'containers' | 'images' | 'volumes' | 'networks';
type ContainerSortKey = 'recent' | 'size' | 'name';
type ImageSortKey = 'recent' | 'size' | 'name';
type VolumeSortKey = 'recent' | 'size' | 'name';
type NetworkSortKey = 'recent' | 'usage' | 'name';

type ConfirmState = {
    message: string;
    detail?: string;
    onConfirm: () => void;
} | null;

function visibleRepoTags(tags: string[] | undefined | null): string[] {
    return (tags ?? []).filter((tag) => tag && tag !== '<none>:<none>');
}

function timeValue(input: string | number): number {
    return typeof input === 'number' ? input * 1000 : new Date(input).getTime();
}

function formatAge(input: string | number): string {
    const then = timeValue(input);
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

function formatTimestamp(input: string | number): string {
    const then = timeValue(input);
    if (!Number.isFinite(then) || then <= 0) return '';
    return new Date(then).toLocaleString();
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

function toggleSet<T>(set: Set<T>, value: T): Set<T> {
    const next = new Set(set);
    if (next.has(value)) next.delete(value);
    else next.add(value);
    return next;
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
    const [actionError, setActionError] = useState('');
    const [search, setSearch] = useState('');
    const [lastAction, setLastAction] = useState('');
    const [busy, setBusy] = useState(false);
    const [confirmState, setConfirmState] = useState<ConfirmState>(null);
    const [selectedContainers, setSelectedContainers] = useState<Set<string>>(new Set());
    const [selectedImages, setSelectedImages] = useState<Set<string>>(new Set());
    const [selectedVolumes, setSelectedVolumes] = useState<Set<string>>(new Set());
    const [selectedNetworks, setSelectedNetworks] = useState<Set<string>>(new Set());
    const [containerSort, setContainerSort] = useState<ContainerSortKey>('recent');
    const [imageSort, setImageSort] = useState<ImageSortKey>('recent');
    const [volumeSort, setVolumeSort] = useState<VolumeSortKey>('recent');
    const [networkSort, setNetworkSort] = useState<NetworkSortKey>('recent');
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

    // Clear selections tied to stale ids whenever the underlying lists refresh.
    useEffect(() => {
        const ids = new Set(containers.map((c) => c.id));
        setSelectedContainers((prev) => new Set([...prev].filter((id) => ids.has(id))));
    }, [containers]);
    useEffect(() => {
        const ids = new Set(images.map((i) => i.id));
        setSelectedImages((prev) => new Set([...prev].filter((id) => ids.has(id))));
    }, [images]);
    useEffect(() => {
        const names = new Set(volumes.map((v) => v.name));
        setSelectedVolumes((prev) => new Set([...prev].filter((name) => names.has(name))));
    }, [volumes]);
    useEffect(() => {
        const ids = new Set(networks.map((n) => n.id));
        setSelectedNetworks((prev) => new Set([...prev].filter((id) => ids.has(id))));
    }, [networks]);

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
        setActionError('');
        try {
            await fn();
            onDone?.();
            refresh();
        } catch (e) {
            setActionError(String(e));
        } finally {
            setBusy(false);
        }
    }, [refresh]);

    const runBulk = useCallback(async (ids: string[], performRemove: (id: string) => Promise<unknown>, onDone?: () => void) => {
        setBusy(true);
        setActionError('');
        const failures: string[] = [];
        for (const id of ids) {
            try {
                await performRemove(id);
            } catch (e) {
                failures.push(`${shortId(id) || id}: ${String(e)}`);
            }
        }
        onDone?.();
        refresh();
        setBusy(false);
        if (failures.length > 0) {
            setActionError(failures.join('\n'));
        }
    }, [refresh]);

    const askConfirm = (message: string, onConfirm: () => void, detail?: string) => {
        setConfirmState({message, detail, onConfirm});
    };

    const handleRemoveContainer = (c: deploy.ContainerSummary) => {
        const label = c.names?.[0]?.replace(/^\//, '') || shortId(c.id);
        askConfirm(
            `Remove container "${label}"?`,
            () => {
                setConfirmState(null);
                runAction(() => RemoveDockerContainer(c.id, c.state === 'running'));
            },
            c.state === 'running' ? 'It is currently running.' : undefined,
        );
    };

    const handleRemoveImage = (i: deploy.ImageSummary) => {
        const tags = visibleRepoTags(i.repoTags);
        const label = tags[0] || shortId(i.id);
        const details: string[] = [];
        if (tags.length > 1) {
            details.push(`This image ID also has ${tags.length - 1} other tag${tags.length === 2 ? '' : 's'}: ${tags.join(', ')}. Removing it deletes every tag on that image ID.`);
        }
        if (i.containers > 0) {
            details.push(`It is used by ${i.containers} container(s).`);
        }
        askConfirm(
            `Remove image "${label}"?`,
            () => {
                setConfirmState(null);
                runAction(
                    () => RemoveDockerImage(i.id, i.containers > 0),
                    () => setLastAction(`Removed image ${shortId(i.id)}${tags[0] ? ` (${tags[0]})` : ''}.`),
                );
            },
            details.length > 0 ? details.join(' ') : undefined,
        );
    };

    const handleRemoveVolume = (v: deploy.VolumeOverview) => {
        askConfirm(
            `Delete volume "${v.name}"? This permanently removes its data (${formatBytes(v.size)}).`,
            () => {
                setConfirmState(null);
                runAction(() => RemoveDockerVolume(v.name, v.refCount > 0));
            },
        );
    };

    const handleRemoveNetwork = (n: deploy.NetworkSummary) => {
        askConfirm(`Remove network "${n.name}"?`, () => {
            setConfirmState(null);
            runAction(() => RemoveDockerNetwork(n.id));
        });
    };

    const handleBulkRemoveContainers = (rows: deploy.ContainerSummary[]) => {
        if (rows.length === 0) return;
        const anyRunning = rows.some((c) => c.state === 'running');
        askConfirm(
            `Remove ${rows.length} selected container${rows.length > 1 ? 's' : ''}?`,
            () => {
                setConfirmState(null);
                runBulk(
                    rows.map((c) => c.id),
                    (id) => {
                        const row = rows.find((c) => c.id === id);
                        return RemoveDockerContainer(id, row?.state === 'running');
                    },
                    () => setSelectedContainers(new Set()),
                );
            },
            anyRunning ? 'Some of these are currently running.' : undefined,
        );
    };

    const handleBulkRemoveImages = (rows: deploy.ImageSummary[]) => {
        if (rows.length === 0) return;
        const anyInUse = rows.some((i) => i.containers > 0);
        askConfirm(
            `Remove ${rows.length} selected image${rows.length > 1 ? 's' : ''}?`,
            () => {
                setConfirmState(null);
                runBulk(
                    rows.map((i) => i.id),
                    (id) => {
                        const row = rows.find((i) => i.id === id);
                        return RemoveDockerImage(id, (row?.containers || 0) > 0);
                    },
                    () => {
                        setSelectedImages(new Set());
                        setLastAction(`Removed ${rows.length} image${rows.length === 1 ? '' : 's'}.`);
                    },
                );
            },
            anyInUse ? 'Some of these are used by existing containers.' : undefined,
        );
    };

    const handleBulkRemoveVolumes = (rows: deploy.VolumeOverview[]) => {
        if (rows.length === 0) return;
        askConfirm(
            `Delete ${rows.length} selected volume${rows.length > 1 ? 's' : ''}? This permanently removes their data.`,
            () => {
                setConfirmState(null);
                runBulk(
                    rows.map((v) => v.name),
                    (name) => {
                        const row = rows.find((v) => v.name === name);
                        return RemoveDockerVolume(name, (row?.refCount || 0) > 0);
                    },
                    () => setSelectedVolumes(new Set()),
                );
            },
        );
    };

    const handleBulkRemoveNetworks = (rows: deploy.NetworkSummary[]) => {
        if (rows.length === 0) return;
        askConfirm(
            `Remove ${rows.length} selected network${rows.length > 1 ? 's' : ''}?`,
            () => {
                setConfirmState(null);
                runBulk(
                    rows.map((n) => n.id),
                    (id) => RemoveDockerNetwork(id),
                    () => setSelectedNetworks(new Set()),
                );
            },
        );
    };

    const handlePrune = (resource: string, label: string) => {
        askConfirm(`Remove unused ${label}? This cannot be undone.`, async () => {
            setConfirmState(null);
            setBusy(true);
            setActionError('');
            try {
                const report = await PruneDocker(resource, false);
                setLastAction(`Freed ${formatBytes(report.spaceReclaimed || 0)} from ${label}${report.removed?.length ? ` (${report.removed.length} removed)` : ''}.`);
                refresh();
            } catch (e) {
                setActionError(String(e));
            } finally {
                setBusy(false);
            }
        });
    };

    const q = search.trim().toLowerCase();
    const filteredContainers = useMemo(() => {
        const rows = containers.filter((c) => !q || `${c.names?.join(' ')} ${c.image} ${c.status}`.toLowerCase().includes(q));
        rows.sort((a, b) => {
            if (containerSort === 'name') return (a.names?.[0] || a.id).localeCompare(b.names?.[0] || b.id);
            if (containerSort === 'size') return ((b.sizeRw || 0) + (b.sizeRootFs || 0)) - ((a.sizeRw || 0) + (a.sizeRootFs || 0));
            return timeValue(b.created) - timeValue(a.created);
        });
        return rows;
    }, [containers, q, containerSort]);
    const filteredImages = useMemo(() => {
        const rows = images.filter((i) => !q || `${i.repoTags?.join(' ')} ${i.id}`.toLowerCase().includes(q));
        rows.sort((a, b) => {
            if (imageSort === 'name') {
                const aLabel = visibleRepoTags(a.repoTags)[0] || a.id;
                const bLabel = visibleRepoTags(b.repoTags)[0] || b.id;
                return aLabel.localeCompare(bLabel);
            }
            if (imageSort === 'size') return (b.size || 0) - (a.size || 0);
            return timeValue(b.created) - timeValue(a.created);
        });
        return rows;
    }, [images, q, imageSort]);
    const filteredVolumes = useMemo(() => {
        const rows = volumes.filter((v) => !q || `${v.name} ${v.target} ${v.nodeLabel}`.toLowerCase().includes(q));
        rows.sort((a, b) => {
            if (volumeSort === 'name') return a.name.localeCompare(b.name);
            if (volumeSort === 'size') return (b.size || 0) - (a.size || 0);
            return timeValue(b.createdAt) - timeValue(a.createdAt);
        });
        return rows;
    }, [volumes, q, volumeSort]);
    const filteredNetworks = useMemo(() => {
        const rows = networks.filter((n) => !q || `${n.name} ${n.driver}`.toLowerCase().includes(q));
        rows.sort((a, b) => {
            if (networkSort === 'name') return a.name.localeCompare(b.name);
            if (networkSort === 'usage') return (b.containers || 0) - (a.containers || 0);
            return timeValue(b.created) - timeValue(a.created);
        });
        return rows;
    }, [networks, q, networkSort]);
    const selectableNetworks = useMemo(
        () => filteredNetworks.filter((n) => n.name !== 'bridge' && n.name !== 'host' && n.name !== 'none'),
        [filteredNetworks],
    );
    const sortLabel =
        tab === 'containers' ? containerSort :
            tab === 'images' ? imageSort :
                tab === 'volumes' ? volumeSort :
                    networkSort;

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
                {actionError && (
                    <p className="form-error docker-action-error">
                        {actionError}
                        <button className="btn btn-ghost docker-dismiss-error" onClick={() => setActionError('')}>Dismiss</button>
                    </p>
                )}

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
                    <button className="btn btn-ghost" disabled={busy} onClick={() => handlePrune('buildcache', 'build cache')}>
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

                <div className="docker-controls">
                    <div className="docker-search">
                        <Search size={14}/>
                        <input
                            className="input"
                            placeholder="Search…"
                            value={search}
                            onChange={(e) => setSearch(e.target.value)}
                        />
                    </div>
                    <label className="docker-sort">
                        <span>Sort</span>
                        {tab === 'containers' ? (
                            <select className="input select-styled docker-sort-select" value={sortLabel} onChange={(e) => setContainerSort(e.target.value as ContainerSortKey)}>
                                <option value="recent">Most recent</option>
                                <option value="size">Largest size</option>
                                <option value="name">Name</option>
                            </select>
                        ) : tab === 'images' ? (
                            <select className="input select-styled docker-sort-select" value={sortLabel} onChange={(e) => setImageSort(e.target.value as ImageSortKey)}>
                                <option value="recent">Most recent</option>
                                <option value="size">Largest size</option>
                                <option value="name">Name</option>
                            </select>
                        ) : tab === 'volumes' ? (
                            <select className="input select-styled docker-sort-select" value={sortLabel} onChange={(e) => setVolumeSort(e.target.value as VolumeSortKey)}>
                                <option value="recent">Most recent</option>
                                <option value="size">Largest size</option>
                                <option value="name">Name</option>
                            </select>
                        ) : (
                            <select className="input select-styled docker-sort-select" value={sortLabel} onChange={(e) => setNetworkSort(e.target.value as NetworkSortKey)}>
                                <option value="recent">Most recent</option>
                                <option value="usage">Most containers</option>
                                <option value="name">Name</option>
                            </select>
                        )}
                    </label>
                </div>

                {tab === 'containers' && selectedContainers.size > 0 && (
                    <div className="docker-bulk-bar">
                        <span>{selectedContainers.size} selected</span>
                        <button className="btn btn-ghost" disabled={busy} onClick={() => setSelectedContainers(new Set())}>Clear</button>
                        <button
                            className="btn btn-ghost docker-icon-btn--danger"
                            disabled={busy}
                            onClick={() => handleBulkRemoveContainers(filteredContainers.filter((c) => selectedContainers.has(c.id)))}
                        >
                            <Trash2 size={13}/> Remove selected
                        </button>
                    </div>
                )}
                {tab === 'images' && selectedImages.size > 0 && (
                    <div className="docker-bulk-bar">
                        <span>{selectedImages.size} selected</span>
                        <button className="btn btn-ghost" disabled={busy} onClick={() => setSelectedImages(new Set())}>Clear</button>
                        <button
                            className="btn btn-ghost docker-icon-btn--danger"
                            disabled={busy}
                            onClick={() => handleBulkRemoveImages(filteredImages.filter((i) => selectedImages.has(i.id)))}
                        >
                            <Trash2 size={13}/> Remove selected
                        </button>
                    </div>
                )}
                {tab === 'volumes' && selectedVolumes.size > 0 && (
                    <div className="docker-bulk-bar">
                        <span>{selectedVolumes.size} selected</span>
                        <button className="btn btn-ghost" disabled={busy} onClick={() => setSelectedVolumes(new Set())}>Clear</button>
                        <button
                            className="btn btn-ghost docker-icon-btn--danger"
                            disabled={busy}
                            onClick={() => handleBulkRemoveVolumes(filteredVolumes.filter((v) => selectedVolumes.has(v.name)))}
                        >
                            <Trash2 size={13}/> Remove selected
                        </button>
                    </div>
                )}
                {tab === 'networks' && selectedNetworks.size > 0 && (
                    <div className="docker-bulk-bar">
                        <span>{selectedNetworks.size} selected</span>
                        <button className="btn btn-ghost" disabled={busy} onClick={() => setSelectedNetworks(new Set())}>Clear</button>
                        <button
                            className="btn btn-ghost docker-icon-btn--danger"
                            disabled={busy}
                            onClick={() => handleBulkRemoveNetworks(filteredNetworks.filter((n) => selectedNetworks.has(n.id)))}
                        >
                            <Trash2 size={13}/> Remove selected
                        </button>
                    </div>
                )}

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
                                        <th className="docker-col-check">
                                            <input
                                                type="checkbox"
                                                checked={filteredContainers.length > 0 && filteredContainers.every((c) => selectedContainers.has(c.id))}
                                                onChange={(e) => setSelectedContainers(e.target.checked ? new Set(filteredContainers.map((c) => c.id)) : new Set())}
                                            />
                                        </th>
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
                                            <td className="docker-col-check">
                                                <input
                                                    type="checkbox"
                                                    checked={selectedContainers.has(c.id)}
                                                    onChange={() => setSelectedContainers((prev) => toggleSet(prev, c.id))}
                                                />
                                            </td>
                                            <td><span className="docker-name">{c.names?.[0]?.replace(/^\//, '') || shortId(c.id)}</span></td>
                                            <td><code className="docker-mono">{c.image}</code></td>
                                            <td>
                                                <span className={`docker-status docker-status--${c.state === 'running' ? 'up' : 'down'}`}>{c.status}</span>
                                            </td>
                                            <td className="docker-mono-small">{formatPorts(c.ports)}</td>
                                            <td className="docker-col-num">{formatBytes((c.sizeRw || 0) + (c.sizeRootFs || 0))}</td>
                                            <td title={formatTimestamp(c.created)}>{formatAge(c.created)}</td>
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
                                        <th className="docker-col-check">
                                            <input
                                                type="checkbox"
                                                checked={filteredImages.length > 0 && filteredImages.every((i) => selectedImages.has(i.id))}
                                                onChange={(e) => setSelectedImages(e.target.checked ? new Set(filteredImages.map((i) => i.id)) : new Set())}
                                            />
                                        </th>
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
                                    {filteredImages.map((i) => {
                                        const tags = visibleRepoTags(i.repoTags);
                                        return (
                                            <tr key={i.id}>
                                                <td className="docker-col-check">
                                                    <input
                                                        type="checkbox"
                                                        checked={selectedImages.has(i.id)}
                                                        onChange={() => setSelectedImages((prev) => toggleSet(prev, i.id))}
                                                    />
                                                </td>
                                                <td>
                                                    {tags.length > 0 ? (
                                                        <div className="docker-image-tags" title={tags.join('\n')}>
                                                            <span className="docker-name">{tags[0]}</span>
                                                            {tags.length > 1 && (
                                                                <span className="docker-tag-more">+{tags.length - 1} more</span>
                                                            )}
                                                        </div>
                                                    ) : (
                                                        <span className="docker-badge docker-badge--external">dangling</span>
                                                    )}
                                                </td>
                                                <td><code className="docker-mono">{shortId(i.id)}</code></td>
                                                <td className="docker-col-num">{formatBytes(i.size)}</td>
                                                <td className="docker-col-num">{formatBytes(i.sharedSize)}</td>
                                                <td className="docker-col-num">{i.containers}</td>
                                                <td title={formatTimestamp(i.created)}>{formatAge(i.created)}</td>
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
                                        );
                                    })}
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
                                        <th className="docker-col-check">
                                            <input
                                                type="checkbox"
                                                checked={filteredVolumes.length > 0 && filteredVolumes.every((v) => selectedVolumes.has(v.name))}
                                                onChange={(e) => setSelectedVolumes(e.target.checked ? new Set(filteredVolumes.map((v) => v.name)) : new Set())}
                                            />
                                        </th>
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
                                            <td className="docker-col-check">
                                                <input
                                                    type="checkbox"
                                                    checked={selectedVolumes.has(v.name)}
                                                    onChange={() => setSelectedVolumes((prev) => toggleSet(prev, v.name))}
                                                />
                                            </td>
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
                                            <td title={formatTimestamp(v.createdAt)}>{formatAge(v.createdAt)}</td>
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
                                    <th className="docker-col-check">
                                        <input
                                            type="checkbox"
                                            checked={selectableNetworks.length > 0 && selectableNetworks.every((n) => selectedNetworks.has(n.id))}
                                            onChange={(e) => setSelectedNetworks(e.target.checked ? new Set(selectableNetworks.map((n) => n.id)) : new Set())}
                                        />
                                    </th>
                                    <th>Name</th>
                                    <th>Driver</th>
                                    <th>Scope</th>
                                    <th className="docker-col-num">Containers</th>
                                    <th>Owner</th>
                                    <th>Created</th>
                                    <th className="docker-col-actions"/>
                                </tr>
                            </thead>
                            <tbody>
                                {filteredNetworks.map((n) => {
                                    const protectedNetwork = n.name === 'bridge' || n.name === 'host' || n.name === 'none';
                                    return (
                                        <tr key={n.id}>
                                            <td className="docker-col-check">
                                                <input
                                                    type="checkbox"
                                                    disabled={protectedNetwork}
                                                    checked={selectedNetworks.has(n.id)}
                                                    onChange={() => setSelectedNetworks((prev) => toggleSet(prev, n.id))}
                                                />
                                            </td>
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
                                            <td title={formatTimestamp(n.created)}>{formatAge(n.created)}</td>
                                            <td className="docker-col-actions">
                                                <button
                                                    className="btn btn-ghost docker-icon-btn docker-icon-btn--danger"
                                                    title="Remove"
                                                    disabled={busy || protectedNetwork}
                                                    onClick={() => handleRemoveNetwork(n)}
                                                >
                                                    <Trash2 size={14}/>
                                                </button>
                                            </td>
                                        </tr>
                                    );
                                })}
                            </tbody>
                        </table>
                    </div>
                )}
            </div>

            {confirmState && (
                <Dialog
                    title="Confirm"
                    onClose={() => setConfirmState(null)}
                    footer={
                        <>
                            <button className="btn btn-ghost" onClick={() => setConfirmState(null)}>Cancel</button>
                            <button className="btn btn-danger" onClick={confirmState.onConfirm}>Remove</button>
                        </>
                    }
                >
                    <p>{confirmState.message}</p>
                    {confirmState.detail && <p className="docker-confirm-detail">{confirmState.detail}</p>}
                </Dialog>
            )}
        </div>
    );
}
