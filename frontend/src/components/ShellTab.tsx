import {useEffect, useMemo, useRef, useState, type KeyboardEvent} from 'react';
import {Terminal as XTerm} from '@xterm/xterm';
import {FitAddon} from '@xterm/addon-fit';
import {Eraser, Minus, Play, Plus, Square} from 'lucide-react';
import '@xterm/xterm/css/xterm.css';
import {GetEnvVars, MintShellAttach, RunCommand} from '../../wailsjs/go/main/App';
import {deploy} from '../../wailsjs/go/models';
import {useLinkedServiceTarget} from '../lib/linkedService';
import './ShellTab.css';

type ShellTabProps = {
    nodeId: string;
};

const DEFAULT_SHELL = 'bash';
const SHELLS = ['bash', 'sh', 'ash', 'zsh'];

/** Draft-injected runtime keys always present after a successful deploy. */
const DRAFT_ENV_KEYS = [
    'DRAFT_SERVICE_PORT',
    'DRAFT_INTERNAL_HOSTNAME',
    'DRAFT_INTERNAL_URL',
    'DRAFT_PUBLIC_HOSTNAME',
    'DRAFT_PUBLIC_URL',
    'DRAFT_SERVICE_NAME',
    'DRAFT_PROJECT_NAME',
    'DRAFT_ENVIRONMENT',
];

const FONT_STORAGE_KEY = 'draft:shell-font-size';
const FONT_MIN = 11;
const FONT_MAX = 20;
const FONT_DEFAULT = 13;

function readFontSize(): number {
    const raw = localStorage.getItem(FONT_STORAGE_KEY);
    const n = raw ? Number(raw) : FONT_DEFAULT;
    if (!Number.isFinite(n)) return FONT_DEFAULT;
    return Math.min(FONT_MAX, Math.max(FONT_MIN, Math.round(n)));
}

function isRuntimeScope(scope: string | undefined): boolean {
    return scope === 'runtime' || scope === 'both' || !scope;
}

// ShellTab opens a live, interactive TTY in the service's running container
// via a WebSocket to the daemon's /exec/attach endpoint, rendered with xterm.js.
// Auth uses a short-lived single-use ticket (minted over Wails/HTTP), never the
// long-lived daemon token in the WebSocket URL.
// The connection is bound to the tab's lifetime: switching tabs or unmounting
// closes the WS, which tears down the exec on the daemon side.
export default function ShellTab({nodeId}: ShellTabProps) {
    const termRef = useRef<HTMLDivElement>(null);
    const termInstanceRef = useRef<XTerm | null>(null);
    const fitRef = useRef<FitAddon | null>(null);
    const wsRef = useRef<WebSocket | null>(null);
    const [shell, setShell] = useState<string>(DEFAULT_SHELL);
    const shellRef = useRef<string>(shell);
    const [sessionKey, setSessionKey] = useState(0);
    const [status, setStatus] = useState<'connecting' | 'open' | 'closed' | 'error'>('connecting');
    const statusRef = useRef(status);
    const [error, setError] = useState<string | null>(null);
    const [fontSize, setFontSize] = useState(readFontSize);
    const fontSizeRef = useRef(fontSize);
    const {loading: linkLoading, isLinked, linkInfo, targetNodeId} = useLinkedServiceTarget(nodeId);

    // Env insert typeahead — service runtime vars + DRAFT_* keys.
    const [envKeys, setEnvKeys] = useState<string[]>([]);
    const [envQuery, setEnvQuery] = useState('');
    const [envOpen, setEnvOpen] = useState(false);
    const [envHighlight, setEnvHighlight] = useState(0);
    const envBoxRef = useRef<HTMLDivElement>(null);

    // One-shot "Run" bar state: run a command (e.g. `npm run migrate`) in the
    // running container without leaving the tab. Output is captured (not TTY'd)
    // and shown in a collapsible panel below the bar.
    const [cmd, setCmd] = useState<string>('');
    const [workDir, setWorkDir] = useState<string>('');
    const [running, setRunning] = useState(false);
    const [runOutput, setRunOutput] = useState<string>('');
    const [runExit, setRunExit] = useState<number | null>(null);
    const [showOutput, setShowOutput] = useState(false);
    const runHistoryRef = useRef<string[]>([]);

    useEffect(() => { shellRef.current = shell; }, [shell]);
    useEffect(() => { fontSizeRef.current = fontSize; }, [fontSize]);
    useEffect(() => { statusRef.current = status; }, [status]);

    useEffect(() => {
        if (linkLoading) return;
        let cancelled = false;
        GetEnvVars(targetNodeId)
            .then((vars) => {
                if (cancelled) return;
                const keys = new Set<string>(DRAFT_ENV_KEYS);
                for (const v of vars ?? []) {
                    if (v?.key && isRuntimeScope(v.scope)) keys.add(v.key);
                }
                setEnvKeys([...keys].sort((a, b) => a.localeCompare(b)));
            })
            .catch(() => {
                if (!cancelled) setEnvKeys([...DRAFT_ENV_KEYS]);
            });
        return () => { cancelled = true; };
    }, [targetNodeId, linkLoading, sessionKey]);

    const filteredEnvKeys = useMemo(() => {
        const q = envQuery.trim().replace(/^\$/, '').toLowerCase();
        if (!q) return envKeys.slice(0, 40);
        return envKeys.filter((k) => k.toLowerCase().includes(q)).slice(0, 40);
    }, [envKeys, envQuery]);

    useEffect(() => {
        setEnvHighlight(0);
    }, [envQuery, envOpen]);

    useEffect(() => {
        if (!envOpen) return;
        const onDoc = (e: MouseEvent) => {
            if (!envBoxRef.current?.contains(e.target as Node)) setEnvOpen(false);
        };
        document.addEventListener('mousedown', onDoc);
        return () => document.removeEventListener('mousedown', onDoc);
    }, [envOpen]);

    const insertIntoTerm = (text: string) => {
        const term = termInstanceRef.current;
        const ws = wsRef.current;
        if (!term || statusRef.current !== 'open') return;
        try {
            term.paste(text);
        } catch {
            if (ws && ws.readyState === WebSocket.OPEN) {
                try { ws.send(text); } catch { /* ignore */ }
            }
        }
        term.focus();
    };

    const insertEnvKey = (key: string) => {
        insertIntoTerm(`$${key}`);
        setEnvQuery('');
        setEnvOpen(false);
    };

    const clearTerminal = () => {
        termInstanceRef.current?.clear();
        termInstanceRef.current?.focus();
    };

    const bumpFont = (delta: number) => {
        setFontSize((prev) => {
            const next = Math.min(FONT_MAX, Math.max(FONT_MIN, prev + delta));
            localStorage.setItem(FONT_STORAGE_KEY, String(next));
            const term = termInstanceRef.current;
            if (term) {
                term.options.fontSize = next;
                try { fitRef.current?.fit(); } catch { /* ignore */ }
            }
            return next;
        });
    };

    // (Re)connect whenever the node, shell, or sessionKey changes. Each
    // connection owns its own xterm instance so a reconnect always starts clean.
    useEffect(() => {
        if (linkLoading) {
            setStatus('connecting');
            setError(null);
            return;
        }
        let cancelled = false;
        let term: XTerm | null = null;
        let fit: FitAddon | null = null;

        async function connect() {
            if (!termRef.current) return;
            termRef.current.innerHTML = '';
            setStatus('connecting');
            setError(null);

            term = new XTerm({
                convertEol: true,
                fontSize: fontSizeRef.current,
                fontFamily: 'Menlo, Consolas, "DejaVu Sans Mono", monospace',
                cursorBlink: true,
                scrollback: 5000,
            });
            fit = new FitAddon();
            term.loadAddon(fit);
            termInstanceRef.current = term;
            fitRef.current = fit;
            term.open(termRef.current);
            try { fit.fit(); } catch { /* not laid out yet */ }
            term.write('Connecting to service shell…\r\n');

            let addr = '';
            let ticket = '';
            try {
                const info = await MintShellAttach(targetNodeId, shellRef.current);
                addr = info.addr;
                ticket = info.ticket;
            } catch (e: any) {
                if (cancelled) return;
                const msg = typeof e === 'string' ? e : e?.message || 'daemon unavailable';
                setStatus('error');
                setError(msg);
                term.write(`\r\nDaemon unavailable: ${msg}\r\n`);
                return;
            }

            if (cancelled) return;
            const url = `ws://${addr}/exec/attach?nodeId=${encodeURIComponent(targetNodeId)}&shell=${encodeURIComponent(shellRef.current)}&ticket=${encodeURIComponent(ticket)}`;
            const ws = new WebSocket(url);
            ws.binaryType = 'arraybuffer';
            wsRef.current = ws;

            ws.onopen = () => {
                if (cancelled) return;
                setStatus('open');
                term!.clear();
                sendResize(term!, ws);
            };
            ws.onmessage = (ev) => {
                if (cancelled) return;
                const data = ev.data instanceof ArrayBuffer
                    ? new Uint8Array(ev.data)
                    : ev.data;
                term!.write(data);
            };
            ws.onerror = () => {
                if (cancelled) return;
                setStatus('error');
                term!.write('\r\n[shell connection error]\r\n');
            };
            ws.onclose = () => {
                if (cancelled) return;
                setStatus('closed');
                term!.write('\r\n[shell closed]\r\n');
            };

            const dataDisp = term.onData((d) => {
                if (ws.readyState === WebSocket.OPEN) ws.send(d);
            });
            const resizeDisp = term.onResize(() => sendResize(term!, ws));

            const hostEl = termRef.current;
            const onKey = (ev: globalThis.KeyboardEvent) => {
                if (!(ev.ctrlKey || ev.metaKey) || ev.key.toLowerCase() !== 'v') return;
                if (statusRef.current !== 'open') return;
                // Explicit paste for WebView reliability (esp. Windows).
                ev.preventDefault();
                void (async () => {
                    try {
                        const text = await navigator.clipboard.readText();
                        if (!text || !term || statusRef.current !== 'open') return;
                        try {
                            term.paste(text);
                        } catch {
                            if (ws.readyState === WebSocket.OPEN) ws.send(text);
                        }
                    } catch { /* ignore */ }
                })();
            };
            hostEl.addEventListener('keydown', onKey);

            const ro = new ResizeObserver(() => {
                try { fit!.fit(); } catch { /* ignore */ }
            });
            ro.observe(hostEl);

            cleanup = () => {
                dataDisp.dispose();
                resizeDisp.dispose();
                ro.disconnect();
                hostEl.removeEventListener('keydown', onKey);
                try { ws.close(); } catch { /* ignore */ }
            };
        }

        let cleanup: (() => void) | null = null;
        connect();

        return () => {
            cancelled = true;
            cleanup?.();
            term?.dispose();
            termInstanceRef.current = null;
            fitRef.current = null;
            wsRef.current = null;
        };
    }, [targetNodeId, shell, linkLoading, sessionKey]);

    function sendResize(term: XTerm, ws: WebSocket) {
        if (ws.readyState !== WebSocket.OPEN) return;
        try {
            ws.send(JSON.stringify({type: 'resize', cols: term.cols, rows: term.rows}));
        } catch { /* ignore */ }
    }

    const reconnect = () => setSessionKey((k) => k + 1);

    const runCommand = () => {
        const trimmed = cmd.trim();
        if (!trimmed || running) return;
        // Simple whitespace tokenizer; v1 doesn't handle quoted args.
        const argv = trimmed.split(/\s+/).filter(Boolean);
        setRunning(true);
        setRunOutput('');
        setRunExit(null);
        setShowOutput(true);
        runHistoryRef.current = [trimmed, ...runHistoryRef.current].slice(0, 20);
        RunCommand(targetNodeId, argv, workDir.trim())
            .then((res: deploy.RunCommandResult) => {
                setRunning(false);
                setRunOutput(res.output || '');
                setRunExit(res.exitCode);
                if (res.error) setRunOutput((prev) => (prev ? prev + '\n' : '') + `[error] ${res.error}`);
            })
            .catch((e: any) => {
                setRunning(false);
                const msg = typeof e === 'string' ? e : e?.message || 'run failed';
                setRunOutput(`[error] ${msg}`);
                setRunExit(null);
            });
    };

    const onEnvKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
        if (e.key === 'ArrowDown') {
            e.preventDefault();
            setEnvOpen(true);
            setEnvHighlight((i) => Math.min(i + 1, Math.max(filteredEnvKeys.length - 1, 0)));
            return;
        }
        if (e.key === 'ArrowUp') {
            e.preventDefault();
            setEnvHighlight((i) => Math.max(i - 1, 0));
            return;
        }
        if (e.key === 'Enter') {
            e.preventDefault();
            const key = filteredEnvKeys[envHighlight] ?? filteredEnvKeys[0];
            if (key) insertEnvKey(key);
            return;
        }
        if (e.key === 'Escape') {
            setEnvOpen(false);
            return;
        }
    };

    return (
        <div className="shell-tab">
            {isLinked && (
                <div className="shell-linked-banner">
                    Shell access is attached to the shared root service in <strong>{linkInfo?.rootEnvName || 'another environment'}</strong>
                    {linkInfo?.rootLabel ? ` · ${linkInfo.rootLabel}` : ''}.
                </div>
            )}
            <div className="shell-tab-toolbar">
                <label className="shell-shell-label">Shell</label>
                <select className="input select-styled shell-shell-select" value={shell} onChange={(e) => setShell(e.target.value)}>
                    {SHELLS.map((s) => <option key={s} value={s}>{s}</option>)}
                </select>
                <span className={`shell-status shell-status--${status}`}>{status}</span>

                <div className="shell-env-insert" ref={envBoxRef}>
                    <input
                        className="input shell-env-input"
                        type="text"
                        value={envQuery}
                        disabled={status !== 'open'}
                        placeholder="Insert $VAR…"
                        title="Type to find a runtime env key, then Enter to insert $KEY into the shell"
                        onChange={(e) => {
                            setEnvQuery(e.target.value);
                            setEnvOpen(true);
                        }}
                        onFocus={() => setEnvOpen(true)}
                        onKeyDown={onEnvKeyDown}
                    />
                    {envOpen && status === 'open' && filteredEnvKeys.length > 0 && (
                        <ul className="shell-env-menu" role="listbox">
                            {filteredEnvKeys.map((key, i) => (
                                <li key={key}>
                                    <button
                                        type="button"
                                        className={'shell-env-option' + (i === envHighlight ? ' shell-env-option--active' : '')}
                                        onMouseEnter={() => setEnvHighlight(i)}
                                        onClick={() => insertEnvKey(key)}
                                    >
                                        ${key}
                                    </button>
                                </li>
                            ))}
                        </ul>
                    )}
                </div>

                <div className="shell-toolbar-actions">
                    <button
                        type="button"
                        className="btn btn-ghost shell-tool-btn"
                        onClick={() => bumpFont(-1)}
                        disabled={fontSize <= FONT_MIN}
                        title="Decrease font size"
                    >
                        <Minus size={13}/>
                    </button>
                    <span className="shell-font-size" title="Terminal font size">{fontSize}</span>
                    <button
                        type="button"
                        className="btn btn-ghost shell-tool-btn"
                        onClick={() => bumpFont(1)}
                        disabled={fontSize >= FONT_MAX}
                        title="Increase font size"
                    >
                        <Plus size={13}/>
                    </button>
                    <button
                        type="button"
                        className="btn btn-ghost shell-tool-btn"
                        onClick={clearTerminal}
                        disabled={status !== 'open'}
                        title="Clear terminal"
                    >
                        <Eraser size={13}/>
                        Clear
                    </button>
                    <button
                        type="button"
                        className="btn btn-ghost shell-reconnect"
                        onClick={reconnect}
                        title="Reconnect the shell"
                        disabled={linkLoading}
                    >
                        Reconnect
                    </button>
                </div>
            </div>
            {error && <p className="form-error shell-error">{error}</p>}
            <div className="shell-runbar">
                <span className="shell-run-prompt">$</span>
                <input
                    className="input shell-run-input"
                    type="text"
                    value={cmd}
                    onChange={(e) => setCmd(e.target.value)}
                    onKeyDown={(e) => { if (e.key === 'Enter') runCommand(); }}
                    placeholder="Run a one-shot command in the container (e.g. npm run migrate)"
                    disabled={running}
                    list="shell-run-history"
                />
                <datalist id="shell-run-history">
                    {runHistoryRef.current.map((c, i) => <option key={i} value={c} />)}
                </datalist>
                <input
                    className="input shell-run-workdir"
                    type="text"
                    value={workDir}
                    onChange={(e) => setWorkDir(e.target.value)}
                    placeholder="workdir (optional)"
                    disabled={running}
                    title="Container working directory for the command (optional)"
                />
                <button
                    className="btn btn-primary shell-run-btn"
                    onClick={runCommand}
                    disabled={running || !cmd.trim()}
                    title="Run the command in the running container"
                >
                    {running ? <Square size={13}/> : <Play size={13}/>}
                    {running ? 'Running…' : 'Run'}
                </button>
                <button
                    className="btn btn-ghost shell-run-toggle"
                    onClick={() => setShowOutput((s) => !s)}
                    disabled={!runOutput && !running}
                >
                    {showOutput ? 'Hide output' : 'Show output'}
                </button>
            </div>
            {showOutput && (runOutput || running) && (
                <div className="shell-run-output">
                    {runExit !== null && (
                        <div className={`shell-run-exit ${runExit === 0 ? 'shell-run-exit--ok' : 'shell-run-exit--fail'}`}>
                            exit {runExit}
                        </div>
                    )}
                    <pre className="shell-run-pre">{runOutput || (running ? 'Running…' : '')}</pre>
                </div>
            )}
            <div className="shell-term" ref={termRef} />
        </div>
    );
}
