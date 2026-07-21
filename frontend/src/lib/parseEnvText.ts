/**
 * Parse KEY=value text (dotenv-style paste) into ordered entries.
 * Mirrors internal/envfile.Read for multiline JSON brace values, plus common
 * paste conveniences: export prefix, simple quotes, BOM strip.
 */

export type ParsedEnvEntry = {
    key: string;
    value: string;
    /** 1-based line of the KEY= start (for review UI). */
    line: number;
};

export type ParseEnvTextResult = {
    entries: ParsedEnvEntry[];
    /** Lines that looked like assignments but had invalid keys. */
    invalid: {line: number; raw: string; reason: string}[];
    /** Blank / comment lines ignored (not an error). */
    skippedLines: number;
};

/** Same rule as store.validateEnvKey / envKeyPattern. */
const ENV_KEY_RE = /^[A-Za-z_][A-Za-z0-9_]*$/;

export function isValidEnvKey(key: string): boolean {
    return ENV_KEY_RE.test(key);
}

function braceDelta(line: string): number {
    let delta = 0;
    let inString = false;
    let escaped = false;
    for (const r of line) {
        if (escaped) {
            escaped = false;
            continue;
        }
        if (r === '\\') {
            escaped = true;
            continue;
        }
        if (r === '"') {
            inString = !inString;
            continue;
        }
        if (inString) continue;
        if (r === '{') delta++;
        else if (r === '}') delta--;
    }
    return delta;
}

/** Strip matching single or double quotes around a whole value. */
function unquote(val: string): string {
    if (val.length < 2) return val;
    const a = val[0];
    const b = val[val.length - 1];
    if ((a === '"' && b === '"') || (a === "'" && b === "'")) {
        return val.slice(1, -1);
    }
    return val;
}

/**
 * Parse dotenv-style text. Later duplicate keys win (same as typical .env loaders).
 * Returns ordered unique keys (last write order).
 */
export function parseEnvText(text: string): ParseEnvTextResult {
    const raw = text.replace(/^\uFEFF/, '');
    const lines = raw.split(/\r?\n/);
    const byKey = new Map<string, ParsedEnvEntry>();
    const invalid: ParseEnvTextResult['invalid'] = [];
    let skippedLines = 0;

    let i = 0;
    while (i < lines.length) {
        const lineNo = i + 1;
        let line = lines[i];
        i++;

        const trimmed = line.trim();
        if (trimmed === '' || trimmed.startsWith('#')) {
            skippedLines++;
            continue;
        }

        // Optional "export KEY=..."
        let work = trimmed;
        if (/^export\s+/i.test(work)) {
            work = work.replace(/^export\s+/i, '');
        }

        const eq = work.indexOf('=');
        if (eq === -1) {
            // Not an assignment — ignore bare words (common when pasting shell output).
            skippedLines++;
            continue;
        }

        let key = work.slice(0, eq).trim();
        let val = work.slice(eq + 1).trim();

        // Drop trailing inline comment only when value is unquoted and comment is " #"
        // (do not strip URLs or values that legitimately contain #).
        if (
            !val.startsWith('"') &&
            !val.startsWith("'") &&
            !val.startsWith('{')
        ) {
            const hash = val.search(/(^|\s)#/);
            if (hash > 0 && /\s#/.test(val.slice(hash - 1 < 0 ? 0 : hash - 1))) {
                // only strip " # comment" form
                const m = val.match(/^(.*?)(\s+#.*)$/);
                if (m) val = m[1].trimEnd();
            }
        }

        if (!key) {
            invalid.push({line: lineNo, raw: trimmed, reason: 'empty key'});
            continue;
        }
        if (!isValidEnvKey(key)) {
            invalid.push({
                line: lineNo,
                raw: trimmed,
                reason: 'key must match [A-Za-z_][A-Za-z0-9_]*',
            });
            continue;
        }

        // Multiline JSON / brace-balanced values (same as envfile.Read).
        if (val.startsWith('{') && braceDelta(val) > 0) {
            const parts = [val];
            let balance = braceDelta(val);
            while (balance > 0 && i < lines.length) {
                const next = lines[i];
                i++;
                parts.push(next);
                balance += braceDelta(next);
            }
            val = parts.join('\n');
        } else {
            val = unquote(val);
        }

        byKey.set(key, {key, value: val, line: lineNo});
    }

    return {
        entries: Array.from(byKey.values()),
        invalid,
        skippedLines,
    };
}
