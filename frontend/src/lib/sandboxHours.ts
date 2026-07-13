export function parseSandboxHours(raw: string, min = 0): number {
    const parsed = Number.parseInt(raw, 10);
    if (!Number.isFinite(parsed)) return min;
    return Math.max(min, parsed);
}

export function hasSandboxHoursDecimal(raw: string): boolean {
    return raw.includes('.');
}

export function sandboxHoursDecimalWarning(raw: string, min = 0): string | null {
    if (!hasSandboxHoursDecimal(raw)) return null;
    const saved = parseSandboxHours(raw, min);
    const unit = saved === 1 ? 'hour' : 'hours';
    return `Decimals are rounded down to whole hours. This will be saved as ${saved} ${unit}.`;
}
