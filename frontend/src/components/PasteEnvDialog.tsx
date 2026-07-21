import {useEffect, useMemo, useState} from 'react';
import {AlertCircle, ClipboardPaste, Eye, EyeOff} from 'lucide-react';
import Dialog from './Dialog';
import {isValidEnvKey, parseEnvText, type ParsedEnvEntry} from '../lib/parseEnvText';
import './PasteEnvDialog.css';

export type PasteEnvExisting = {
    key: string;
    /** When omitted (e.g. masked secrets list), conflict is "exists" without value compare. */
    value?: string;
};

export type PasteEnvApplyEntry = {
    key: string;
    value: string;
};

type ReviewRow = {
    key: string;
    value: string;
    line: number;
    included: boolean;
    status: 'new' | 'update' | 'unchanged' | 'exists' | 'invalid';
    existingValue?: string;
    reason?: string;
};

type PasteEnvDialogProps = {
    title: string;
    description: string;
    existing: PasteEnvExisting[];
    /** Placeholder shown in the paste box. */
    placeholder?: string;
    applyLabel?: string;
    onClose: () => void;
    onApply: (entries: PasteEnvApplyEntry[]) => Promise<void> | void;
};

function buildRows(
    entries: ParsedEnvEntry[],
    existingByKey: Map<string, PasteEnvExisting>,
): ReviewRow[] {
    return entries.map((e) => {
        if (!isValidEnvKey(e.key)) {
            return {
                key: e.key,
                value: e.value,
                line: e.line,
                included: false,
                status: 'invalid' as const,
                reason: 'invalid key',
            };
        }
        const ex = existingByKey.get(e.key);
        if (!ex) {
            return {
                key: e.key,
                value: e.value,
                line: e.line,
                included: true,
                status: 'new' as const,
            };
        }
        if (ex.value === undefined) {
            return {
                key: e.key,
                value: e.value,
                line: e.line,
                included: true,
                status: 'exists' as const,
                reason: 'key already exists — will overwrite',
            };
        }
        if (ex.value === e.value) {
            return {
                key: e.key,
                value: e.value,
                line: e.line,
                included: false,
                status: 'unchanged' as const,
                existingValue: ex.value,
            };
        }
        return {
            key: e.key,
            value: e.value,
            line: e.line,
            included: true,
            status: 'update' as const,
            existingValue: ex.value,
        };
    });
}

export default function PasteEnvDialog({
    title,
    description,
    existing,
    placeholder = 'KEY=value\nANOTHER_KEY=…',
    applyLabel = 'Add selected',
    onClose,
    onApply,
}: PasteEnvDialogProps) {
    const [text, setText] = useState('');
    const [rows, setRows] = useState<ReviewRow[] | null>(null);
    const [invalid, setInvalid] = useState<ReturnType<typeof parseEnvText>['invalid']>([]);
    const [revealAll, setRevealAll] = useState(false);
    const [revealed, setRevealed] = useState<Record<string, boolean>>({});
    const [saving, setSaving] = useState(false);
    const [error, setError] = useState('');

    const existingByKey = useMemo(() => {
        const m = new Map<string, PasteEnvExisting>();
        for (const e of existing) m.set(e.key, e);
        return m;
    }, [existing]);

    // Re-parse whenever paste text changes so review stays in sync.
    useEffect(() => {
        if (!text.trim()) {
            setRows(null);
            setInvalid([]);
            return;
        }
        const parsed = parseEnvText(text);
        setInvalid(parsed.invalid);
        setRows(buildRows(parsed.entries, existingByKey));
    }, [text, existingByKey]);

    const selectedCount = rows?.filter((r) => r.included && r.status !== 'invalid').length ?? 0;
    const newCount = rows?.filter((r) => r.included && r.status === 'new').length ?? 0;
    const updateCount = rows?.filter((r) => r.included && (r.status === 'update' || r.status === 'exists')).length ?? 0;

    const setIncluded = (key: string, included: boolean) => {
        setRows((prev) =>
            prev?.map((r) => (r.key === key && r.status !== 'invalid' ? {...r, included} : r)) ?? null,
        );
    };

    const setValue = (key: string, value: string) => {
        setRows((prev) =>
            prev?.map((r) => {
                if (r.key !== key || r.status === 'invalid') return r;
                const ex = existingByKey.get(key);
                let status: ReviewRow['status'] = 'new';
                if (!ex) status = 'new';
                else if (ex.value === undefined) status = 'exists';
                else if (ex.value === value) status = 'unchanged';
                else status = 'update';
                return {
                    ...r,
                    value,
                    status,
                    // Editing to match existing unchecks; any real change re-includes.
                    included: status !== 'unchanged',
                };
            }) ?? null,
        );
    };

    const selectAll = (included: boolean) => {
        setRows((prev) =>
            prev?.map((r) =>
                r.status === 'invalid' || r.status === 'unchanged'
                    ? r
                    : {...r, included},
            ) ?? null,
        );
    };

    const handleApply = async () => {
        if (!rows || selectedCount === 0) return;
        setSaving(true);
        setError('');
        try {
            const entries = rows
                .filter((r) => r.included && r.status !== 'invalid')
                .map((r) => ({key: r.key, value: r.value}));
            await onApply(entries);
            onClose();
        } catch (e: any) {
            setError(typeof e === 'string' ? e : e?.message || 'Could not apply variables');
        } finally {
            setSaving(false);
        }
    };

    const statusLabel = (s: ReviewRow['status']) => {
        switch (s) {
            case 'new': return 'new';
            case 'update': return 'update';
            case 'unchanged': return 'unchanged';
            case 'exists': return 'overwrite';
            case 'invalid': return 'invalid';
        }
    };

    return (
        <Dialog
            title={title}
            wide
            onClose={onClose}
            footer={
                <>
                    <button type="button" className="btn btn-ghost" onClick={onClose} disabled={saving}>
                        Cancel
                    </button>
                    <button
                        type="button"
                        className="btn btn-primary"
                        onClick={() => void handleApply()}
                        disabled={saving || selectedCount === 0}
                    >
                        {saving ? 'Adding…' : `${applyLabel} (${selectedCount})`}
                    </button>
                </>
            }
        >
            <div className="paste-env">
                <p className="paste-env-desc">{description}</p>

                <div className="form-field">
                    <label className="form-label" htmlFor="paste-env-text">
                        <ClipboardPaste size={13}/> Paste KEY=value lines
                    </label>
                    <textarea
                        id="paste-env-text"
                        className="input paste-env-textarea"
                        value={text}
                        onChange={(e) => setText(e.target.value)}
                        placeholder={placeholder}
                        spellCheck={false}
                        autoFocus
                        rows={8}
                    />
                    <span className="settings-hint">
                        Supports comments (#), <code>export KEY=…</code>, quoted values, and multiline JSON braces.
                        Duplicate keys keep the last value.
                    </span>
                </div>

                {error && (
                    <div className="paste-env-error">
                        <AlertCircle size={13}/> {error}
                    </div>
                )}

                {invalid.length > 0 && (
                    <div className="paste-env-invalid">
                        <strong>Skipped invalid lines</strong>
                        <ul>
                            {invalid.map((inv) => (
                                <li key={`${inv.line}-${inv.raw}`}>
                                    line {inv.line}: {inv.reason}
                                    <code>{inv.raw.length > 80 ? inv.raw.slice(0, 80) + '…' : inv.raw}</code>
                                </li>
                            ))}
                        </ul>
                    </div>
                )}

                {rows && rows.length === 0 && text.trim() && (
                    <div className="paste-env-empty">No KEY=value pairs found in the paste.</div>
                )}

                {rows && rows.length > 0 && (
                    <div className="paste-env-review">
                        <div className="paste-env-review-head">
                            <div className="paste-env-review-summary">
                                <strong>Review</strong>
                                <span>
                                    {newCount} new
                                    {updateCount > 0 ? ` · ${updateCount} update` : ''}
                                    {rows.filter((r) => r.status === 'unchanged').length > 0
                                        ? ` · ${rows.filter((r) => r.status === 'unchanged').length} unchanged`
                                        : ''}
                                </span>
                            </div>
                            <div className="paste-env-review-actions">
                                <button type="button" className="btn btn-ghost" onClick={() => selectAll(true)}>
                                    Select all
                                </button>
                                <button type="button" className="btn btn-ghost" onClick={() => selectAll(false)}>
                                    Select none
                                </button>
                                <button
                                    type="button"
                                    className="btn btn-ghost"
                                    onClick={() => setRevealAll((v) => !v)}
                                    title={revealAll ? 'Hide values' : 'Show values'}
                                >
                                    {revealAll ? <EyeOff size={13}/> : <Eye size={13}/>}
                                    {revealAll ? 'Hide' : 'Show'} values
                                </button>
                            </div>
                        </div>

                        <div className="paste-env-table-wrap">
                            <table className="paste-env-table">
                                <thead>
                                    <tr>
                                        <th className="paste-env-col-check" aria-label="Include"/>
                                        <th className="paste-env-col-status">Status</th>
                                        <th className="paste-env-col-key">Key</th>
                                        <th className="paste-env-col-value">Value</th>
                                    </tr>
                                </thead>
                                <tbody>
                                    {rows.map((r) => {
                                        const show = revealAll || revealed[r.key];
                                        return (
                                            <tr
                                                key={r.key}
                                                className={`paste-env-row paste-env-row--${r.status}${r.included ? '' : ' paste-env-row--off'}`}
                                            >
                                                <td className="paste-env-col-check">
                                                    <input
                                                        type="checkbox"
                                                        checked={r.included}
                                                        disabled={r.status === 'invalid'}
                                                        onChange={(e) => setIncluded(r.key, e.target.checked)}
                                                        aria-label={`Include ${r.key}`}
                                                    />
                                                </td>
                                                <td className="paste-env-col-status">
                                                    <span className={`paste-env-badge paste-env-badge--${r.status}`}>
                                                        {statusLabel(r.status)}
                                                    </span>
                                                </td>
                                                <td className="paste-env-col-key">
                                                    <code>{r.key}</code>
                                                </td>
                                                <td className="paste-env-col-value">
                                                    <div className="paste-env-value-row">
                                                        <input
                                                            className="input paste-env-value-input"
                                                            type={show ? 'text' : 'password'}
                                                            value={r.value}
                                                            disabled={r.status === 'invalid'}
                                                            onChange={(e) => setValue(r.key, e.target.value)}
                                                            autoComplete="off"
                                                            spellCheck={false}
                                                        />
                                                        <button
                                                            type="button"
                                                            className="btn btn-ghost"
                                                            onClick={() =>
                                                                setRevealed((prev) => ({...prev, [r.key]: !prev[r.key]}))
                                                            }
                                                            title={show ? 'Hide' : 'Show'}
                                                        >
                                                            {show ? <EyeOff size={13}/> : <Eye size={13}/>}
                                                        </button>
                                                    </div>
                                                    {r.status === 'update' && r.existingValue !== undefined && (
                                                        <div className="paste-env-existing" title={r.existingValue}>
                                                            was: <code>{r.existingValue.length > 60 ? r.existingValue.slice(0, 60) + '…' : r.existingValue}</code>
                                                        </div>
                                                    )}
                                                    {r.reason && r.status !== 'update' && (
                                                        <div className="paste-env-existing">{r.reason}</div>
                                                    )}
                                                </td>
                                            </tr>
                                        );
                                    })}
                                </tbody>
                            </table>
                        </div>
                    </div>
                )}
            </div>
        </Dialog>
    );
}
