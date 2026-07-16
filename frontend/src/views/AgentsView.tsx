import {useCallback, useEffect, useMemo, useRef, useState} from 'react';
import {Bot, FolderOpen, Plus, RefreshCw, Square, X} from 'lucide-react';
import {Terminal as XTerm} from '@xterm/xterm';
import {FitAddon} from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';
import {
    ListAgents,
    ListAgentSessions,
    ResizeAgentSession,
    StartAgentSession,
    StopAgentSession,
    WriteAgentSession,
} from '../../wailsjs/go/main/App';
import {EventsOn} from '../../wailsjs/runtime/runtime';
import {agents, store} from '../../wailsjs/go/models';
import PageHeader from '../components/PageHeader';
import './WorkspaceViews.css';
import './AgentsView.css';

type AgentsViewProps = {
    projects: store.Project[];
};

type OutputPayload = {sessionId: string; data: string};
type ExitPayload = {sessionId: string; exitCode: number; error?: string};

function b64ToUint8(b64: string): Uint8Array {
    const bin = atob(b64);
    const out = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
    return out;
}

function uint8ToB64(bytes: Uint8Array): string {
    let s = '';
    for (let i = 0; i < bytes.length; i++) s += String.fromCharCode(bytes[i]);
    return btoa(s);
}

function AgentSessionTerminal({
    session,
    active,
}: {
    session: agents.SessionInfo;
    active: boolean;
}) {
    const hostRef = useRef<HTMLDivElement>(null);
    const termRef = useRef<XTerm | null>(null);
    const fitRef = useRef<FitAddon | null>(null);
    const sessionId = session.id;

    useEffect(() => {
        if (!hostRef.current) return;
        const term = new XTerm({
            convertEol: true,
            fontSize: 13,
            fontFamily: 'Menlo, Consolas, "DejaVu Sans Mono", monospace',
            cursorBlink: true,
            scrollback: 8000,
        });
        const fit = new FitAddon();
        term.loadAddon(fit);
        term.open(hostRef.current);
        termRef.current = term;
        fitRef.current = fit;
        try {
            fit.fit();
        } catch {
            /* not laid out */
        }

        const dataDisp = term.onData((d) => {
            const bytes = new TextEncoder().encode(d);
            WriteAgentSession(sessionId, uint8ToB64(bytes)).catch(() => undefined);
        });
        const resizeDisp = term.onResize(({cols, rows}) => {
            ResizeAgentSession(sessionId, cols, rows).catch(() => undefined);
        });

        // Initial resize once fitted.
        try {
            const dims = fit.proposeDimensions();
            if (dims) ResizeAgentSession(sessionId, dims.cols, dims.rows).catch(() => undefined);
        } catch {
            /* ignore */
        }

        const unsubOut = EventsOn('agent:session:output', (payload: OutputPayload) => {
            if (!payload || payload.sessionId !== sessionId) return;
            try {
                term.write(b64ToUint8(payload.data));
            } catch {
                /* ignore decode errors */
            }
        });
        const unsubExit = EventsOn('agent:session:exit', (payload: ExitPayload) => {
            if (!payload || payload.sessionId !== sessionId) return;
            const code = payload.exitCode ?? 0;
            const err = payload.error ? ` (${payload.error})` : '';
            term.write(`\r\n\r\n[session exited: ${code}]${err}\r\n`);
        });

        const ro = new ResizeObserver(() => {
            try {
                fit.fit();
            } catch {
                /* ignore */
            }
        });
        ro.observe(hostRef.current);

        return () => {
            dataDisp.dispose();
            resizeDisp.dispose();
            unsubOut();
            unsubExit();
            ro.disconnect();
            term.dispose();
            termRef.current = null;
            fitRef.current = null;
        };
    }, [sessionId]);

    useEffect(() => {
        if (!active) return;
        try {
            fitRef.current?.fit();
            termRef.current?.focus();
        } catch {
            /* ignore */
        }
    }, [active]);

    return <div className="agents-term-host" ref={hostRef} hidden={!active} />;
}

export default function AgentsView({projects}: AgentsViewProps) {
    const [agentList, setAgentList] = useState<agents.AgentInfo[]>([]);
    const [sessions, setSessions] = useState<agents.SessionInfo[]>([]);
    const [activeId, setActiveId] = useState<string | null>(null);
    const [loading, setLoading] = useState(true);
    const [starting, setStarting] = useState(false);
    const [error, setError] = useState('');
    const [selectedAgent, setSelectedAgent] = useState<string>('claude');
    const [cwd, setCwd] = useState('');
    const [ephemeral, setEphemeral] = useState(true);

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
                if (list?.some((a) => a.id === cur && a.installed)) return cur;
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

    useEffect(() => {
        if (!cwd && projects.length === 1) {
            setCwd(projects[0].path || '');
        }
    }, [projects, cwd]);

    const showEphemeralToggle = !!selected?.supportsEphemeral;

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
            await refreshAgents();
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
        setSessions((prev) => prev.filter((s) => s.id !== id));
        setActiveId((cur) => (cur === id ? null : cur));
    };

    return (
        <div className="agents-view">
            <div className="agents-layout">
                <PageHeader
                    title="Agents"
                    description="Run your own coding agents inside Draft. Draft MCP is wired per session (ephemeral for Claude/Codex when enabled) or installed into the agent config when needed."
                    action={
                        <button type="button" className="btn btn-ghost" onClick={refreshAgents} disabled={loading}>
                            <RefreshCw size={14} strokeWidth={2} />
                            Rescan
                        </button>
                    }
                />

                {error && <div className="agents-error">{error}</div>}

                <div className="agents-start panel">
                    <div className="agents-start-row">
                        <label className="agents-field">
                            <span>Agent</span>
                            <select
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

                        <label className="agents-field agents-field--grow">
                            <span>Working directory</span>
                            <div className="agents-cwd">
                                <select
                                    value={projects.some((p) => p.path === cwd) ? cwd : ''}
                                    onChange={(e) => {
                                        if (e.target.value) setCwd(e.target.value);
                                    }}
                                    disabled={starting}
                                >
                                    <option value="">Custom / none</option>
                                    {projects.map((p) => (
                                        <option key={p.id} value={p.path}>
                                            {p.name}
                                        </option>
                                    ))}
                                </select>
                                <input
                                    type="text"
                                    value={cwd}
                                    onChange={(e) => setCwd(e.target.value)}
                                    placeholder="Project path"
                                    disabled={starting}
                                />
                            </div>
                        </label>
                    </div>

                    <div className="agents-start-row agents-start-row--actions">
                        {showEphemeralToggle && (
                            <label className="agents-ephemeral">
                                <input
                                    type="checkbox"
                                    checked={ephemeral}
                                    onChange={(e) => setEphemeral(e.target.checked)}
                                    disabled={starting}
                                />
                                <span>
                                    Session-only Draft MCP
                                    {selected?.id === 'claude'
                                        ? ' (inline --mcp-config, no disk write)'
                                        : selected?.id === 'codex'
                                          ? ' (-c mcp_servers.draft…)'
                                          : ''}
                                </span>
                            </label>
                        )}
                        {!showEphemeralToggle && selected?.installed && (
                            <span className="agents-hint">
                                {selected.mcpConfigured
                                    ? 'Draft MCP already configured for this agent'
                                    : 'Draft MCP will be added to this agent’s config, then the session starts'}
                            </span>
                        )}
                        {showEphemeralToggle && !ephemeral && (
                            <span className="agents-hint">
                                {selected?.mcpConfigured
                                    ? 'Using existing Draft MCP config'
                                    : 'Will add Draft MCP to user config, then start'}
                            </span>
                        )}

                        <button
                            type="button"
                            className="btn btn-primary"
                            onClick={handleStart}
                            disabled={starting || !selected?.installed}
                        >
                            <Plus size={14} strokeWidth={2} />
                            {starting ? 'Starting…' : 'Start session'}
                        </button>
                    </div>

                    {selected && !selected.installed && (
                        <p className="agents-missing">
                            <Bot size={14} /> {selected.name} was not found on PATH. Install the CLI and click Rescan
                            (no admin required — Draft only looks at user PATH).
                        </p>
                    )}
                </div>

                <div className="agents-sessions panel">
                    <div className="agents-tabs">
                        {sessions.length === 0 ? (
                            <div className="agents-tabs-empty">No sessions yet — start an agent above.</div>
                        ) : (
                            sessions.map((s) => (
                                <button
                                    key={s.id}
                                    type="button"
                                    className={'agents-tab' + (s.id === activeId ? ' active' : '')}
                                    onClick={() => setActiveId(s.id)}
                                >
                                    <span className="agents-tab-label">
                                        {s.agentName}
                                        {s.ephemeral ? ' · temp MCP' : ''}
                                        {s.status === 'exited' || s.status === 'error' ? ' · exited' : ''}
                                    </span>
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
                                        <X size={12} />
                                    </span>
                                </button>
                            ))
                        )}
                    </div>

                    <div className="agents-term-wrap">
                        {sessions.length === 0 ? (
                            <div className="agents-term-placeholder">
                                <FolderOpen size={28} strokeWidth={1.5} />
                                <p>Your agent TUI runs here with Draft MCP available.</p>
                            </div>
                        ) : (
                            sessions.map((s) => (
                                <AgentSessionTerminal key={s.id} session={s} active={s.id === activeId} />
                            ))
                        )}
                    </div>

                    {activeId && (
                        <div className="agents-session-meta">
                            {(() => {
                                const s = sessions.find((x) => x.id === activeId);
                                if (!s) return null;
                                return (
                                    <>
                                        <code>{s.command || s.agentName}</code>
                                        {s.cwd ? <span className="agents-meta-cwd">{s.cwd}</span> : null}
                                        {(s.status === 'running' || s.status === 'starting') && (
                                            <button
                                                type="button"
                                                className="btn btn-ghost btn-sm"
                                                onClick={() => handleClose(s.id)}
                                            >
                                                <Square size={12} />
                                                Stop
                                            </button>
                                        )}
                                    </>
                                );
                            })()}
                        </div>
                    )}
                </div>
            </div>
        </div>
    );
}
