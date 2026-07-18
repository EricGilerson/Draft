import {useCallback, useEffect, useMemo, useRef, useState} from 'react';
import {ChevronDown, HardDrive, FolderOpen, Plus, Trash2, AlertTriangle} from 'lucide-react';
import {DeleteManagedVolume, ListVolumesOverview} from '../../wailsjs/go/main/App';
import {deploy} from '../../wailsjs/go/models';
import Dialog from './Dialog';
import './VolumeEditor.css';

// VolumeEntry is the frontend mirror of deploy.VolumeSpec / store.TemplateVolume.
// The JSON stored in node_settings.volume_mounts (and ServiceTemplate.Volumes)
// uses these field names, so keep them in sync with the Go structs. A missing
// "type" is treated as "bind" for back-compat with pre-volume rows.
export type VolumeEntry = {
    type: 'bind' | 'volume';
    source?: string;          // volume: "" = auto (Draft-managed) name; bind: host path
    hostPath?: string;        // legacy bind host path (back-compat read)
    containerPath: string;
    readOnly?: boolean;
    sizeHint?: string;        // advisory, e.g. "10g" — monitored, NOT enforced by Docker
    labels?: Record<string, string>;
};

export function parseVolumeEntries(raw?: string): VolumeEntry[] {
    if (!raw) return [];
    try {
        const arr = JSON.parse(raw);
        if (!Array.isArray(arr)) return [];
        return arr.map((e: any) => ({
            type: ((e?.type === 'volume' ? 'volume' : 'bind') as 'volume' | 'bind'),
            source: e?.source ?? '',
            hostPath: e?.hostPath ?? '',
            containerPath: String(e?.containerPath ?? ''),
            readOnly: !!e?.readOnly,
            sizeHint: e?.sizeHint ?? '',
            labels: e?.labels && typeof e?.labels === 'object' ? e.labels : undefined,
        })).filter((e) => e.containerPath || e.source || e.hostPath);
    } catch {
        return [];
    }
}

export function serializeVolumeEntries(entries: VolumeEntry[]): string {
    const cleaned = entries
        .map((e) => ({
            type: e.type,
            source: (e.source || '').trim(),
            containerPath: e.containerPath.trim(),
            readOnly: !!e.readOnly,
            sizeHint: (e.sizeHint || '').trim(),
            ...(e.labels && Object.keys(e.labels).length ? {labels: e.labels} : {}),
        }))
        .filter((e) => e.containerPath);
    return JSON.stringify(cleaned);
}

export function formatBytes(n: number): string {
    if (!n || n <= 0) return '—';
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    let i = 0;
    let v = n;
    while (v >= 1024 && i < units.length - 1) {
        v /= 1024;
        i++;
    }
    return `${v.toFixed(v >= 10 || i === 0 ? 0 : 1)} ${units[i]}`;
}

type Props = {
    entries: VolumeEntry[];
    onChange: (entries: VolumeEntry[]) => void;
    /** When false, rows render read-only (e.g. a wizard with Editable=false). */
    editable?: boolean;
    /** Optional "add" control; default true. */
    allowAdd?: boolean;
    /** Resolved Docker volumes for this node (target -> ManagedVolume), so the
     * editor can show the real auto-generated name and live usage for named
     * volumes that have already been deployed. */
    managedByTarget?: Record<string, deploy.ManagedVolume>;
    /** Remove the actual Docker volume (called by the per-row trash action on a
     * named volume that exists in Docker). Optional; only Settings passes it. */
    onDeleteVolume?: (name: string) => Promise<void>;
    /** Compact mode for the wizard (hides the size-hint and labels fields). */
    compact?: boolean;
    /** Show a searchable orphaned-volume picker on named mounts. Off for
     * template defaults (those aren't attaching live Docker volumes). */
    enableOrphanPicker?: boolean;
    /** When set, same-project orphans sort first in the picker. */
    projectId?: number;
    /** Called after a replace-flow permanently deletes a previous Docker volume. */
    onManagedVolumesChanged?: () => void;
};

type PendingOrphanAction =
    | {kind: 'update'; index: number; patch: Partial<VolumeEntry>; orphan: deploy.ManagedVolume}
    | {kind: 'remove'; index: number; orphan: deploy.ManagedVolume};

function projectLabel(v: deploy.VolumeOverview): string {
    const name = (v.labels?.['draft.projectName'] || '').trim();
    if (name) return name;
    return v.projectId ? `Project ${v.projectId}` : 'Unknown project';
}

/** True when applying `next` would stop using the Docker volume currently
 * backing this mount (it becomes reclaimable / "orphaned" in the Volumes UI). */
function volumeAbandonedByChange(
    prev: VolumeEntry,
    next: VolumeEntry,
    managedByTarget?: Record<string, deploy.ManagedVolume>,
): deploy.ManagedVolume | null {
    if (!managedByTarget) return null;
    const managed = managedByTarget[prev.containerPath];
    if (!managed?.name) return null;

    if (next.type !== 'volume') return managed;

    const nextSource = (next.source || '').trim();
    if (!nextSource) {
        // Still auto-named: same container path keeps the same Draft volume.
        if (next.containerPath === prev.containerPath) return null;
        return managed;
    }
    if (nextSource === managed.name) return null;
    return managed;
}

export default function VolumeEditor({
    entries,
    onChange,
    editable = true,
    allowAdd = true,
    managedByTarget,
    onDeleteVolume,
    compact = false,
    enableOrphanPicker = true,
    projectId,
    onManagedVolumesChanged,
}: Props) {
    const [orphans, setOrphans] = useState<deploy.VolumeOverview[]>([]);
    const [orphansLoading, setOrphansLoading] = useState(false);
    const [openPickerIndex, setOpenPickerIndex] = useState<number | null>(null);
    const [pending, setPending] = useState<PendingOrphanAction | null>(null);
    const [pendingBusy, setPendingBusy] = useState(false);
    const [actionError, setActionError] = useState('');

    const refreshOrphans = useCallback(() => {
        if (!enableOrphanPicker || !editable) {
            setOrphans([]);
            return;
        }
        setOrphansLoading(true);
        ListVolumesOverview()
            .then((list) => setOrphans((list ?? []).filter((v) => v.orphaned)))
            .catch(() => setOrphans([]))
            .finally(() => setOrphansLoading(false));
    }, [enableOrphanPicker, editable]);

    useEffect(() => {
        refreshOrphans();
    }, [refreshOrphans]);

    const usedNames = useMemo(() => {
        const names = new Set<string>();
        for (const e of entries) {
            if (e.type !== 'volume') continue;
            const n = (e.source || '').trim();
            if (n) names.add(n);
        }
        return names;
    }, [entries]);

    const applyUpdate = (i: number, patch: Partial<VolumeEntry>) => {
        onChange(entries.map((e, j) => (j === i ? {...e, ...patch} : e)));
    };

    const applyRemove = (i: number) => {
        onChange(entries.filter((_, j) => j !== i));
    };

    const requestUpdate = (i: number, patch: Partial<VolumeEntry>) => {
        const prev = entries[i];
        const next = {...prev, ...patch};
        const orphan = volumeAbandonedByChange(prev, next, managedByTarget);
        if (orphan && editable) {
            setActionError('');
            setPending({kind: 'update', index: i, patch, orphan});
            return;
        }
        applyUpdate(i, patch);
    };

    const requestRemove = (i: number) => {
        const prev = entries[i];
        const managed = prev.type === 'volume' ? managedByTarget?.[prev.containerPath] : undefined;
        if (managed?.name && editable) {
            setActionError('');
            setPending({kind: 'remove', index: i, orphan: managed});
            return;
        }
        applyRemove(i);
    };

    const closePending = () => {
        if (pendingBusy) return;
        setPending(null);
    };

    const commitPending = async (deleteVolume: boolean) => {
        if (!pending) return;
        setPendingBusy(true);
        setActionError('');
        const orphanName = pending.orphan.name;
        try {
            if (pending.kind === 'update') {
                applyUpdate(pending.index, pending.patch);
            } else {
                applyRemove(pending.index);
            }
            if (deleteVolume) {
                try {
                    await DeleteManagedVolume(orphanName, false);
                    onManagedVolumesChanged?.();
                    refreshOrphans();
                } catch (e) {
                    const msg = typeof e === 'string' ? e : (e as Error)?.message || 'Delete failed';
                    setActionError(
                        `Mount updated, but could not delete "${orphanName}": ${msg}. ` +
                        'It may still be attached to a running container — stop the service and delete it from the Volumes tab.',
                    );
                }
            }
            setPending(null);
        } finally {
            setPendingBusy(false);
        }
    };

    return (
        <div className="volume-editor">
            {actionError && <p className="form-error volume-action-error">{actionError}</p>}
            {entries.length === 0 && (
                <span className="settings-hint">No volumes. The container's writable layer is ephemeral — data won't persist across redeploys.</span>
            )}
            {entries.map((entry, i) => {
                const isVolume = entry.type === 'volume';
                const isAuto = isVolume && !(entry.source || '').trim();
                const managed = managedByTarget?.[entry.containerPath];
                const resolvedName = isVolume ? (entry.source || '').trim() || managed?.name || '' : '';
                const usage = managed?.size ?? 0;
                const overHint = entry.sizeHint && usage > 0 && parseBytes(entry.sizeHint) > 0 && usage > parseBytes(entry.sizeHint);
                return (
                    <div key={i} className="volume-row">
                        <div className="volume-row-top">
                            <div className="volume-type-seg" role="group">
                                <button
                                    type="button"
                                    className={`trigger-seg-btn ${isVolume ? 'trigger-seg-btn--active' : ''}`}
                                    onClick={() => requestUpdate(i, {type: 'volume', source: isVolume ? entry.source : ''})}
                                    disabled={!editable}
                                    title="Docker-managed named volume — no host path needed, persists across redeploys."
                                >
                                    <HardDrive size={12} style={{verticalAlign: '-2px', marginRight: 4}}/>
                                    Named volume
                                </button>
                                <button
                                    type="button"
                                    className={`trigger-seg-btn ${!isVolume ? 'trigger-seg-btn--active' : ''}`}
                                    onClick={() => requestUpdate(i, {type: 'bind', source: entry.hostPath || entry.source || ''})}
                                    disabled={!editable}
                                    title="Bind mount a host directory into the container."
                                >
                                    <FolderOpen size={12} style={{verticalAlign: '-2px', marginRight: 4}}/>
                                    Bind mount
                                </button>
                            </div>
                            <label className="settings-kv-check" title="Read-only mount">
                                <input
                                    type="checkbox"
                                    checked={!!entry.readOnly}
                                    onChange={(e) => applyUpdate(i, {readOnly: e.target.checked})}
                                    disabled={!editable}
                                />
                                <span className="settings-kv-check-label">RO</span>
                            </label>
                            {editable && (
                                <button
                                    className="btn btn-ghost settings-kv-remove"
                                    onClick={() => requestRemove(i)}
                                    title="Remove volume"
                                >
                                    <Trash2 size={12}/>
                                </button>
                            )}
                        </div>
                        <div className="volume-row-fields">
                            <label className="csd-field volume-field">
                                <span className="csd-field-label">Container path</span>
                                <input
                                    className="input"
                                    value={entry.containerPath}
                                    onChange={(e) => applyUpdate(i, {containerPath: e.target.value})}
                                    placeholder="e.g. /var/lib/postgresql/data"
                                    disabled={!editable}
                                />
                            </label>
                            {isVolume ? (
                                <div className="csd-field volume-field">
                                    <span className="csd-field-label">
                                        Volume name <span className="csd-optional">(auto = Draft-managed)</span>
                                    </span>
                                    {enableOrphanPicker && editable ? (
                                        <OrphanVolumePicker
                                            value={entry.source || ''}
                                            open={openPickerIndex === i}
                                            onOpenChange={(open) => setOpenPickerIndex(open ? i : null)}
                                            onChange={(name) => requestUpdate(i, {source: name})}
                                            orphans={orphans}
                                            orphansLoading={orphansLoading}
                                            projectId={projectId}
                                            excludeNames={usedNames}
                                            currentManagedName={managed?.name}
                                            placeholder={isAuto ? 'Auto — created on first deploy' : 'explicit volume name'}
                                        />
                                    ) : (
                                        <input
                                            className="input"
                                            value={entry.source || ''}
                                            onChange={(e) => applyUpdate(i, {source: e.target.value})}
                                            placeholder={isAuto ? 'Auto — created on first deploy' : 'explicit volume name'}
                                            disabled={!editable}
                                        />
                                    )}
                                    {isAuto && resolvedName && (
                                        <span className="settings-resolved volume-resolved">{resolvedName}</span>
                                    )}
                                    {isAuto && !resolvedName && (
                                        <span className="settings-hint">Draft mints a stable name from this service's identity on first deploy; the same service keeps its data across redeploys.</span>
                                    )}
                                    {(entry.source || '').trim() && (
                                        <span className="settings-hint">An explicit name may be shared across services / environments — those services will read and write the same data. Pick an orphaned volume from the list to reclaim it.</span>
                                    )}
                                </div>
                            ) : (
                                <label className="csd-field volume-field">
                                    <span className="csd-field-label">Host path</span>
                                    <input
                                        className="input"
                                        value={entry.source || entry.hostPath || ''}
                                        onChange={(e) => applyUpdate(i, {source: e.target.value, hostPath: ''})}
                                        placeholder="/host/path"
                                        disabled={!editable}
                                    />
                                </label>
                            )}
                        </div>
                        {!compact && (
                            <div className="volume-row-meta">
                                <label className="csd-field volume-field volume-field-size">
                                    <span className="csd-field-label">Size hint <span className="csd-optional">(advisory)</span></span>
                                    <input
                                        className="input"
                                        value={entry.sizeHint || ''}
                                        onChange={(e) => applyUpdate(i, {sizeHint: e.target.value})}
                                        placeholder="e.g. 10g"
                                        disabled={!editable}
                                    />
                                    <span className="settings-hint">
                                        Monitored, not enforced — Docker's local driver has no per-volume quota. {usage > 0 ? `In use: ${formatBytes(usage)}.` : ''}
                                        {overHint && <em className="settings-toggle-inactive-note"> Over the hint.</em>}
                                    </span>
                                </label>
                                {managed && onDeleteVolume && (
                                    <button
                                        className="btn btn-ghost volume-delete-docker"
                                        onClick={() => onDeleteVolume(managed.name)}
                                        disabled={!editable}
                                        title={`Delete the Docker volume ${managed.name}`}
                                    >
                                        <Trash2 size={12}/> Delete Docker volume
                                    </button>
                                )}
                            </div>
                        )}
                        {isVolume && managed && (managed.refCount > 0) && (
                            <span className="settings-hint volume-in-use">
                                <AlertTriangle size={11} style={{verticalAlign: '-1px', marginRight: 3}}/>
                                Currently attached to {managed.refCount} container{managed.refCount > 1 ? 's' : ''}.
                            </span>
                        )}
                    </div>
                );
            })}
            {editable && allowAdd && (
                <button
                    className="btn btn-ghost settings-add-btn"
                    onClick={() => onChange([...entries, {type: 'volume', source: '', containerPath: '', readOnly: false}])}
                >
                    <Plus size={12}/> Add volume
                </button>
            )}

            {pending && (
                <Dialog
                    title="Previous volume will be orphaned"
                    onClose={closePending}
                    footer={
                        <>
                            <button className="btn btn-ghost" onClick={closePending} disabled={pendingBusy}>
                                Cancel
                            </button>
                            <button className="btn btn-ghost" onClick={() => commitPending(false)} disabled={pendingBusy}>
                                Keep as orphan
                            </button>
                            <button className="btn btn-danger" onClick={() => commitPending(true)} disabled={pendingBusy}>
                                {pendingBusy ? 'Working…' : 'Delete permanently'}
                            </button>
                        </>
                    }
                >
                    <div className="dialog-copy">
                        <p className="dialog-message">
                            This change stops using{' '}
                            <code className="volume-orphan-name">{pending.orphan.name}</code>
                            {pending.orphan.size > 0 ? ` (${formatBytes(pending.orphan.size)})` : ''}.
                        </p>
                        <p className="dialog-detail">
                            Keep it as an orphan so you can reclaim it later from the Volumes tab, or permanently
                            delete it now. Deleting fails while a container still has it attached — stop the service
                            first if needed.
                        </p>
                        {pending.orphan.refCount > 0 && (
                            <p className="settings-hint volume-in-use">
                                <AlertTriangle size={11} style={{verticalAlign: '-1px', marginRight: 3}}/>
                                Still attached to {pending.orphan.refCount} container
                                {pending.orphan.refCount > 1 ? 's' : ''} — permanent delete will likely fail until
                                you stop or redeploy.
                            </p>
                        )}
                    </div>
                </Dialog>
            )}
        </div>
    );
}

type OrphanVolumePickerProps = {
    value: string;
    open: boolean;
    onOpenChange: (open: boolean) => void;
    onChange: (name: string) => void;
    orphans: deploy.VolumeOverview[];
    orphansLoading: boolean;
    projectId?: number;
    excludeNames: Set<string>;
    currentManagedName?: string;
    placeholder?: string;
};

function OrphanVolumePicker({
    value,
    open,
    onOpenChange,
    onChange,
    orphans,
    orphansLoading,
    projectId,
    excludeNames,
    currentManagedName,
    placeholder,
}: OrphanVolumePickerProps) {
    const rootRef = useRef<HTMLDivElement>(null);
    const [query, setQuery] = useState(value);

    useEffect(() => {
        if (!open) setQuery(value);
    }, [value, open]);

    useEffect(() => {
        if (!open) return;
        const onDoc = (e: MouseEvent) => {
            if (!rootRef.current?.contains(e.target as Node)) {
                onOpenChange(false);
            }
        };
        document.addEventListener('mousedown', onDoc);
        return () => document.removeEventListener('mousedown', onDoc);
    }, [open, onOpenChange]);

    const filtered = useMemo(() => {
        const q = query.trim().toLowerCase();
        const list = orphans.filter((v) => {
            if (!v.name) return false;
            if (currentManagedName && v.name === currentManagedName) return false;
            if (excludeNames.has(v.name) && v.name !== value.trim()) return false;
            if (!q) return true;
            const hay = [
                v.name,
                v.target,
                projectLabel(v),
                v.environment,
                formatBytes(v.size),
            ].join(' ').toLowerCase();
            return hay.includes(q);
        });
        list.sort((a, b) => {
            const aSame = projectId && a.projectId === projectId ? 0 : 1;
            const bSame = projectId && b.projectId === projectId ? 0 : 1;
            if (aSame !== bSame) return aSame - bSame;
            return (b.size || 0) - (a.size || 0) || a.name.localeCompare(b.name);
        });
        return list;
    }, [orphans, query, projectId, excludeNames, currentManagedName, value]);

    const commitTyped = () => {
        onChange(query);
        onOpenChange(false);
    };

    return (
        <div className="volume-orphan-picker" ref={rootRef}>
            <div className="volume-orphan-picker-input">
                <input
                    className="input"
                    value={open ? query : value}
                    onChange={(e) => {
                        setQuery(e.target.value);
                        if (!open) onOpenChange(true);
                    }}
                    onFocus={() => {
                        setQuery(value);
                        onOpenChange(true);
                    }}
                    onKeyDown={(e) => {
                        if (e.key === 'Enter') {
                            e.preventDefault();
                            commitTyped();
                        } else if (e.key === 'Escape') {
                            e.preventDefault();
                            setQuery(value);
                            onOpenChange(false);
                        }
                    }}
                    placeholder={placeholder}
                    aria-expanded={open}
                    aria-autocomplete="list"
                    role="combobox"
                />
                <button
                    type="button"
                    className="volume-orphan-picker-toggle"
                    onClick={() => onOpenChange(!open)}
                    title="Browse orphaned volumes"
                    aria-label="Browse orphaned volumes"
                >
                    <ChevronDown size={14}/>
                </button>
            </div>
            {open && (
                <div className="volume-orphan-menu" role="listbox">
                    <button
                        type="button"
                        className={`volume-orphan-option${!value.trim() ? ' volume-orphan-option--active' : ''}`}
                        onClick={() => {
                            onChange('');
                            onOpenChange(false);
                        }}
                        role="option"
                        aria-selected={!value.trim()}
                    >
                        <span className="volume-orphan-option-main">
                            <span className="volume-orphan-option-name">Auto — Draft-managed</span>
                            <span className="volume-orphan-option-meta">Minted on first deploy</span>
                        </span>
                    </button>
                    {orphansLoading && (
                        <div className="volume-orphan-empty">Loading orphaned volumes…</div>
                    )}
                    {!orphansLoading && filtered.length === 0 && (
                        <div className="volume-orphan-empty">
                            {orphans.length === 0
                                ? 'No orphaned volumes to reclaim.'
                                : 'No orphans match this search.'}
                        </div>
                    )}
                    {!orphansLoading && filtered.map((v) => (
                        <button
                            key={v.name}
                            type="button"
                            className={`volume-orphan-option${value.trim() === v.name ? ' volume-orphan-option--active' : ''}`}
                            onClick={() => {
                                onChange(v.name);
                                onOpenChange(false);
                            }}
                            role="option"
                            aria-selected={value.trim() === v.name}
                            title={v.name}
                        >
                            <span className="volume-orphan-option-main">
                                <span className="volume-orphan-option-name">{v.name}</span>
                                <span className="volume-orphan-option-meta">
                                    {v.target || '—'}
                                    {' · '}
                                    {projectLabel(v)}
                                    {v.environment ? ` / ${v.environment}` : ''}
                                </span>
                            </span>
                            <span className="volume-orphan-option-size">{formatBytes(v.size)}</span>
                        </button>
                    ))}
                    {query.trim() && query.trim() !== value.trim() && (
                        <button
                            type="button"
                            className="volume-orphan-option volume-orphan-option--custom"
                            onClick={commitTyped}
                            role="option"
                        >
                            <span className="volume-orphan-option-main">
                                <span className="volume-orphan-option-name">Use “{query.trim()}”</span>
                                <span className="volume-orphan-option-meta">Explicit volume name</span>
                            </span>
                        </button>
                    )}
                </div>
            )}
        </div>
    );
}

function parseBytes(spec: string): number {
    const m = /^(\d+(?:\.\d+)?)\s*([kmgtp]?b?|b)?$/i.exec(spec.trim());
    if (!m) return 0;
    const n = parseFloat(m[1]);
    const unit = (m[2] || '').toLowerCase();
    const mult: Record<string, number> = {
        '': 1, b: 1,
        k: 1024, kb: 1024,
        m: 1024 ** 2, mb: 1024 ** 2,
        g: 1024 ** 3, gb: 1024 ** 3,
        t: 1024 ** 4, tb: 1024 ** 4,
    };
    return n * (mult[unit] || 1);
}
