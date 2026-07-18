const STORAGE_KEY = 'draft.agents.launcher';
const AGENTS_CACHE_KEY = 'draft.agents.detected';

export type AgentsLauncherPrefs = {
    agentId?: string;
    cwd?: string;
    ephemeral?: boolean;
    plain?: boolean;
    /** When true, hide page header + start form so the terminal can use more space. */
    launcherCollapsed?: boolean;
    /** User-ordered agent session tab ids. */
    tabOrder?: string[];
};

/** Last successful ListAgents snapshot — used to paint the Agents tab immediately. */
export type CachedAgentInfo = {
    id: string;
    name: string;
    binary: string;
    path: string;
    version?: string;
    installed: boolean;
    supportsEphemeral: boolean;
    mcpConfigured: boolean;
    error?: string;
};

function parseTabOrder(value: unknown): string[] | undefined {
    if (!Array.isArray(value)) return undefined;
    const ids = value.filter((id): id is string => typeof id === 'string' && id.length > 0);
    return ids.length > 0 ? ids : undefined;
}

export function loadAgentsPrefs(): AgentsLauncherPrefs {
    try {
        const raw = localStorage.getItem(STORAGE_KEY);
        if (!raw) return {};
        const parsed = JSON.parse(raw) as AgentsLauncherPrefs;
        if (!parsed || typeof parsed !== 'object') return {};
        return {
            agentId: typeof parsed.agentId === 'string' ? parsed.agentId : undefined,
            cwd: typeof parsed.cwd === 'string' ? parsed.cwd : undefined,
            ephemeral: typeof parsed.ephemeral === 'boolean' ? parsed.ephemeral : undefined,
            plain: typeof parsed.plain === 'boolean' ? parsed.plain : undefined,
            launcherCollapsed:
                typeof parsed.launcherCollapsed === 'boolean' ? parsed.launcherCollapsed : undefined,
            tabOrder: parseTabOrder(parsed.tabOrder),
        };
    } catch {
        return {};
    }
}

export function saveAgentsPrefs(prefs: AgentsLauncherPrefs): void {
    try {
        const prev = loadAgentsPrefs();
        const next: AgentsLauncherPrefs = {
            agentId: prefs.agentId ?? prev.agentId,
            cwd: prefs.cwd ?? prev.cwd,
            ephemeral: prefs.ephemeral ?? prev.ephemeral,
            plain: prefs.plain ?? prev.plain,
            launcherCollapsed: prefs.launcherCollapsed ?? prev.launcherCollapsed,
            tabOrder: prefs.tabOrder ?? prev.tabOrder,
        };
        // Allow explicit empty cwd to clear.
        if (Object.prototype.hasOwnProperty.call(prefs, 'cwd')) {
            next.cwd = prefs.cwd;
        }
        if (Object.prototype.hasOwnProperty.call(prefs, 'plain')) {
            next.plain = prefs.plain;
        }
        if (Object.prototype.hasOwnProperty.call(prefs, 'ephemeral')) {
            next.ephemeral = prefs.ephemeral;
        }
        if (Object.prototype.hasOwnProperty.call(prefs, 'launcherCollapsed')) {
            next.launcherCollapsed = prefs.launcherCollapsed;
        }
        if (Object.prototype.hasOwnProperty.call(prefs, 'tabOrder')) {
            next.tabOrder = prefs.tabOrder;
        }
        localStorage.setItem(STORAGE_KEY, JSON.stringify(next));
    } catch {
        /* quota / private mode */
    }
}

function isCachedAgent(value: unknown): value is CachedAgentInfo {
    if (!value || typeof value !== 'object') return false;
    const a = value as Record<string, unknown>;
    return (
        typeof a.id === 'string' &&
        typeof a.name === 'string' &&
        typeof a.binary === 'string' &&
        typeof a.path === 'string' &&
        typeof a.installed === 'boolean' &&
        typeof a.supportsEphemeral === 'boolean' &&
        typeof a.mcpConfigured === 'boolean'
    );
}

/** Returns the last detected agent list, or null when nothing usable is cached. */
export function loadCachedAgents(): CachedAgentInfo[] | null {
    try {
        const raw = localStorage.getItem(AGENTS_CACHE_KEY);
        if (!raw) return null;
        const parsed = JSON.parse(raw) as {agents?: unknown};
        if (!parsed || !Array.isArray(parsed.agents)) return null;
        const agents = parsed.agents.filter(isCachedAgent);
        return agents.length > 0 ? agents : null;
    } catch {
        return null;
    }
}

export function saveCachedAgents(list: CachedAgentInfo[]): void {
    if (!list.length) return;
    try {
        localStorage.setItem(
            AGENTS_CACHE_KEY,
            JSON.stringify({
                savedAt: Date.now(),
                agents: list.map((a) => ({
                    id: a.id,
                    name: a.name,
                    binary: a.binary,
                    path: a.path,
                    version: a.version,
                    installed: a.installed,
                    supportsEphemeral: a.supportsEphemeral,
                    mcpConfigured: a.mcpConfigured,
                    error: a.error,
                })),
            }),
        );
    } catch {
        /* quota / private mode */
    }
}

/** Apply a saved tab order; unknown ids keep relative API order at the end. */
export function applySessionTabOrder<T extends {id: string}>(list: T[], order?: string[]): T[] {
    if (!order?.length || list.length <= 1) return list;
    const byId = new Map(list.map((s) => [s.id, s]));
    const ordered: T[] = [];
    for (const id of order) {
        const s = byId.get(id);
        if (!s) continue;
        ordered.push(s);
        byId.delete(id);
    }
    for (const s of list) {
        if (byId.has(s.id)) ordered.push(s);
    }
    return ordered;
}
