import {Terminal as XTerm} from '@xterm/xterm';
import {FitAddon} from '@xterm/addon-fit';
import {ResizeAgentSession, WriteAgentSession} from '../../wailsjs/go/main/App';
import {EventsOn} from '../../wailsjs/runtime/runtime';

type OutputPayload = {sessionId: string; data: string};
type ExitPayload = {sessionId: string; exitCode: number; error?: string; restarting?: boolean};

type LiveTerminal = {
    sessionId: string;
    term: XTerm;
    fit: FitAddon;
    host: HTMLElement | null;
    lastCols: number;
    lastRows: number;
    writeQueue: Uint8Array[];
    raf: number;
    resizeTimer: ReturnType<typeof setTimeout> | null;
    unsubOut: () => void;
    unsubExit: () => void;
    dataDisp: {dispose: () => void};
    resizeDisp: {dispose: () => void};
    disposed: boolean;
};

const live = new Map<string, LiveTerminal>();

function b64ToUint8(b64: string): Uint8Array {
    const bin = atob(b64);
    const out = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
    return out;
}

function uint8ToB64(bytes: Uint8Array): string {
    let s = '';
    const chunk = 0x8000;
    for (let i = 0; i < bytes.length; i += chunk) {
        s += String.fromCharCode(...bytes.subarray(i, i + chunk));
    }
    return btoa(s);
}

function concatChunks(chunks: Uint8Array[]): Uint8Array {
    if (chunks.length === 1) return chunks[0];
    let total = 0;
    for (const c of chunks) total += c.length;
    const out = new Uint8Array(total);
    let off = 0;
    for (const c of chunks) {
        out.set(c, off);
        off += c.length;
    }
    return out;
}

function flushWrites(lt: LiveTerminal) {
    lt.raf = 0;
    if (lt.disposed || lt.writeQueue.length === 0) return;
    const data = concatChunks(lt.writeQueue);
    lt.writeQueue = [];
    lt.term.write(data);
}

function enqueueWrite(lt: LiveTerminal, data: Uint8Array) {
    if (lt.disposed || data.length === 0) return;
    lt.writeQueue.push(data);
    if (!lt.raf) {
        lt.raf = requestAnimationFrame(() => flushWrites(lt));
    }
}

function applyResize(lt: LiveTerminal, cols: number, rows: number, force = false) {
    if (lt.disposed) return;
    if (!force && cols === lt.lastCols && rows === lt.lastRows) return;
    if (cols < 2 || rows < 2) return;
    lt.lastCols = cols;
    lt.lastRows = rows;
    ResizeAgentSession(lt.sessionId, cols, rows).catch(() => undefined);
}

function scheduleFit(lt: LiveTerminal) {
    if (lt.disposed) return;
    if (lt.resizeTimer) clearTimeout(lt.resizeTimer);
    lt.resizeTimer = setTimeout(() => {
        lt.resizeTimer = null;
        if (lt.disposed || !lt.host) return;
        try {
            lt.fit.fit();
            const dims = lt.fit.proposeDimensions();
            if (dims) applyResize(lt, dims.cols, dims.rows);
        } catch {
            /* ignore */
        }
    }, 100);
}

function createLive(sessionId: string): LiveTerminal {
    const term = new XTerm({
        // Full-screen agent TUIs (Claude/Codex) manage newlines themselves.
        convertEol: false,
        fontSize: 13,
        fontFamily: 'Menlo, Consolas, "DejaVu Sans Mono", monospace',
        cursorBlink: true,
        scrollback: 10000,
    });
    const fit = new FitAddon();
    term.loadAddon(fit);

    const lt: LiveTerminal = {
        sessionId,
        term,
        fit,
        host: null,
        lastCols: 0,
        lastRows: 0,
        writeQueue: [],
        raf: 0,
        resizeTimer: null,
        unsubOut: () => undefined,
        unsubExit: () => undefined,
        dataDisp: {dispose: () => undefined},
        resizeDisp: {dispose: () => undefined},
        disposed: false,
    };

    lt.dataDisp = term.onData((d) => {
        const bytes = new TextEncoder().encode(d);
        WriteAgentSession(sessionId, uint8ToB64(bytes)).catch(() => undefined);
    });
    lt.resizeDisp = term.onResize(({cols, rows}) => {
        applyResize(lt, cols, rows);
    });

    lt.unsubOut = EventsOn('agent:session:output', (payload: OutputPayload) => {
        if (!payload || payload.sessionId !== sessionId || lt.disposed) return;
        try {
            enqueueWrite(lt, b64ToUint8(payload.data));
        } catch {
            /* ignore */
        }
    });
    lt.unsubExit = EventsOn('agent:session:exit', (payload: ExitPayload) => {
        if (!payload || payload.sessionId !== sessionId || lt.disposed) return;
        // Draft is relaunching in place after an update — keep the terminal attached.
        if (payload.restarting) return;
        const code = payload.exitCode ?? 0;
        const err = payload.error ? ` (${payload.error})` : '';
        enqueueWrite(lt, new TextEncoder().encode(`\r\n\r\n[session exited: ${code}]${err}\r\n`));
    });

    live.set(sessionId, lt);
    return lt;
}

/** Attach (or re-attach) a durable xterm instance to a host element. */
export function attachAgentTerminal(sessionId: string, host: HTMLElement): () => void {
    let lt = live.get(sessionId);
    if (!lt || lt.disposed) {
        lt = createLive(sessionId);
    }

    if (lt.host && lt.host !== host) {
        try {
            host.replaceChildren();
            // Move the existing terminal DOM node if present.
            const screen = lt.term.element;
            if (screen) host.appendChild(screen);
            else lt.term.open(host);
        } catch {
            lt.term.open(host);
        }
    } else if (!lt.term.element) {
        host.replaceChildren();
        lt.term.open(host);
    } else if (!host.contains(lt.term.element)) {
        host.replaceChildren();
        host.appendChild(lt.term.element);
    }
    lt.host = host;

    const ro = new ResizeObserver(() => scheduleFit(lt!));
    ro.observe(host);
    scheduleFit(lt);

    return () => {
        ro.disconnect();
        // Detach from React host but keep the PTY-backed terminal alive for StrictMode remounts.
        if (lt && lt.host === host) {
            lt.host = null;
        }
    };
}

export function focusAgentTerminal(sessionId: string) {
    const lt = live.get(sessionId);
    if (!lt || lt.disposed) return;
    scheduleFit(lt);
    try {
        lt.term.focus();
    } catch {
        /* ignore */
    }
}

export function disposeAgentTerminal(sessionId: string) {
    const lt = live.get(sessionId);
    if (!lt) return;
    lt.disposed = true;
    if (lt.raf) cancelAnimationFrame(lt.raf);
    if (lt.resizeTimer) clearTimeout(lt.resizeTimer);
    try {
        lt.dataDisp.dispose();
        lt.resizeDisp.dispose();
        lt.unsubOut();
        lt.unsubExit();
    } catch {
        /* ignore */
    }
    try {
        lt.term.dispose();
    } catch {
        /* ignore */
    }
    live.delete(sessionId);
}
