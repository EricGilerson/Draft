import {useEffect, useMemo, useState} from 'react';
import Dialog from './Dialog';
import {formatSettingValue} from '../lib/settingStaging';
import './DiscardChangesDialog.css';

export type DiscardChangeItem = {
    /** Stable id for selection (e.g. "setting:service_port", "env:FOO"). */
    id: string;
    kind: 'setting' | 'env';
    /** setting key or env key */
    key: string;
    /** Human label shown as primary text */
    label: string;
    /** Optional detail under the label (before → after, delete, etc.) */
    detail?: string;
    /** Optional secondary badge */
    badge?: string;
};

type DiscardChangesDialogProps = {
    title: string;
    description: string;
    items: DiscardChangeItem[];
    confirmLabel?: string;
    onClose: () => void;
    onDiscard: (selectedIds: string[]) => Promise<void> | void;
};

function truncate(s: string, max = 72): string {
    const t = s.replace(/\s+/g, ' ').trim();
    if (t.length <= max) return t;
    return t.slice(0, max - 1) + '…';
}

export function humanizeSettingKey(key: string): string {
    return key
        .split('_')
        .filter(Boolean)
        .map((w) => w.charAt(0).toUpperCase() + w.slice(1))
        .join(' ');
}

export function settingChangeDetail(key: string, from: string, to: string): string {
    return `${truncate(formatSettingValue(key, from))} → ${truncate(formatSettingValue(key, to))}`;
}

export function envChangeDetail(from: string | undefined, to: string | undefined, isDelete: boolean): string {
    if (isDelete) {
        return from !== undefined ? `delete (was ${truncate(from || '(empty)')})` : 'delete';
    }
    if (from === undefined) {
        return `new · ${truncate(to || '(empty)')}`;
    }
    return `${truncate(from || '(empty)')} → ${truncate(to || '(empty)')}`;
}

export default function DiscardChangesDialog({
    title,
    description,
    items,
    confirmLabel = 'Discard selected',
    onClose,
    onDiscard,
}: DiscardChangesDialogProps) {
    const [selected, setSelected] = useState<Set<string>>(() => new Set(items.map((i) => i.id)));
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState('');

    // Reset selection when the item list identity changes (re-open / reload).
    const itemKey = useMemo(() => items.map((i) => i.id).join('\0'), [items]);
    useEffect(() => {
        setSelected(new Set(items.map((i) => i.id)));
        setError('');
    }, [itemKey, items]);

    const allSelected = items.length > 0 && selected.size === items.length;
    const noneSelected = selected.size === 0;

    const toggle = (id: string) => {
        setSelected((prev) => {
            const next = new Set(prev);
            if (next.has(id)) next.delete(id);
            else next.add(id);
            return next;
        });
    };

    const selectAll = () => setSelected(new Set(items.map((i) => i.id)));
    const selectNone = () => setSelected(new Set());

    const run = async (ids: string[]) => {
        if (ids.length === 0) return;
        setBusy(true);
        setError('');
        try {
            await onDiscard(ids);
            onClose();
        } catch (e: any) {
            setError(typeof e === 'string' ? e : e?.message || 'Discard failed');
        } finally {
            setBusy(false);
        }
    };

    return (
        <Dialog
            title={title}
            wide
            onClose={onClose}
            footer={
                <>
                    <button type="button" className="btn btn-ghost" onClick={onClose} disabled={busy}>
                        Cancel
                    </button>
                    <button
                        type="button"
                        className="btn btn-ghost"
                        disabled={busy || items.length === 0}
                        onClick={() => void run(items.map((i) => i.id))}
                    >
                        Discard all
                    </button>
                    <button
                        type="button"
                        className="btn btn-danger"
                        disabled={busy || noneSelected}
                        onClick={() => void run([...selected])}
                    >
                        {busy ? 'Discarding…' : `${confirmLabel} (${selected.size})`}
                    </button>
                </>
            }
        >
            <div className="discard-changes">
                <p className="discard-changes-desc">{description}</p>

                {error && <p className="form-error">{error}</p>}

                {items.length === 0 ? (
                    <div className="discard-changes-empty">Nothing to discard.</div>
                ) : (
                    <>
                        <div className="discard-changes-toolbar">
                            <label className="discard-changes-select-all">
                                <input
                                    type="checkbox"
                                    checked={allSelected}
                                    ref={(el) => {
                                        if (el) {
                                            el.indeterminate = !allSelected && selected.size > 0;
                                        }
                                    }}
                                    onChange={() => (allSelected ? selectNone() : selectAll())}
                                />
                                <span>
                                    {allSelected
                                        ? 'All selected'
                                        : selected.size === 0
                                            ? 'None selected'
                                            : `${selected.size} of ${items.length} selected`}
                                </span>
                            </label>
                            <div className="discard-changes-toolbar-actions">
                                <button type="button" className="btn btn-ghost" onClick={selectAll}>
                                    Select all
                                </button>
                                <button type="button" className="btn btn-ghost" onClick={selectNone}>
                                    Select none
                                </button>
                            </div>
                        </div>

                        <ul className="discard-changes-list">
                            {items.map((item) => {
                                const checked = selected.has(item.id);
                                return (
                                    <li
                                        key={item.id}
                                        className={`discard-changes-item${checked ? '' : ' discard-changes-item--off'}`}
                                    >
                                        <label className="discard-changes-item-label">
                                            <input
                                                type="checkbox"
                                                checked={checked}
                                                onChange={() => toggle(item.id)}
                                            />
                                            <span className="discard-changes-item-body">
                                                <span className="discard-changes-item-top">
                                                    <span className={`discard-changes-kind discard-changes-kind--${item.kind}`}>
                                                        {item.kind === 'setting' ? 'Setting' : 'Variable'}
                                                    </span>
                                                    {item.badge && (
                                                        <span className="discard-changes-badge">{item.badge}</span>
                                                    )}
                                                    <span className="discard-changes-item-name">{item.label}</span>
                                                </span>
                                                {item.detail && (
                                                    <span className="discard-changes-item-detail mono">{item.detail}</span>
                                                )}
                                            </span>
                                        </label>
                                    </li>
                                );
                            })}
                        </ul>
                    </>
                )}
            </div>
        </Dialog>
    );
}
