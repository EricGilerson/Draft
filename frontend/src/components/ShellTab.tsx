import {useEffect, useRef, useState} from 'react';
import {Terminal as XTerm} from '@xterm/xterm';
import {FitAddon} from '@xterm/addon-fit';
import {Play, Square} from 'lucide-react';
import '@xterm/xterm/css/xterm.css';
import {MintShellAttach, RunCommand} from '../../wailsjs/go/main/App';
import {deploy} from '../../wailsjs/go/models';
import {useLinkedServiceTarget} from '../lib/linkedService';
import './ShellTab.css';

type ShellTabProps = {
    nodeId: string;
};

const DEFAULT_SHELL = 'sh';
const SHELLS = ['sh', 'bash', 'ash', 'zsh'];

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
    const [status, setStatus] = useState<'connecting' | 'open' | 'closed' | 'error'>('connecting');
    const [error, setError] = useState<string | null>(null);
    const {loading: linkLoading, isLinked, linkInfo, targetNodeId} = useLinkedServiceTarget(nodeId);

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

    // (Re)connect whenever the node or chosen shell changes. Each connection
    // owns its own xterm instance so a reconnect always starts from a clean
    // terminal rather than a half-written one.
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
                fontSize: 13,
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
                // Resize pty to the terminal's current cols/rows.
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

            // Re-fit on container resize.
            const ro = new ResizeObserver(() => {
                try { fit!.fit(); } catch { /* ignore */ }
            });
            ro.observe(termRef.current);

            cleanup = () => {
                dataDisp.dispose();
                resizeDisp.dispose();
                ro.disconnect();
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
    }, [targetNodeId, shell, linkLoading]);

    function sendResize(term: XTerm, ws: WebSocket) {
        if (ws.readyState !== WebSocket.OPEN) return;
        try {
            ws.send(JSON.stringify({type: 'resize', cols: term.cols, rows: term.rows}));
        } catch { /* ignore */ }
    }

    const reconnect = () => {
        // Toggling shell to the same value would skip the effect; force a
        // remount by flipping to a sentinel then back.
        const cur = shellRef.current;
        setShell(cur + ' ');
        setTimeout(() => setShell(cur), 0);
    };

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
                <button className="btn btn-ghost shell-reconnect" onClick={reconnect} title="Reconnect the shell" disabled={linkLoading}>
                    Reconnect
                </button>
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
