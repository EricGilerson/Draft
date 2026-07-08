import {
    AlertTriangle,
    Copy,
    Database,
    ExternalLink,
    HardDrive,
    RefreshCw,
    Search,
    Trash2,
} from 'lucide-react';
import {useCallback, useEffect, useMemo, useState} from 'react';
import {DeleteManagedVolume, ListVolumesOverview} from '../../wailsjs/go/main/App';
import {deploy} from '../../wailsjs/go/models';
import PageHeader from '../components/PageHeader';
import {formatBytes} from '../components/VolumeEditor';
import './VolumesView.css';

type VolumesViewProps = {
    /** Navigate to the owning project's canvas and focus this volume's node.
     * Only offered for non-orphaned volumes (an orphan has no service to reveal). */
    onRevealVolume?: (projectId: number, nodeId: string, target: string) => void;
};

type StatusKind = 'in-use' | 'idle' | 'orphan';
type StatusFilter = 'all' | StatusKind;
type SortKey = 'size' | 'name' | 'created';

function volumeStatus(v: deploy.VolumeOverview): StatusKind {
    if (v.orphaned) return 'orphan';
    if (v.refCount > 0) return 'in-use';
    return 'idle';
}

// inferFormat guesses what a volume holds from its container mount path. It's a
// display-only hint — Draft doesn't open the volume — so a generic fallback is
// fine when nothing matches.
function inferFormat(target: string): string {
    const t = (target || '').toLowerCase();
    if (t.includes('postgres')) return 'PostgreSQL';
    if (t.includes('mysql')) return 'MySQL';
    if (t.includes('mongo')) return 'MongoDB';
    if (t.includes('redis')) return 'Redis';
    if (t.includes('elastic') || t.includes('opensearch')) return 'Elasticsearch';
    if (t.includes('rabbitmq')) return 'RabbitMQ';
    if (t.includes('clickhouse')) return 'ClickHouse';
    if (t.includes('minio') || t.includes('/data/s3')) return 'Object store';
    return 'Data';
}

function projectLabel(v: deploy.VolumeOverview): string {
    const name = (v.labels?.['draft.projectName'] || '').trim();
    if (name) return name;
    return v.projectId ? `Project ${v.projectId}` : 'Unknown project';
}

function formatAge(createdAt: string): string {
    if (!createdAt) return '—';
    const then = new Date(createdAt).getTime();
    if (!Number.isFinite(then)) return '—';
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

export default function VolumesView({onRevealVolume}: VolumesViewProps) {
    const [volumes, setVolumes] = useState<deploy.VolumeOverview[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState('');
    const [search, setSearch] = useState('');
    const [projectFilter, setProjectFilter] = useState<string>('all');
    const [statusFilter, setStatusFilter] = useState<StatusFilter>('all');
    const [sortKey, setSortKey] = useState<SortKey>('size');
    const [selected, setSelected] = useState<Set<string>>(new Set());

    const refresh = useCallback(() => {
        setLoading(true);
        setError('');
        ListVolumesOverview()
            .then((list) => setVolumes(list ?? []))
            .catch((e) => setError(typeof e === 'string' ? e : e?.message || 'Failed to load volumes'))
            .finally(() => setLoading(false));
    }, []);

    useEffect(() => {
        refresh();
    }, [refresh]);

    // Drop selections that no longer exist after a refresh.
    useEffect(() => {
        setSelected((prev) => {
            const names = new Set(volumes.map((v) => v.name));
            const next = new Set([...prev].filter((n) => names.has(n)));
            return next.size === prev.size ? prev : next;
        });
    }, [volumes]);

    const stats = useMemo(() => {
        let total = 0;
        let reclaimable = 0;
        let orphans = 0;
        for (const v of volumes) {
            total += v.size || 0;
            const status = volumeStatus(v);
            if (status === 'orphan') orphans++;
            if (status === 'orphan' || status === 'idle') reclaimable += v.size || 0;
        }
        return {count: volumes.length, total, reclaimable, orphans};
    }, [volumes]);

    const projectOptions = useMemo(() => {
        const seen = new Map<string, string>();
        for (const v of volumes) {
            const key = String(v.projectId || 0);
            if (!seen.has(key)) seen.set(key, projectLabel(v));
        }
        return [...seen.entries()].sort((a, b) => a[1].localeCompare(b[1]));
    }, [volumes]);

    const filtered = useMemo(() => {
        const q = search.trim().toLowerCase();
        const rows = volumes.filter((v) => {
            if (projectFilter !== 'all' && String(v.projectId || 0) !== projectFilter) return false;
            if (statusFilter !== 'all' && volumeStatus(v) !== statusFilter) return false;
            if (q) {
                const hay = `${v.name} ${v.target} ${v.nodeLabel} ${projectLabel(v)}`.toLowerCase();
                if (!hay.includes(q)) return false;
            }
            return true;
        });
        rows.sort((a, b) => {
            if (sortKey === 'name') return a.name.localeCompare(b.name);
            if (sortKey === 'created') {
                return new Date(b.createdAt).getTime() - new Date(a.createdAt).getTime();
            }
            return (b.size || 0) - (a.size || 0);
        });
        return rows;
    }, [volumes, search, projectFilter, statusFilter, sortKey]);

    const toggleSelect = (name: string) => {
        setSelected((prev) => {
            const next = new Set(prev);
            if (next.has(name)) next.delete(name);
            else next.add(name);
            return next;
        });
    };

    const allVisibleSelected = filtered.length > 0 && filtered.every((v) => selected.has(v.name));
    const toggleSelectAll = () => {
        setSelected((prev) => {
            if (allVisibleSelected) {
                const next = new Set(prev);
                for (const v of filtered) next.delete(v.name);
                return next;
            }
            const next = new Set(prev);
            for (const v of filtered) next.add(v.name);
            return next;
        });
    };

    const handleDelete = useCallback(async (v: deploy.VolumeOverview) => {
        if (!confirm(`Delete volume "${v.name}"? This permanently removes its data (${formatBytes(v.size)}).`)) {
            return;
        }
        try {
            await DeleteManagedVolume(v.name, false);
        } catch (e) {
            // Most commonly the volume is still attached to a container.
            if (!confirm(`Couldn't delete "${v.name}":\n${String(e)}\n\nForce delete? This stops and removes the container currently using it${v.nodeLabel ? ` (service "${v.nodeLabel}")` : ''}, then deletes the volume and its data.`)) {
                return;
            }
            try {
                await DeleteManagedVolume(v.name, true);
            } catch (e2) {
                alert(String(e2));
                return;
            }
        }
        refresh();
    }, [refresh]);

    const bulkDelete = useCallback(async (targets: deploy.VolumeOverview[], noun: string) => {
        if (targets.length === 0) return;
        const totalBytes = targets.reduce((sum, v) => sum + (v.size || 0), 0);
        if (!confirm(`Delete ${targets.length} ${noun} volume${targets.length > 1 ? 's' : ''} (${formatBytes(totalBytes)} reclaimable)? This permanently removes their data.`)) {
            return;
        }
        const failures: string[] = [];
        for (const v of targets) {
            try {
                // Orphans/idle aren't attached; force clears the rare in-use case too.
                await DeleteManagedVolume(v.name, v.orphaned);
            } catch (e) {
                failures.push(`${v.name}: ${String(e)}`);
            }
        }
        refresh();
        if (failures.length) {
            alert(`Some volumes could not be deleted:\n\n${failures.join('\n')}`);
        }
    }, [refresh]);

    const orphanRows = useMemo(() => volumes.filter((v) => v.orphaned), [volumes]);
    const selectedRows = useMemo(() => volumes.filter((v) => selected.has(v.name)), [volumes, selected]);

    const copyName = (name: string) => {
        navigator.clipboard?.writeText(name).catch(() => {});
    };

    return (
        <div className="volumes-view">
            <PageHeader
                title="Volumes"
                description="Draft-managed named volumes across every project. Reclaim stale data, track disk usage, and jump to the service a volume belongs to."
                action={
                    <button className="btn btn-ghost" onClick={refresh} disabled={loading} title="Refresh">
                        <RefreshCw size={15} className={loading ? 'volumes-spin' : ''}/> Refresh
                    </button>
                }
            />

            <div className="volumes-layout">
                {error && <p className="form-error">{error}</p>}

                <div className="volumes-stats">
                    <div className="volumes-stat">
                        <span className="volumes-stat-value">{stats.count}</span>
                        <span className="volumes-stat-label">Managed volumes</span>
                    </div>
                    <div className="volumes-stat">
                        <span className="volumes-stat-value">{formatBytes(stats.total)}</span>
                        <span className="volumes-stat-label">Total size</span>
                    </div>
                    <div className="volumes-stat">
                        <span className="volumes-stat-value">{formatBytes(stats.reclaimable)}</span>
                        <span className="volumes-stat-label">Reclaimable (idle + orphaned)</span>
                    </div>
                    <div className={'volumes-stat' + (stats.orphans > 0 ? ' volumes-stat--warn' : '')}>
                        <span className="volumes-stat-value">{stats.orphans}</span>
                        <span className="volumes-stat-label">Orphaned</span>
                    </div>
                </div>

                <div className="volumes-toolbar">
                    <div className="volumes-search">
                        <Search size={14}/>
                        <input
                            className="input"
                            placeholder="Search name, path, or service…"
                            value={search}
                            onChange={(e) => setSearch(e.target.value)}
                        />
                    </div>
                    <select className="input select-styled volumes-select" value={projectFilter} onChange={(e) => setProjectFilter(e.target.value)}>
                        <option value="all">All projects</option>
                        {projectOptions.map(([id, label]) => (
                            <option key={id} value={id}>{label}</option>
                        ))}
                    </select>
                    <select className="input select-styled volumes-select" value={statusFilter} onChange={(e) => setStatusFilter(e.target.value as StatusFilter)}>
                        <option value="all">All statuses</option>
                        <option value="in-use">In use</option>
                        <option value="idle">Idle</option>
                        <option value="orphan">Orphaned</option>
                    </select>
                    <select className="input select-styled volumes-select" value={sortKey} onChange={(e) => setSortKey(e.target.value as SortKey)}>
                        <option value="size">Largest first</option>
                        <option value="name">Name</option>
                        <option value="created">Newest first</option>
                    </select>
                </div>

                <div className="volumes-actions">
                    <button
                        className="btn btn-ghost"
                        onClick={() => bulkDelete(orphanRows, 'orphaned')}
                        disabled={orphanRows.length === 0}
                        title="Delete every orphaned volume (owning service was removed)"
                    >
                        <Trash2 size={14}/> Prune orphans{orphanRows.length ? ` (${orphanRows.length})` : ''}
                    </button>
                    <button
                        className="btn btn-ghost"
                        onClick={() => bulkDelete(selectedRows, 'selected')}
                        disabled={selectedRows.length === 0}
                        title="Delete the selected volumes"
                    >
                        <Trash2 size={14}/> Delete selected{selectedRows.length ? ` (${selectedRows.length})` : ''}
                    </button>
                </div>

                {loading ? (
                    <div className="volumes-empty">Loading volumes…</div>
                ) : volumes.length === 0 ? (
                    <div className="volumes-empty">
                        <HardDrive size={20}/>
                        <div className="volumes-empty-copy">
                            <strong>No managed volumes yet.</strong>
                            <span>Deploy a service with a named volume (e.g. a database template) and it'll appear here.</span>
                        </div>
                    </div>
                ) : filtered.length === 0 ? (
                    <div className="volumes-empty">
                        <Search size={20}/>
                        <div className="volumes-empty-copy">
                            <strong>No volumes match your filters.</strong>
                            <span>Try clearing the search or status filter.</span>
                        </div>
                    </div>
                ) : (
                    <div className="volumes-table-wrap">
                        <table className="volumes-table">
                            <thead>
                                <tr>
                                    <th className="volumes-col-check">
                                        <input type="checkbox" checked={allVisibleSelected} onChange={toggleSelectAll} aria-label="Select all"/>
                                    </th>
                                    <th>Volume</th>
                                    <th>Linked to</th>
                                    <th>Mount path</th>
                                    <th className="volumes-col-num">Size</th>
                                    <th>Status</th>
                                    <th>Created</th>
                                    <th className="volumes-col-actions"/>
                                </tr>
                            </thead>
                            <tbody>
                                {filtered.map((v) => {
                                    const status = volumeStatus(v);
                                    const isOrphan = status === 'orphan';
                                    const canReveal = !isOrphan && !!v.nodeId && !!onRevealVolume;
                                    return (
                                        <tr key={v.name} className={selected.has(v.name) ? 'volumes-row--selected' : ''}>
                                            <td className="volumes-col-check">
                                                <input
                                                    type="checkbox"
                                                    checked={selected.has(v.name)}
                                                    onChange={() => toggleSelect(v.name)}
                                                    aria-label={`Select ${v.name}`}
                                                />
                                            </td>
                                            <td>
                                                <div className="volumes-name-cell">
                                                    <span className="volumes-format-icon" title={inferFormat(v.target)}>
                                                        <Database size={15}/>
                                                    </span>
                                                    <div className="volumes-name-text">
                                                        <span className="volumes-name" title={v.name}>{v.name}</span>
                                                        <span className="volumes-format">{inferFormat(v.target)} · {v.driver || 'local'}</span>
                                                    </div>
                                                </div>
                                            </td>
                                            <td>
                                                {isOrphan ? (
                                                    <span className="volumes-linked volumes-linked--orphan">
                                                        <AlertTriangle size={12}/> Orphaned
                                                    </span>
                                                ) : (
                                                    <div className="volumes-linked">
                                                        <span className="volumes-linked-service">{v.nodeLabel || v.nodeId || '—'}</span>
                                                        <span className="volumes-linked-project">{projectLabel(v)}</span>
                                                    </div>
                                                )}
                                            </td>
                                            <td><code className="volumes-path">{v.target || '—'}</code></td>
                                            <td className="volumes-col-num">{formatBytes(v.size)}</td>
                                            <td>
                                                <span className={`volumes-status volumes-status--${status}`}>
                                                    {status === 'in-use' ? `In use${v.refCount ? ` · ${v.refCount}` : ''}` : status === 'orphan' ? 'Orphan' : 'Idle'}
                                                </span>
                                            </td>
                                            <td className="volumes-age">{formatAge(v.createdAt)}</td>
                                            <td className="volumes-col-actions">
                                                <div className="volumes-row-actions">
                                                    {canReveal && (
                                                        <button
                                                            className="btn btn-ghost volumes-icon-btn"
                                                            onClick={() => onRevealVolume!(v.projectId, v.nodeId, v.target)}
                                                            title="Reveal on canvas"
                                                        >
                                                            <ExternalLink size={14}/>
                                                        </button>
                                                    )}
                                                    <button
                                                        className="btn btn-ghost volumes-icon-btn"
                                                        onClick={() => copyName(v.name)}
                                                        title="Copy volume name"
                                                    >
                                                        <Copy size={14}/>
                                                    </button>
                                                    <button
                                                        className="btn btn-ghost volumes-icon-btn volumes-icon-btn--danger"
                                                        onClick={() => handleDelete(v)}
                                                        title="Delete volume"
                                                    >
                                                        <Trash2 size={14}/>
                                                    </button>
                                                </div>
                                            </td>
                                        </tr>
                                    );
                                })}
                            </tbody>
                        </table>
                    </div>
                )}
            </div>
        </div>
    );
}
