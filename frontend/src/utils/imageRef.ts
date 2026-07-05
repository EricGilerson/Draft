// Helpers for the image-mode version picker. The backend stores a curated list
// of tags (ServiceTemplate.imageTags, JSON array) plus a default full ref
// (ServiceTemplate.image). The base image name is derived from the default ref
// by stripping the tag, so a curated tag list composes back into full refs.

export type ImageOption = {
    /** Full ref, e.g. "postgres:16-alpine". This is what gets stamped. */
    ref: string;
    /** Display label, e.g. "16-alpine". */
    label: string;
};

/**
 * Split an image ref into base name + tag. The tag is the portion after the
 * last ':' that follows the last '/'; if none, the tag is "latest" (Docker's
 * implicit default). Handles "postgres:16-alpine", "library/nginx:1.27", and
 * "myregistry.io:5000/postgres:16".
 */
export function splitImageRef(ref: string): { base: string; tag: string } {
    const r = (ref || '').trim();
    if (!r) return { base: '', tag: '' };
    const slash = r.lastIndexOf('/');
    if (slash >= 0) {
        const head = r.slice(0, slash + 1);
        const tail = r.slice(slash + 1);
        const colon = tail.lastIndexOf(':');
        if (colon >= 0) return { base: head + tail.slice(0, colon), tag: tail.slice(colon + 1) };
        return { base: head + tail, tag: 'latest' };
    }
    const colon = r.lastIndexOf(':');
    if (colon >= 0) return { base: r.slice(0, colon), tag: r.slice(colon + 1) };
    return { base: r, tag: 'latest' };
}

/**
 * Parse a template's imageTags JSON into a list of tag strings. Malformed JSON
 * or an empty value yields an empty list (the caller can fall back to free-text).
 */
export function parseImageTags(raw: string | undefined | null): string[] {
    if (!raw) return [];
    try {
        const parsed = JSON.parse(raw);
        if (!Array.isArray(parsed)) return [];
        return parsed.filter((t): t is string => typeof t === 'string' && t.trim().length > 0);
    } catch {
        return [];
    }
}

/**
 * Build the curated option list for an image-mode template. The default ref is
 * always present (even if its tag isn't in the curated list) so the picker's
 * initial value is always a real option. Tags are composed with the base name
 * derived from the default ref.
 */
export function buildImageOptions(defaultRef: string, tagsJson: string | undefined | null): ImageOption[] {
    const { base } = splitImageRef(defaultRef);
    if (!base) return [];
    const tags = parseImageTags(tagsJson);
    const seen = new Set<string>();
    const out: ImageOption[] = [];

    // Always include the default ref first.
    const defaultTag = splitImageRef(defaultRef).tag;
    const defaultOpt = { ref: defaultRef, label: defaultTag };
    out.push(defaultOpt);
    seen.add(defaultRef);

    for (const tag of tags) {
        const ref = `${base}:${tag}`;
        if (seen.has(ref)) continue;
        seen.add(ref);
        out.push({ ref, label: tag });
    }
    return out;
}

export const CUSTOM_IMAGE_VALUE = '__draft_custom_image__';
