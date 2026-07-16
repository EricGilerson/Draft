import {useCallback, useEffect, useMemo, useRef, useState} from 'react';
import {FolderOpen, Plus, RefreshCw, Square, SquareTerminal, X} from 'lucide-react';
import '@xterm/xterm/css/xterm.css';
import {
    ListAgents,
    ListAgentSessions,
    SelectFolder,
    StartAgentSession,
    StopAgentSession,
} from '../../wailsjs/go/main/App';
import {EventsOn} from '../../wailsjs/runtime/runtime';
import {agents, store} from '../../wailsjs/go/models';
import PageHeader from '../components/PageHeader';
import {attachAgentTerminal, disposeAgentTerminal, focusAgentTerminal} from '../lib/agentTerminalHost';
import {loadAgentsPrefs, saveAgentsPrefs} from '../lib/agentsPrefs';
import './WorkspaceViews.css';
import './AgentsView.css';

type AgentsViewProps = {
    projects: store.Project[];
};

function AgentSessionTerminal({
    sessionId,
    active,
}: {
    sessionId: string;
    active: boolean;
}) {
    const hostRef = useRef<HTMLDivElement>(null);

    useEffect(() => {
        const host = hostRef.current;
        if (!host) return;
        return attachAgentTerminal(sessionId, host);
    }, [sessionId]);

    useEffect(() => {
        if (!active) return;
        // Defer one frame so layout (display/visibility) is settled before fit/focus.
        const id = requestAnimationFrame(() => focusAgentTerminal(sessionId));
        return () => cancelAnimationFrame(id);
    }, [active, sessionId]);

    return (
        <div
            className={'agents-term-host' + (active ? ' agents-term-host--active' : '')}
            ref={hostRef}
            aria-hidden={!active}
        />
    );
}

export default function AgentsView({projects}: AgentsViewProps) {
    const savedPrefs = useMemo(() => loadAgentsPrefs(), []);
    const [agentList, setAgentList] = useState<agents.AgentInfo[]>([]);
    const [sessions, setSessions] = useState<agents.SessionInfo[]>([]);
    const [activeId, setActiveId] = useState<string | null>(null);
    const [loading, setLoading] = useState(true);
    const [starting, setStarting] = useState(false);
    const [error, setError] = useState('');
    const [selectedAgent, setSelectedAgent] = useState<string>(savedPrefs.agentId || 'claude');
    const [cwd, setCwd] = useState(savedPrefs.cwd ?? '');
    const [ephemeral, setEphemeral] = useState(savedPrefs.ephemeral ?? true);
    const appliedDefaultCwd = useRef(savedPrefs.cwd !== undefined);

    const selected = useMemo(
        () => agentList.find((a) => a.id === selectedAgent) ?? null,
        [agentList, selectedAgent],
    );

    const refreshAgents = useCallback(async () => {
        setLoading(true);
        setError('');
        try {
            const list = await ListAgents();
            setAgentList(list ?? []);
            setSelectedAgent((cur) => {
                // Keep the user's last choice even if temporarily missing from PATH.
                if (list?.some((a) => a.id === cur)) return cur;
                const preferred = loadAgentsPrefs().agentId;
                if (preferred && list?.some((a) => a.id === preferred)) return preferred;
                const first = list?.find((a) => a.installed);
                return first?.id ?? cur;
            });
        } catch (e: any) {
            setError(typeof e === 'string' ? e : e?.message || 'Failed to list agents');
        } finally {
            setLoading(false);
        }
    }, []);

    const refreshSessions = useCallback(async () => {
        try {
            const list = await ListAgentSessions();
            setSessions(list ?? []);
            setActiveId((cur) => {
                if (cur && list?.some((s) => s.id === cur)) return cur;
                return list?.[0]?.id ?? null;
            });
        } catch {
            /* ignore */
        }
    }, []);

    useEffect(() => {
        refreshAgents();
        refreshSessions();
    }, [refreshAgents, refreshSessions]);

    useEffect(() => {
        const unsub = EventsOn('agent:session:exit', () => {
            refreshSessions();
        });
        return unsub;
    }, [refreshSessions]);

    // Only auto-pick the sole project path when the user has never saved a cwd.
    useEffect(() => {
        if (appliedDefaultCwd.current) return;
        if (cwd) {
            appliedDefaultCwd.current = true;
            return;
        }
        if (projects.length === 1 && projects[0].path) {
            setCwd(projects[0].path);
            appliedDefaultCwd.current = true;
        }
    }, [projects, cwd]);

    useEffect(() => {
        saveAgentsPrefs({agentId: selectedAgent});
    }, [selectedAgent]);

    useEffect(() => {
        saveAgentsPrefs({cwd});
    }, [cwd]);

    useEffect(() => {
        saveAgentsPrefs({ephemeral});
    }, [ephemeral]);

    const showEphemeralToggle = !!selected?.supportsEphemeral;
    const activeSession = useMemo(
        () => sessions.find((s) => s.id === activeId) ?? null,
        [sessions, activeId],
    );

    const handleStart = async () => {
        if (!selected?.installed) return;
        setStarting(true);
        setError('');
        try {
            const info = await StartAgentSession(
                agents.StartSessionRequest.createFrom({
                    agentId: selected.id,
                    cwd: cwd.trim(),
                    ephemeral: showEphemeralToggle ? ephemeral : false,
                    cols: 120,
                    rows: 36,
                }),
            );
            setSessions((prev) => {
                const next = prev.filter((s) => s.id !== info.id);
                return [info, ...next];
            });
            setActiveId(info.id);
            // Soft refresh — don't flip the toolbar into skeleton / loading.
            ListAgents()
                .then((list) => setAgentList(list ?? []))
                .catch(() => undefined);
        } catch (e: any) {
            setError(typeof e === 'string' ? e : e?.message || 'Failed to start agent');
        } finally {
            setStarting(false);
        }
    };

    const handleClose = async (id: string) => {
        try {
            await StopAgentSession(id);
        } catch {
            /* already gone */
        }
        disposeAgentTerminal(id);
        setSessions((prev) => prev.filter((s) => s.id !== id));
        setActiveId((cur) => (cur === id ? null : cur));
    };

    return (
        <div className="agents-view">
            <div className="agents-layout">
                <PageHeader
                    title="Agents"
                    description="Run Claude, Codex, and other CLIs here. Draft MCP is attached per session or installed into the agent config."
                    action={
                        <button type="button" className="btn btn-ghost" onClick={refreshAgents} disabled={loading}>
                            <RefreshCw size={14} strokeWidth={2} className={loading ? 'agents-spin' : undefined} />
                            Rescan
                        </button>
                    }
                />

                {error && <div className="agents-error">{error}</div>}

                <div className={'agents-start panel' + (loading && agentList.length === 0 ? ' agents-start--loading' : '')} aria-busy={loading && agentList.length === 0}>
                    {loading && agentList.length === 0 ? (
                        <>
                            <div className="agents-toolbar agents-toolbar--skeleton" aria-hidden="true">
                                <div className="agents-field">
                                    <span className="agents-skel agents-skel--label" />
                                    <span className="agents-skel agents-skel--control" />
                                </div>
                                <div className="agents-field">
                                    <span className="agents-skel agents-skel--label" />
                                    <span className="agents-skel agents-skel--control" />
                                </div>
                                <div className="agents-field agents-field--path">
                                    <span className="agents-skel agents-skel--label" />
                                    <div className="agents-skel-path">
                                        <span className="agents-skel agents-skel--control agents-skel--grow" />
                                        <span className="agents-skel agents-skel--icon" />
                                    </div>
                                </div>
                                <div className="agents-toolbar-end">
                                    <span className="agents-skel agents-skel--btn" />
                                </div>
                            </div>
                            <div className="agents-options" aria-hidden="true">
                                <span className="agents-skel agents-skel--hint" />
                            </div>
                        </>
                    ) : (
                        <>
                    <div className="agents-toolbar">
                        <label className="agents-field agents-field--agent">
                            <span>Agent</span>
                            <select
                                className="input select-styled"
                                value={selectedAgent}
                                onChange={(e) => setSelectedAgent(e.target.value)}
                                disabled={starting}
                            >
                                {agentList.map((a) => (
                                    <option key={a.id} value={a.id} disabled={!a.installed}>
                                        {a.name}
                                        {!a.installed ? ' (not found)' : a.version ? ` — ${a.version}` : ''}
                                    </option>
                                ))}
                            </select>
                        </label>

                        <label className="agents-field agents-field--project">
                            <span>Project</span>
                            <select
                                className="input select-styled"
                                value={projects.some((p) => p.path === cwd) ? cwd : ''}
                                onChange={(e) => {
                                    if (e.target.value) setCwd(e.target.value);
                                }}
                                disabled={starting}
                            >
                                <option value="">Custom path</option>
                                {projects.map((p) => (
                                    <option key={p.id} value={p.path}>
                                        {p.name}
                                    </option>
                                ))}
                            </select>
                        </label>

                        <label className="agents-field agents-field--path">
                            <span>Working directory</span>
                            <div className="input-with-action">
                                <input
                                    className="input"
                                    type="text"
                                    value={cwd}
                                    onChange={(e) => setCwd(e.target.value)}
                                    placeholder="Folder for the agent session"
                                    disabled={starting}
                                />
                                <button
                                    type="button"
                                    className="btn btn-ghost input-action-btn"
                                    title="Browse for folder"
                                    disabled={starting}
                                    onClick={() => {
                                        SelectFolder()
                                            .then((p) => {
                                                if (p) setCwd(p);
                                            })
                                            .catch(() => undefined);
                                    }}
                                >
                                    <FolderOpen size={15} strokeWidth={2} />
                                </button>
                            </div>
                        </label>

                        <div className="agents-toolbar-end">
                            <button
                                type="button"
                                className="btn btn-primary"
                                onClick={handleStart}
                                disabled={starting || !selected?.installed}
                            >
                                <Plus size={14} strokeWidth={2} />
                                {starting ? 'Starting…' : 'Start'}
                            </button>
                        </div>
                    </div>

                    <div className="agents-options">
                        {showEphemeralToggle ? (
                            <label className="agents-ephemeral">
                                <input
                                    type="checkbox"
                                    checked={ephemeral}
                                    onChange={(e) => setEphemeral(e.target.checked)}
                                    disabled={starting}
                                />
                                <span>
                                    Session-only Draft MCP
                                    {!ephemeral
                                        ? selected?.mcpConfigured
                                            ? ' — using existing config'
                                            : ' — will add to user config'
                                        : selected?.id === 'claude'
                                          ? ' — inline --mcp-config'
                                          : selected?.id === 'codex'
                                            ? ' — -c overrides'
                                            : ''}
                                </span>
                            </label>
                        ) : selected?.installed ? (
                            <span className="agents-hint">
                                {selected.mcpConfigured
                                    ? 'Draft MCP already configured'
                                    : 'Draft MCP will be added to this agent’s config on start'}
                            </span>
                        ) : null}

                        {selected && !selected.installed && (
                            <p className="agents-missing">
                                <SquareTerminal size={14} /> {selected.name} not found on PATH — install it, then Rescan.
                            </p>
                        )}
                    </div>
                        </>
                    )}
                </div>

                <div className="agents-sessions panel">
                    <div className="agents-tabs">
                        {sessions.length === 0 ? (
                            <div className="agents-tabs-empty">No sessions yet — start an agent above.</div>
                        ) : (
                            <>
                                <div className="agents-tabs-list">
                                    {sessions.map((s) => {
                                        const exited = s.status === 'exited' || s.status === 'error';
                                        return (
                                            <button
                                                key={s.id}
                                                type="button"
                                                className={
                                                    'agents-tab' +
                                                    (s.id === activeId ? ' active' : '') +
                                                    (exited ? ' agents-tab--exited' : '')
                                                }
                                                onClick={() => setActiveId(s.id)}
                                                title={[s.agentName, s.cwd, s.command].filter(Boolean).join('\n')}
                                            >
                                                <span className={'agents-tab-dot' + (exited ? ' agents-tab-dot--off' : '')} />
                                                <span className="agents-tab-label">{s.agentName}</span>
                                                {s.ephemeral && <span className="agents-tab-chip">temp</span>}
                                                {exited && <span className="agents-tab-chip agents-tab-chip--muted">exited</span>}
                                                <span
                                                    className="agents-tab-close"
                                                    role="button"
                                                    tabIndex={0}
                                                    title="Close session"
                                                    onClick={(e) => {
                                                        e.stopPropagation();
                                                        handleClose(s.id);
                                                    }}
                                                    onKeyDown={(e) => {
                                                        if (e.key === 'Enter' || e.key === ' ') {
                                                            e.stopPropagation();
                                                            handleClose(s.id);
                                                        }
                                                    }}
                                                >
                                                    <X size={11} strokeWidth={2.25} />
                                                </span>
                                            </button>
                                        );
                                    })}
                                </div>
                                {activeSession && (
                                    <div className="agents-tabs-meta">
                                        {activeSession.cwd ? (
                                            <span className="agents-tabs-cwd" title={activeSession.cwd}>
                                                {activeSession.cwd}
                                            </span>
                                        ) : null}
                                        {(activeSession.status === 'running' || activeSession.status === 'starting') && (
                                            <button
                                                type="button"
                                                className="btn btn-ghost agents-tabs-stop"
                                                onClick={() => handleClose(activeSession.id)}
                                                title="Stop session"
                                            >
                                                <Square size={11} strokeWidth={2.25} />
                                                Stop
                                            </button>
                                        )}
                                    </div>
                                )}
                            </>
                        )}
                    </div>

                    <div className="agents-term-wrap">
                        {sessions.length === 0 ? (
                            <div className="agents-term-placeholder">
                                <SquareTerminal size={22} strokeWidth={1.5} />
                                <p>Your agent TUI runs here with Draft MCP available.</p>
                            </div>
                        ) : (
                            sessions.map((s) => (
                                <AgentSessionTerminal key={s.id} sessionId={s.id} active={s.id === activeId} />
                            ))
                        )}
                    </div>
                </div>
            </div>
        </div>
    );
}
