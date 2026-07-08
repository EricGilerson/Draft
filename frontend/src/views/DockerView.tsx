import {
    Box,
    ChevronDown,
    ChevronRight,
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

type ImageGroup = {
    head: deploy.ImageSummary;
    /** Ancestor chain (parent → grandparent → …), collapsed under the head by default. */
    intermediates: deploy.ImageSummary[];
};

/** Draft N-1 rollback retention tag — kept on disk for rollback, hidden in the Docker tab. */
function isPreviousImageTag(tag: string): boolean {
    return /:\d+-previous$/.test(tag);
}

function visibleRepoTags(tags: string[] | undefined | null): string[] {
    return (tags ?? [])
        .filter((tag) => tag && tag !== '<none>:<none>' && !isPreviousImageTag(tag))
        // Stable order so multi-env tags (main/staging/…) are easy to scan.
        .slice()
        .sort((a, b) => a.localeCompare(b));
}

/** Images that only exist as …:N-previous rollback candidates — omit from the tab. */
function isRollbackOnlyImage(img: deploy.ImageSummary): boolean {
    const raw = (img.repoTags ?? []).filter((tag) => tag && tag !== '<none>:<none>');
    return raw.length > 0 && raw.every(isPreviousImageTag);
}

function imageKey(id: string | undefined | null): string {
    return (id || '').replace(/^sha256:/, '');
}

/** Exclusive layer size: Size − SharedSize. SharedSize −1 means "not computed". */
function imageUniqueSize(img: {size?: number; sharedSize?: number}): number {
    const size = img.size || 0;
    const shared = img.sharedSize;
    if (shared == null || shared < 0) return size;
    return Math.max(0, size - shared);
}

function formatSharedBytes(sharedSize: number | undefined): string {
    if (sharedSize == null || sharedSize < 0) return '—';
    if (sharedSize === 0) return '0 B';
    return formatBytes(sharedSize);
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

/**
 * Group All:true image list into head rows with collapsed parent chains.
 * Heads are leaves in the ParentID graph (nothing points at them as parent) —
 * tagged builds, pulled images, and true untagged orphans. Intermediate
 * parents from multi-stage / iterative builds nest under the head that
 * descends from them so the table doesn't look like N independent multi-GB
 * "dangling" images.
 */
function groupImages(images: deploy.ImageSummary[]): ImageGroup[] {
    const byKey = new Map<string, deploy.ImageSummary>();
    for (const img of images) {
        byKey.set(imageKey(img.id), img);
    }

    const childCount = new Map<string, number>();
    for (const img of images) {
        const parent = imageKey(img.parentId);
        if (!parent) continue;
        childCount.set(parent, (childCount.get(parent) || 0) + 1);
    }

    const heads = images
        .filter((img) => (childCount.get(imageKey(img.id)) || 0) === 0)
        // Newest heads claim shared ancestors first (typical: latest build tag).
        .sort((a, b) => timeValue(b.created) - timeValue(a.created));
    const claimed = new Set<string>();
    const groups: ImageGroup[] = [];

    for (const head of heads) {
        const intermediates: deploy.ImageSummary[] = [];
        const seen = new Set<string>([imageKey(head.id)]);
        let parent = imageKey(head.parentId);
        while (parent && byKey.has(parent) && !seen.has(parent)) {
            seen.add(parent);
            // Exclusive nest: each intermediate appears under one head so bulk
            // select / counts stay unambiguous. Newest head wins (above sort).
            if (!claimed.has(parent)) {
                intermediates.push(byKey.get(parent)!);
                claimed.add(parent);
            }
            parent = imageKey(byKey.get(parent)!.parentId);
        }
        groups.push({head, intermediates});
    }

    // Any leftover images (cycles / orphans not reachable from a head) surface
    // as their own head so nothing disappears from the verbose list.
    for (const img of images) {
        const key = imageKey(img.id);
        if (claimed.has(key)) continue;
        if (heads.some((h) => imageKey(h.id) === key)) continue;
        groups.push({head: img, intermediates: []});
        claimed.add(key);
    }

    return groups;
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
    /** Head image ids whose intermediate parent chain is expanded. */
    const [expandedImageGroups, setExpandedImageGroups] = useState<Set<string>>(new Set());
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
        // LayersSize is the real on-disk image total (shared layers counted once).
        // Summing per-image Size double-counts shared layers and intermediate parents.
        const imagesTotal = typeof du.LayersSize === 'number' && du.LayersSize > 0
            ? du.LayersSize
            : imgs.reduce((s, i) => s + imageUniqueSize({size: i.Size, sharedSize: i.SharedSize}), 0);
        // Reclaimable ≈ unique layers of images not used by any container (docker system df).
        const imagesReclaimable = imgs.reduce((s, i) => {
            if ((i.Containers || 0) > 0) return s;
            return s + imageUniqueSize({size: i.Size, sharedSize: i.SharedSize});
        }, 0);
        const volumesTotal = vols.reduce((s, v) => s + (v.UsageData?.Size || 0), 0);
        const volumesReclaimable = vols.reduce((s, v) => s + ((v.UsageData?.RefCount || 0) === 0 ? v.UsageData?.Size || 0 : 0), 0);
        const buildCacheTotal = cache.reduce((s, c) => s + (c.Size || 0), 0);
        const buildCacheReclaimable = cache.reduce((s, c) => s + (!c.InUse ? c.Size || 0 : 0), 0);
        // Only the writable layer is container-owned; SizeRootFs includes the image.
        const containersTotal = containers.reduce((s, c) => s + (c.sizeRw || 0), 0);
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
        const unique = imageUniqueSize(i);
        const shared = i.sharedSize;
        if (unique > 0 && shared != null && shared > 0) {
            details.push(
                `Up to ${formatBytes(unique)} exclusive layers may be freed; ${formatBytes(shared)} is shared with other images and stays until those are removed too.`,
            );
        } else if (unique > 0) {
            details.push(`About ${formatBytes(unique)} may be freed if no other image needs these layers.`);
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
    const imageGroups = useMemo(() => {
        // Hide N-1 rollback retention images (…:N-previous) entirely — they stay
        // on disk for Deployments → Redeploy, but clutter the Docker tab.
        const listed = images.filter((i) => !isRollbackOnlyImage(i));
        const groups = groupImages(listed);
        const matchesQuery = (i: deploy.ImageSummary) =>
            !q || `${visibleRepoTags(i.repoTags).join(' ')} ${i.id} ${i.parentId || ''}`.toLowerCase().includes(q);

        // Keep a group if the head or any intermediate matches (search still
        // reaches collapsed parents).
        const filtered = groups.filter(
            (g) => matchesQuery(g.head) || g.intermediates.some(matchesQuery),
        );

        filtered.sort((a, b) => {
            if (imageSort === 'name') {
                const aLabel = visibleRepoTags(a.head.repoTags)[0] || a.head.id;
                const bLabel = visibleRepoTags(b.head.repoTags)[0] || b.head.id;
                return aLabel.localeCompare(bLabel);
            }
            if (imageSort === 'size') return imageUniqueSize(b.head) - imageUniqueSize(a.head);
            return timeValue(b.head.created) - timeValue(a.head.created);
        });
        return filtered;
    }, [images, q, imageSort]);

    /** Flat list of currently visible image rows (heads + expanded intermediates). */
    const visibleImageRows = useMemo(() => {
        const rows: deploy.ImageSummary[] = [];
        for (const g of imageGroups) {
            rows.push(g.head);
            if (expandedImageGroups.has(g.head.id) || (q && g.intermediates.some((i) =>
                `${i.repoTags?.join(' ')} ${i.id}`.toLowerCase().includes(q),
            ))) {
                rows.push(...g.intermediates);
            }
        }
        return rows;
    }, [imageGroups, expandedImageGroups, q]);

    const intermediateCount = useMemo(
        () => imageGroups.reduce((n, g) => n + g.intermediates.length, 0),
        [imageGroups],
    );
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

    const headImageCount = useMemo(() => groupImages(images).length, [images]);
    const tabs: {id: ResourceTab; label: string; icon: typeof Box; count: number}[] = [
        {id: 'containers', label: 'Containers', icon: ContainerIcon, count: containers.length},
        {id: 'images', label: 'Images', icon: Layers, count: headImageCount},
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
                                <option value="size">Largest unique size</option>
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
                            onClick={() => handleBulkRemoveImages(images.filter((i) => selectedImages.has(i.id)))}
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
                                        <th className="docker-col-num" title="Writable container layer only">Writable</th>
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
                                            <td className="docker-col-num" title="Writable layer only (image layers counted under Images)">
                                                {formatBytes(c.sizeRw || 0)}
                                            </td>
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
                    imageGroups.length === 0 ? (
                        <div className="docker-empty">No images found.</div>
                    ) : (
                        <>
                            {intermediateCount > 0 && (
                                <p className="docker-image-hint">
                                    {intermediateCount} intermediate parent image{intermediateCount === 1 ? '' : 's'} nested under builds
                                    (expand a row to inspect or delete). Unique is exclusive layers; Shared is already counted on other images. Disk total above uses Docker&apos;s layer store (no double-counting).
                                </p>
                            )}
                            <div className="docker-table-wrap">
                                <table className="docker-table">
                                    <thead>
                                        <tr>
                                            <th className="docker-col-check">
                                                <input
                                                    type="checkbox"
                                                    checked={visibleImageRows.length > 0 && visibleImageRows.every((i) => selectedImages.has(i.id))}
                                                    onChange={(e) => setSelectedImages(e.target.checked ? new Set(visibleImageRows.map((i) => i.id)) : new Set())}
                                                />
                                            </th>
                                            <th>Repository:Tag</th>
                                            <th>Image ID</th>
                                            <th className="docker-col-num" title="Layers unique to this image (Size − Shared). Not the full virtual size.">Unique</th>
                                            <th className="docker-col-num" title="Layers also used by at least one other image. Not freed until those images are removed.">Shared</th>
                                            <th className="docker-col-num" title="Full virtual size of this image (all layers). Summing this column overcounts disk.">Virtual</th>
                                            <th className="docker-col-num">In use</th>
                                            <th>Created</th>
                                            <th>Owner</th>
                                            <th className="docker-col-actions"/>
                                        </tr>
                                    </thead>
                                    <tbody>
                                        {imageGroups.flatMap((group) => {
                                            const head = group.head;
                                            const expanded = expandedImageGroups.has(head.id)
                                                || (!!q && group.intermediates.some((i) =>
                                                    `${i.repoTags?.join(' ')} ${i.id}`.toLowerCase().includes(q),
                                                ));
                                            const rows: {img: deploy.ImageSummary; depth: number; isHead: boolean}[] = [
                                                {img: head, depth: 0, isHead: true},
                                            ];
                                            if (expanded) {
                                                for (const inter of group.intermediates) {
                                                    rows.push({img: inter, depth: 1, isHead: false});
                                                }
                                            }
                                            return rows.map(({img, depth, isHead}) => {
                                                const tags = visibleRepoTags(img.repoTags);
                                                const unique = imageUniqueSize(img);
                                                return (
                                                    <tr
                                                        key={img.id}
                                                        className={depth > 0 ? 'docker-image-row--nested' : undefined}
                                                    >
                                                        <td className="docker-col-check">
                                                            <input
                                                                type="checkbox"
                                                                checked={selectedImages.has(img.id)}
                                                                onChange={() => setSelectedImages((prev) => toggleSet(prev, img.id))}
                                                            />
                                                        </td>
                                                        <td>
                                                            <div
                                                                className="docker-image-label"
                                                                style={depth > 0 ? {paddingLeft: 12} : undefined}
                                                            >
                                                                {isHead && group.intermediates.length > 0 ? (
                                                                    <button
                                                                        type="button"
                                                                        className="docker-expand-btn"
                                                                        title={expanded
                                                                            ? `Hide ${group.intermediates.length} intermediate parent${group.intermediates.length === 1 ? '' : 's'}`
                                                                            : `Show ${group.intermediates.length} intermediate parent${group.intermediates.length === 1 ? '' : 's'}`}
                                                                        onClick={() => setExpandedImageGroups((prev) => toggleSet(prev, head.id))}
                                                                    >
                                                                        {expanded ? <ChevronDown size={14}/> : <ChevronRight size={14}/>}
                                                                        <span className="docker-expand-count">{group.intermediates.length}</span>
                                                                    </button>
                                                                ) : (
                                                                    <span className="docker-expand-spacer" aria-hidden/>
                                                                )}
                                                                {tags.length > 0 ? (
                                                                    <div
                                                                        className={`docker-image-tags${tags.length > 1 ? ' docker-image-tags--multi' : ''}`}
                                                                        title={tags.join('\n')}
                                                                    >
                                                                        {tags.length > 1 && (
                                                                            <span className="docker-tag-count">
                                                                                {tags.length} tags
                                                                            </span>
                                                                        )}
                                                                        <ul className="docker-image-tag-list">
                                                                            {tags.map((tag) => (
                                                                                <li key={tag} className="docker-image-tag">
                                                                                    <code>{tag}</code>
                                                                                </li>
                                                                            ))}
                                                                        </ul>
                                                                    </div>
                                                                ) : depth > 0 ? (
                                                                    <span
                                                                        className="docker-badge docker-badge--intermediate"
                                                                        title="Untagged parent from a prior build layer — not a separate full image on disk"
                                                                    >
                                                                        intermediate
                                                                    </span>
                                                                ) : (
                                                                    <span
                                                                        className="docker-badge docker-badge--external"
                                                                        title="Untagged image with no children (true dangling)"
                                                                    >
                                                                        untagged
                                                                    </span>
                                                                )}
                                                            </div>
                                                        </td>
                                                        <td><code className="docker-mono">{shortId(img.id)}</code></td>
                                                        <td className="docker-col-num" title={img.size ? `Virtual ${formatBytes(img.size)}` : undefined}>
                                                            {formatBytes(unique)}
                                                        </td>
                                                        <td className="docker-col-num">{formatSharedBytes(img.sharedSize)}</td>
                                                        <td className="docker-col-num docker-col-muted">{formatBytes(img.size)}</td>
                                                        <td className="docker-col-num">{img.containers < 0 ? '—' : img.containers}</td>
                                                        <td title={formatTimestamp(img.created)}>{formatAge(img.created)}</td>
                                                        <td>
                                                            {img.managed ? (
                                                                <span className="docker-badge docker-badge--managed">Draft build</span>
                                                            ) : depth > 0 ? (
                                                                <span className="docker-badge docker-badge--intermediate">Parent</span>
                                                            ) : (
                                                                <span className="docker-badge docker-badge--external">External</span>
                                                            )}
                                                        </td>
                                                        <td className="docker-col-actions">
                                                            <button className="btn btn-ghost docker-icon-btn docker-icon-btn--danger" title="Remove" disabled={busy} onClick={() => handleRemoveImage(img)}>
                                                                <Trash2 size={14}/>
                                                            </button>
                                                        </td>
                                                    </tr>
                                                );
                                            });
                                        })}
                                    </tbody>
                                </table>
                            </div>
                        </>
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
