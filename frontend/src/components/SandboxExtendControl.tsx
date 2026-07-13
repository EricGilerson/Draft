import {useEffect, useRef, useState} from 'react';
import {ChevronDown, Clock3} from 'lucide-react';
import {ExtendSandbox, ExtendSandboxUntil} from '../../wailsjs/go/main/App';
import {store} from '../../wailsjs/go/models';
import {
    SANDBOX_EXTEND_PRESETS,
    formatExtendPreview,
    fromDatetimeLocalValue,
    parseExtendDurationHours,
    previewExtendedExpiry,
    toDatetimeLocalValue,
} from '../lib/sandboxExtend';
import './SandboxExtendControl.css';

type Props = {
    sandboxId: number;
    currentExpiresAt?: unknown;
    onExtended?: (sandbox: store.Sandbox) => void;
    onError?: (message: string) => void;
    compact?: boolean;
};

export default function SandboxExtendControl({sandboxId, currentExpiresAt, onExtended, onError, compact = false}: Props) {
    const [open, setOpen] = useState(false);
    const [busy, setBusy] = useState(false);
    const [customDuration, setCustomDuration] = useState('');
    const [customError, setCustomError] = useState('');
    const [untilValue, setUntilValue] = useState(() => {
        const base = currentExpiresAt ? new Date(currentExpiresAt as string | number | Date) : new Date();
        if (Number.isNaN(base.valueOf()) || base.getTime() <= Date.now()) {
            return toDatetimeLocalValue(new Date(Date.now() + 24 * 60 * 60 * 1000));
        }
        return toDatetimeLocalValue(new Date(base.getTime() + 24 * 60 * 60 * 1000));
    });
    const rootRef = useRef<HTMLDivElement | null>(null);

    useEffect(() => {
        if (!open) return;
        const onDoc = (e: MouseEvent) => {
            if (rootRef.current && !rootRef.current.contains(e.target as Node)) {
                setOpen(false);
            }
        };
        document.addEventListener('mousedown', onDoc);
        return () => document.removeEventListener('mousedown', onDoc);
    }, [open]);

    const run = async (action: () => Promise<store.Sandbox>) => {
        if (busy) return;
        setBusy(true);
        setCustomError('');
        try {
            const sandbox = await action();
            onExtended?.(sandbox);
            setOpen(false);
            setCustomDuration('');
        } catch (e) {
            const message = String(e);
            setCustomError(message);
            onError?.(message);
        } finally {
            setBusy(false);
        }
    };

    const extendHours = (hours: number) => void run(() => ExtendSandbox(sandboxId, hours));

    const applyCustomDuration = () => {
        const hours = parseExtendDurationHours(customDuration);
        if (hours == null) {
            setCustomError('Use a duration like 12h, 2d, 1w, or a whole number of hours.');
            return;
        }
        extendHours(hours);
    };

    const applyUntil = () => {
        const date = fromDatetimeLocalValue(untilValue);
        if (!date) {
            setCustomError('Pick a valid date and time.');
            return;
        }
        if (date.getTime() <= Date.now()) {
            setCustomError('Expiry must be in the future.');
            return;
        }
        void run(() => ExtendSandboxUntil(sandboxId, date.toISOString()));
    };

    const untilDate = fromDatetimeLocalValue(untilValue);
    const customHours = parseExtendDurationHours(customDuration);

    return (
        <div className={'sandbox-extend' + (compact ? ' sandbox-extend--compact' : '')} ref={rootRef}>
            <button
                type="button"
                className="btn btn-ghost sandbox-extend-trigger"
                disabled={busy}
                onClick={() => setOpen((value) => !value)}
                aria-expanded={open}
            >
                <Clock3 size={13}/>
                Extend
                <ChevronDown size={13}/>
            </button>
            {open && (
                <div className="sandbox-extend-menu" role="menu">
                    <div className="sandbox-extend-presets">
                        {SANDBOX_EXTEND_PRESETS.map((preset) => (
                            <button
                                key={preset.hours}
                                type="button"
                                className="sandbox-extend-preset"
                                disabled={busy}
                                onClick={() => extendHours(preset.hours)}
                                title={`Adds ${preset.label} to the remaining lifetime`}
                            >
                                +{preset.label}
                            </button>
                        ))}
                    </div>

                    <div className="sandbox-extend-section">
                        <label className="sandbox-extend-label" htmlFor={`sandbox-extend-duration-${sandboxId}`}>
                            Add custom duration
                        </label>
                        <div className="sandbox-extend-row">
                            <input
                                id={`sandbox-extend-duration-${sandboxId}`}
                                className="input"
                                value={customDuration}
                                placeholder="e.g. 12h, 2d, 1w"
                                disabled={busy}
                                onChange={(e) => {
                                    setCustomDuration(e.target.value);
                                    setCustomError('');
                                }}
                                onKeyDown={(e) => {
                                    if (e.key === 'Enter') applyCustomDuration();
                                }}
                            />
                            <button type="button" className="btn btn-ghost" disabled={busy} onClick={applyCustomDuration}>
                                Apply
                            </button>
                        </div>
                        {customHours != null && (
                            <p className="sandbox-extend-hint">
                                Adds {customHours}h → expires {formatExtendPreview(previewExtendedExpiry(customHours, currentExpiresAt))}
                            </p>
                        )}
                    </div>

                    <div className="sandbox-extend-section">
                        <label className="sandbox-extend-label" htmlFor={`sandbox-extend-until-${sandboxId}`}>
                            Set expiry to date & time
                        </label>
                        <div className="sandbox-extend-row">
                            <input
                                id={`sandbox-extend-until-${sandboxId}`}
                                className="input"
                                type="datetime-local"
                                value={untilValue}
                                disabled={busy}
                                onChange={(e) => {
                                    setUntilValue(e.target.value);
                                    setCustomError('');
                                }}
                            />
                            <button type="button" className="btn btn-primary" disabled={busy} onClick={applyUntil}>
                                Set
                            </button>
                        </div>
                        {untilDate && untilDate.getTime() > Date.now() && (
                            <p className="sandbox-extend-hint">
                                Expires {formatExtendPreview(untilDate)}. Auto-delete still waits for the sandbox grace period after that.
                            </p>
                        )}
                    </div>

                    {customError && <p className="sandbox-extend-error">{customError}</p>}
                </div>
            )}
        </div>
    );
}
