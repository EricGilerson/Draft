import {useCallback, useEffect, useMemo, useRef, useState, type WheelEvent} from 'react';
import {
    ChevronDown,
    ChevronUp,
    FolderOpen,
    Plus,
    RefreshCw,
    Square,
    SquareTerminal,
    X,
} from 'lucide-react';
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
import {Skeleton} from '../components/Skeleton';
import {attachAgentTerminal, disposeAgentTerminal, focusAgentTerminal} from '../lib/agentTerminalHost';
import {applySessionTabOrder, loadAgentsPrefs, saveAgentsPrefs} from '../lib/agentsPrefs';
import './WorkspaceViews.css';
import './AgentsView.css';

type AgentsViewProps = {
    projects: store.Project[];
};

type LauncherFieldsProps = {
    agentList: agents.AgentInfo[];
    projects: store.Project[];
    selectedAgent: string;
    selected: agents.AgentInfo | null;
    cwd: string;
    plain: boolean;
    ephemeral: boolean;
    showEphemeralToggle: boolean;
    starting: boolean;
    compact?: boolean;
    onAgentChange: (id: string) => void;
    onCwdChange: (cwd: string) => void;
    onPlainChange: (plain: boolean) => void;
    onEphemeralChange: (ephemeral: boolean) => void;
    onStart: () => void;
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

function AgentsLauncherFields({
    agentList,
    projects,
    selectedAgent,
    selected,
    cwd,
    plain,
    ephemeral,
    showEphemeralToggle,
    starting,
    compact = false,
    onAgentChange,
    onCwdChange,
    onPlainChange,
    onEphemeralChange,
    onStart,
}: LauncherFieldsProps) {
    const rootClass = compact ? 'agents-launcher-fields agents-launcher-fields--compact' : 'agents-launcher-fields';

    return (
        <div className={rootClass}>
            <div className={compact ? 'agents-popover-fields' : 'agents-toolbar'}>
                <label className={'agents-field' + (compact ? '' : ' agents-field--agent')}>
                    <span>Agent</span>
                    <select
                        className="input select-styled"
                        value={selectedAgent}
                        onChange={(e) => onAgentChange(e.target.value)}
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

                <label className={'agents-field' + (compact ? '' : ' agents-field--project')}>
                    <span>Project</span>
                    <select
                        className="input select-styled"
                        value={projects.some((p) => p.path === cwd) ? cwd : ''}
                        onChange={(e) => {
                            if (e.target.value) onCwdChange(e.target.value);
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

                <label className={'agents-field' + (compact ? '' : ' agents-field--path')}>
                    <span>Working directory</span>
                    <div className="input-with-action">
                        <input
                            className="input"
                            type="text"
                            value={cwd}
                            onChange={(e) => onCwdChange(e.target.value)}
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
                                        if (p) onCwdChange(p);
                                    })
                                    .catch(() => undefined);
                            }}
                        >
                            <FolderOpen size={compact ? 14 : 15} strokeWidth={2} />
                        </button>
                    </div>
                </label>

                {!compact ? (
                    <div className="agents-toolbar-end">
                        <button
                            type="button"
                            className="btn btn-primary"
                            onClick={onStart}
                            disabled={starting || !selected?.installed}
                        >
                            <Plus size={14} strokeWidth={2} />
                            {starting ? 'Starting…' : 'Start'}
                        </button>
                    </div>
                ) : null}
            </div>

            <div className="agents-options">
                <label className="agents-ephemeral">
                    <input
                        type="checkbox"
                        checked={plain}
                        onChange={(e) => onPlainChange(e.target.checked)}
                        disabled={starting}
                    />
                    <span>
                        Plain session
                        {!compact ? (
                            <span className="agents-option-detail">
                                {' '}
                                — no Draft MCP inject/install (normal terminal)
                            </span>
                        ) : null}
                    </span>
                </label>

                {!plain && showEphemeralToggle ? (
                    <label className="agents-ephemeral">
                        <input
                            type="checkbox"
                            checked={ephemeral}
                            onChange={(e) => onEphemeralChange(e.target.checked)}
                            disabled={starting}
                        />
                        <span>
                            Session-only Draft MCP
                            {!compact ? (
                                <span className="agents-option-detail">
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
                            ) : null}
                        </span>
                    </label>
                ) : null}

                {!compact && !plain && !showEphemeralToggle && selected?.installed ? (
                    <span className="agents-hint">
                        {selected.mcpConfigured
                            ? 'Draft MCP already configured'
                            : 'Draft MCP will be added to this agent’s config on start'}
                    </span>
                ) : null}

                {!compact && plain && selected?.installed ? (
                    <span className="agents-hint">
                        Starts {selected.name} as-is
                        {selected.mcpConfigured ? ' (any MCP already in its config may still load)' : ''}
                    </span>
                ) : null}

                {selected && !selected.installed && (
                    <p className="agents-missing">
                        <SquareTerminal size={14} /> {selected.name} not found on PATH
                        {compact ? '' : ' — install it, then Rescan'}.
                    </p>
                )}
            </div>

            {compact ? (
                <button
                    type="button"
                    className="btn btn-primary agents-popover-start"
                    onClick={onStart}
                    disabled={starting || !selected?.installed}
                >
                    <Plus size={14} strokeWidth={2} />
                    {starting ? 'Starting…' : 'Start agent'}
                </button>
            ) : null}
        </div>
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
    const [plain, setPlain] = useState(savedPrefs.plain ?? false);
    // Prefer collapsed chrome once the user has sessions / last chose focus mode.
    const [launcherCollapsed, setLauncherCollapsed] = useState(savedPrefs.launcherCollapsed ?? false);
    const [newTabOpen, setNewTabOpen] = useState(false);
    const [dragTabId, setDragTabId] = useState<string | null>(null);
    const [dropTabId, setDropTabId] = useState<string | null>(null);
    const appliedDefaultCwd = useRef(savedPrefs.cwd !== undefined);
    const tabsListRef = useRef<HTMLDivElement>(null);
    const newTabRef = useRef<HTMLDivElement>(null);

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
            const ordered = applySessionTabOrder(list ?? [], loadAgentsPrefs().tabOrder);
            setSessions(ordered);
            setActiveId((cur) => {
                if (cur && ordered.some((s) => s.id === cur)) return cur;
                return ordered[0]?.id ?? null;
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

    useEffect(() => {
        saveAgentsPrefs({plain});
    }, [plain]);

    useEffect(() => {
        saveAgentsPrefs({launcherCollapsed});
    }, [launcherCollapsed]);

    useEffect(() => {
        if (!newTabOpen) return;
        const onPointerDown = (e: MouseEvent) => {
            const el = newTabRef.current;
            if (el && !el.contains(e.target as Node)) {
                setNewTabOpen(false);
            }
        };
        const onKeyDown = (e: KeyboardEvent) => {
            if (e.key === 'Escape') setNewTabOpen(false);
        };
        document.addEventListener('mousedown', onPointerDown);
        document.addEventListener('keydown', onKeyDown);
        return () => {
            document.removeEventListener('mousedown', onPointerDown);
            document.removeEventListener('keydown', onKeyDown);
        };
    }, [newTabOpen]);

    const showEphemeralToggle = !plain && !!selected?.supportsEphemeral;
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
                    plain,
                    cols: 120,
                    rows: 36,
                }),
            );
            setSessions((prev) => {
                const next = prev.filter((s) => s.id !== info.id);
                const ordered = [info, ...next];
                saveAgentsPrefs({tabOrder: ordered.map((s) => s.id)});
                return ordered;
            });
            setActiveId(info.id);
            setLauncherCollapsed(true);
            setNewTabOpen(false);
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

    const launcherFieldProps: Omit<LauncherFieldsProps, 'compact'> = {
        agentList,
        projects,
        selectedAgent,
        selected,
        cwd,
        plain,
        ephemeral,
        showEphemeralToggle,
        starting,
        onAgentChange: setSelectedAgent,
        onCwdChange: setCwd,
        onPlainChange: setPlain,
        onEphemeralChange: setEphemeral,
        onStart: handleStart,
    };

    const handleClose = async (id: string) => {
        try {
            await StopAgentSession(id);
        } catch {
            /* already gone */
        }
        disposeAgentTerminal(id);
        setSessions((prev) => {
            const next = prev.filter((s) => s.id !== id);
            saveAgentsPrefs({tabOrder: next.map((s) => s.id)});
            return next;
        });
        setActiveId((cur) => (cur === id ? null : cur));
    };

    const moveSessionTab = (fromId: string, toId: string) => {
        if (fromId === toId) return;
        setSessions((prev) => {
            const from = prev.findIndex((s) => s.id === fromId);
            if (from < 0) return prev;
            const next = [...prev];
            const [item] = next.splice(from, 1);
            const insertAt = next.findIndex((s) => s.id === toId);
            if (insertAt < 0) return prev;
            next.splice(insertAt, 0, item);
            saveAgentsPrefs({tabOrder: next.map((s) => s.id)});
            return next;
        });
    };

    const onTabsWheel = (e: WheelEvent<HTMLDivElement>) => {
        const el = tabsListRef.current;
        if (!el) return;
        // Prefer horizontal scroll for overflow tabs (trackpad / shift+wheel / vertical wheel).
        if (el.scrollWidth <= el.clientWidth) return;
        const delta = Math.abs(e.deltaX) > Math.abs(e.deltaY) ? e.deltaX : e.deltaY;
        if (delta === 0) return;
        e.preventDefault();
        el.scrollLeft += delta;
    };

    return (
        <div className={'agents-view' + (launcherCollapsed ? ' agents-view--focus' : '')}>
            <div className="agents-layout">
                {launcherCollapsed ? (
                    <div className="agents-focus-bar">
                        <button
                            type="button"
                            className="btn btn-ghost agents-focus-toggle"
                            onClick={() => setLauncherCollapsed(false)}
                            title="Show launcher"
                        >
                            <ChevronDown size={14} strokeWidth={2} />
                            Launcher
                        </button>
                        <div className="agents-focus-bar-spacer" />
                        <button
                            type="button"
                            className="btn btn-ghost btn-sm"
                            onClick={refreshAgents}
                            disabled={loading}
                            title="Rescan agents"
                        >
                            <RefreshCw size={13} strokeWidth={2} className={loading ? 'agents-spin' : undefined} />
                        </button>
                    </div>
                ) : (
                    <>
                        <PageHeader
                            title="Agents"
                            description="Run Claude, Codex, and other CLIs here. Draft MCP is attached per session or installed into the agent config."
                            action={
                                <div className="agents-header-actions">
                                    {sessions.length > 0 ? (
                                        <button
                                            type="button"
                                            className="btn btn-ghost"
                                            onClick={() => setLauncherCollapsed(true)}
                                            title="Collapse launcher for more terminal space"
                                        >
                                            <ChevronUp size={14} strokeWidth={2} />
                                            Focus
                                        </button>
                                    ) : null}
                                    <button
                                        type="button"
                                        className="btn btn-ghost"
                                        onClick={refreshAgents}
                                        disabled={loading}
                                    >
                                        <RefreshCw
                                            size={14}
                                            strokeWidth={2}
                                            className={loading ? 'agents-spin' : undefined}
                                        />
                                        Rescan
                                    </button>
                                </div>
                            }
                        />

                        {error && <div className="agents-error">{error}</div>}

                        <div
                            className={
                                'agents-start panel' +
                                (loading && agentList.length === 0 ? ' agents-start--loading' : '')
                            }
                            aria-busy={loading && agentList.length === 0}
                        >
                    {loading && agentList.length === 0 ? (
                        <>
                            <div className="agents-toolbar agents-toolbar--skeleton" aria-hidden="true">
                                <div className="agents-field">
                                    <Skeleton width={52} height={10} style={{marginBottom: 5}} />
                                    <Skeleton height={36} />
                                </div>
                                <div className="agents-field">
                                    <Skeleton width={52} height={10} style={{marginBottom: 5}} />
                                    <Skeleton height={36} />
                                </div>
                                <div className="agents-field agents-field--path">
                                    <Skeleton width={52} height={10} style={{marginBottom: 5}} />
                                    <div className="agents-skel-path">
                                        <Skeleton className="skel-grow" height={36} />
                                        <Skeleton width={36} height={36} />
                                    </div>
                                </div>
                                <div className="agents-toolbar-end">
                                    <Skeleton width={88} height={36} />
                                </div>
                            </div>
                            <div className="agents-options" aria-hidden="true">
                                <Skeleton width="min(280px, 55%)" height={12} />
                            </div>
                        </>
                    ) : (
                        <AgentsLauncherFields {...launcherFieldProps} />
                    )}
                </div>
                    </>
                )}

                {launcherCollapsed && error ? <div className="agents-error agents-error--focus">{error}</div> : null}

                <div className="agents-sessions panel">
                    <div className="agents-tabs">
                        {sessions.length === 0 ? (
                            <div className="agents-tabs-empty">
                                {launcherCollapsed
                                    ? 'No sessions yet — use + to start an agent.'
                                    : 'No sessions yet — start an agent above.'}
                            </div>
                        ) : (
                            <div
                                className="agents-tabs-list"
                                ref={tabsListRef}
                                onWheel={onTabsWheel}
                            >
                                {sessions.map((s) => {
                                    const exited = s.status === 'exited' || s.status === 'error';
                                    const restarting = s.status === 'restarting';
                                    const dragging = dragTabId === s.id;
                                    const dropTarget = dropTabId === s.id && dragTabId !== s.id;
                                    return (
                                        <button
                                            key={s.id}
                                            type="button"
                                            draggable
                                            className={
                                                'agents-tab' +
                                                (s.id === activeId ? ' active' : '') +
                                                (exited ? ' agents-tab--exited' : '') +
                                                (dragging ? ' agents-tab--dragging' : '') +
                                                (dropTarget ? ' agents-tab--drop' : '')
                                            }
                                            onClick={() => setActiveId(s.id)}
                                            onDragStart={(e) => {
                                                e.dataTransfer.effectAllowed = 'move';
                                                e.dataTransfer.setData('text/plain', s.id);
                                                setDragTabId(s.id);
                                                setDropTabId(null);
                                            }}
                                            onDragEnd={() => {
                                                setDragTabId(null);
                                                setDropTabId(null);
                                            }}
                                            onDragOver={(e) => {
                                                e.preventDefault();
                                                e.dataTransfer.dropEffect = 'move';
                                                if (dragTabId && dragTabId !== s.id) {
                                                    setDropTabId(s.id);
                                                }
                                            }}
                                            onDragLeave={() => {
                                                setDropTabId((cur) => (cur === s.id ? null : cur));
                                            }}
                                            onDrop={(e) => {
                                                e.preventDefault();
                                                const fromId =
                                                    e.dataTransfer.getData('text/plain') || dragTabId;
                                                if (fromId) moveSessionTab(fromId, s.id);
                                                setDragTabId(null);
                                                setDropTabId(null);
                                            }}
                                            title={[s.agentName, s.cwd, s.command].filter(Boolean).join('\n')}
                                        >
                                            <span className={'agents-tab-dot' + (exited ? ' agents-tab-dot--off' : '')} />
                                            <span className="agents-tab-label">{s.agentName}</span>
                                            {s.plain && <span className="agents-tab-chip">plain</span>}
                                            {!s.plain && s.ephemeral && <span className="agents-tab-chip">temp</span>}
                                            {restarting && <span className="agents-tab-chip">update</span>}
                                            {exited && <span className="agents-tab-chip agents-tab-chip--muted">exited</span>}
                                            <span
                                                className="agents-tab-close"
                                                role="button"
                                                tabIndex={0}
                                                draggable={false}
                                                title="Close session"
                                                onMouseDown={(e) => e.stopPropagation()}
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
                        )}

                        <div className="agents-new-tab-wrap" ref={newTabRef}>
                            <button
                                type="button"
                                className={'agents-tab-add' + (newTabOpen ? ' agents-tab-add--open' : '')}
                                onClick={() => setNewTabOpen((o) => !o)}
                                aria-expanded={newTabOpen}
                                aria-haspopup="dialog"
                                title="Start another agent"
                            >
                                <Plus size={13} strokeWidth={2.25} />
                            </button>
                            {newTabOpen ? (
                                <div className="agents-new-popover" role="dialog" aria-label="Start agent">
                                    <div className="agents-new-popover-head">
                                        <span>New session</span>
                                        <button
                                            type="button"
                                            className="btn btn-ghost agents-new-popover-close"
                                            onClick={() => setNewTabOpen(false)}
                                            title="Close"
                                        >
                                            <X size={12} strokeWidth={2.25} />
                                        </button>
                                    </div>
                                    <AgentsLauncherFields {...launcherFieldProps} compact />
                                </div>
                            ) : null}
                        </div>

                        {activeSession ? (
                            <div className="agents-tabs-meta">
                                {activeSession.cwd ? (
                                    <span className="agents-tabs-cwd" title={activeSession.cwd}>
                                        {activeSession.cwd}
                                    </span>
                                ) : null}
                                {(activeSession.status === 'running' ||
                                    activeSession.status === 'starting' ||
                                    activeSession.status === 'restarting') && (
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
                        ) : null}
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
