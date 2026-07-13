export type SandboxExtendPreset = {
    label: string;
    hours: number;
};

export const SANDBOX_EXTEND_PRESETS: SandboxExtendPreset[] = [
    {label: '1 hour', hours: 1},
    {label: '6 hours', hours: 6},
    {label: '12 hours', hours: 12},
    {label: '1 day', hours: 24},
    {label: '3 days', hours: 72},
    {label: '7 days', hours: 168},
    {label: '14 days', hours: 336},
    {label: '30 days', hours: 720},
];

/** Parse free-text durations like "12h", "2d", "1w", "3 days", or a bare hour count. */
export function parseExtendDurationHours(raw: string): number | null {
    const text = raw.trim().toLowerCase();
    if (!text) return null;

    const bare = Number.parseInt(text, 10);
    if (/^\d+$/.test(text) && Number.isFinite(bare) && bare > 0) {
        return bare;
    }

    const match = text.match(/^(\d+(?:\.\d+)?)\s*(h|hr|hrs|hour|hours|d|day|days|w|wk|wks|week|weeks)$/);
    if (!match) return null;

    const amount = Number(match[1]);
    if (!Number.isFinite(amount) || amount <= 0) return null;

    const unit = match[2];
    let hours = amount;
    if (unit.startsWith('d')) hours = amount * 24;
    else if (unit.startsWith('w')) hours = amount * 24 * 7;

    const rounded = Math.round(hours);
    return rounded > 0 ? rounded : null;
}

/** Base time for additive extensions: remaining expiry if still future, otherwise now. */
export function extendFromTime(currentExpiresAt?: unknown, now = new Date()): Date {
    if (currentExpiresAt == null) return now;
    const expires = new Date(currentExpiresAt as string | number | Date);
    if (Number.isNaN(expires.valueOf()) || expires.getTime() <= now.getTime()) {
        return now;
    }
    return expires;
}

export function previewExtendedExpiry(hours: number, currentExpiresAt?: unknown, now = new Date()): Date {
    return new Date(extendFromTime(currentExpiresAt, now).getTime() + hours * 60 * 60 * 1000);
}

export function hoursUntil(date: Date, now = new Date()): number | null {
    const ms = date.getTime() - now.getTime();
    if (!Number.isFinite(ms) || ms <= 0) return null;
    return Math.max(1, Math.ceil(ms / (60 * 60 * 1000)));
}

export function toDatetimeLocalValue(date: Date): string {
    const pad = (n: number) => String(n).padStart(2, '0');
    return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

export function fromDatetimeLocalValue(value: string): Date | null {
    if (!value) return null;
    const date = new Date(value);
    return Number.isNaN(date.valueOf()) ? null : date;
}

export function formatExtendPreview(expiresAt: Date): string {
    return expiresAt.toLocaleString();
}
