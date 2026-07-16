const STORAGE_KEY = 'draft.agents.launcher';

export type AgentsLauncherPrefs = {
    agentId?: string;
    cwd?: string;
    ephemeral?: boolean;
};

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
        };
        // Allow explicit empty cwd to clear.
        if (Object.prototype.hasOwnProperty.call(prefs, 'cwd')) {
            next.cwd = prefs.cwd;
        }
        localStorage.setItem(STORAGE_KEY, JSON.stringify(next));
    } catch {
        /* quota / private mode */
    }
}
