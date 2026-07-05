import {HardDrive, FolderOpen, Plus, Trash2, AlertTriangle} from 'lucide-react';
import {deploy} from '../../wailsjs/go/models';

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

function formatBytes(n: number): string {
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
};

export default function VolumeEditor({
    entries,
    onChange,
    editable = true,
    allowAdd = true,
    managedByTarget,
    onDeleteVolume,
    compact = false,
}: Props) {
    const update = (i: number, patch: Partial<VolumeEntry>) => {
        const next = entries.map((e, j) => (j === i ? {...e, ...patch} : e));
        onChange(next);
    };

    const remove = (i: number) => {
        onChange(entries.filter((_, j) => j !== i));
    };

    return (
        <div className="volume-editor">
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
                                    onClick={() => update(i, {type: 'volume', source: isVolume ? entry.source : ''})}
                                    disabled={!editable}
                                    title="Docker-managed named volume — no host path needed, persists across redeploys."
                                >
                                    <HardDrive size={12} style={{verticalAlign: '-2px', marginRight: 4}}/>
                                    Named volume
                                </button>
                                <button
                                    type="button"
                                    className={`trigger-seg-btn ${!isVolume ? 'trigger-seg-btn--active' : ''}`}
                                    onClick={() => update(i, {type: 'bind', source: entry.hostPath || entry.source || ''})}
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
                                    onChange={(e) => update(i, {readOnly: e.target.checked})}
                                    disabled={!editable}
                                />
                                <span className="settings-kv-check-label">RO</span>
                            </label>
                            {editable && (
                                <button
                                    className="btn btn-ghost settings-kv-remove"
                                    onClick={() => remove(i)}
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
                                    onChange={(e) => update(i, {containerPath: e.target.value})}
                                    placeholder="e.g. /var/lib/postgresql/data"
                                    disabled={!editable}
                                />
                            </label>
                            {isVolume ? (
                                <label className="csd-field volume-field">
                                    <span className="csd-field-label">
                                        Volume name <span className="csd-optional">(auto = Draft-managed)</span>
                                    </span>
                                    <input
                                        className="input"
                                        value={entry.source || ''}
                                        onChange={(e) => update(i, {source: e.target.value})}
                                        placeholder={isAuto ? 'Auto — created on first deploy' : 'explicit volume name'}
                                        disabled={!editable}
                                    />
                                    {isAuto && resolvedName && (
                                        <span className="settings-resolved volume-resolved">{resolvedName}</span>
                                    )}
                                    {isAuto && !resolvedName && (
                                        <span className="settings-hint">Draft mints a stable name from this service's identity on first deploy; the same service keeps its data across redeploys.</span>
                                    )}
                                    {(entry.source || '').trim() && (
                                        <span className="settings-hint">An explicit name may be shared across services / environments — those services will read and write the same data.</span>
                                    )}
                                </label>
                            ) : (
                                <label className="csd-field volume-field">
                                    <span className="csd-field-label">Host path</span>
                                    <input
                                        className="input"
                                        value={entry.source || entry.hostPath || ''}
                                        onChange={(e) => update(i, {source: e.target.value, hostPath: ''})}
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
                                        onChange={(e) => update(i, {sizeHint: e.target.value})}
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
