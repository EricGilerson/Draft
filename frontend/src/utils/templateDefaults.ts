/** Parse ServiceTemplate.defaultSettings JSON into a key→value map. */
export function parseDefaultSettings(raw?: string | null): Record<string, string> {
    if (!raw || !raw.trim()) return {};
    try {
        const obj = JSON.parse(raw);
        if (!obj || typeof obj !== 'object' || Array.isArray(obj)) return {};
        const out: Record<string, string> = {};
        for (const [k, v] of Object.entries(obj)) {
            if (!k) continue;
            out[k] = v == null ? '' : String(v);
        }
        return out;
    } catch {
        return {};
    }
}

/** Serialize a settings map for ServiceTemplate.defaultSettings. Empty → "". */
export function serializeDefaultSettings(m: Record<string, string>): string {
    const out: Record<string, string> = {};
    for (const [k, v] of Object.entries(m)) {
        const key = k.trim();
        if (!key) continue;
        const val = (v ?? '').trim();
        // Drop empty values so HTTP defaults stay compact (no route_protocol:"" noise).
        if (val === '') continue;
        out[key] = val;
    }
    if (Object.keys(out).length === 0) return '';
    return JSON.stringify(out);
}

/** Effective route protocol for a template's default settings. */
export function templateRouteProtocol(raw?: string | null): 'http' | 'tcp' {
    const m = parseDefaultSettings(raw);
    return m.route_protocol === 'tcp' ? 'tcp' : 'http';
}
