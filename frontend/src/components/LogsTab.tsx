import {memo, useCallback, useEffect, useRef, useState} from 'react';
import {EventsOn} from '../../wailsjs/runtime/runtime';
import {GetContainerLogHistory} from '../../wailsjs/go/main/App';
import {useBuildLog} from './BuildLogProvider';
import {acquireLogStream, releaseLogStream} from '../lib/logStreamManager';
import {useLinkedServiceTarget} from '../lib/linkedService';

function getErrorMessage(error: unknown) {
    return typeof error === 'string' ? error : (error as {message?: string})?.message || 'Could not start log stream';
}

function isTransientConnectError(message: string) {
    const value = message.toLowerCase();
    return value.includes('context canceled') || value.includes('connection reset') || value.includes('unexpected eof');
}

type LogEntry = {line: string; stream: string; timestamp?: string};

const INITIAL_LOG_TAIL = 200;
const LOG_HISTORY_PAGE_SIZE = 500;
const MAX_RENDERED_LOG_LINES = 2000;

function entryKey(entry: LogEntry) {
    return `${entry.timestamp || ''}\u0000${entry.stream}\u0000${entry.line}`;
}

function mergeHistory(history: LogEntry[], current: LogEntry[]) {
    // Docker returns a cumulative tail. Remove only the copies already on
    // screen, so repeated messages with distinct timestamps remain intact.
    const currentCounts = new Map<string, number>();
    for (const entry of current) {
        const key = entryKey(entry);
        currentCounts.set(key, (currentCounts.get(key) || 0) + 1);
    }
    const older = history.filter(entry => {
        const key = entryKey(entry);
        const count = currentCounts.get(key) || 0;
        if (count === 0) return true;
        currentCounts.set(key, count - 1);
        return false;
    });
    return [...older, ...current];
}

const LogLine = memo(function LogLine({entry}: {entry: LogEntry}) {
    return (
        <div className={`log-line ${entry.stream === 'stderr' ? 'log-line--error' : ''}`}>
            {entry.line}
        </div>
    );
});

export default function LogsTab({nodeId}: {nodeId: string}) {
    const [lines, setLines] = useState<LogEntry[]>([]);
    const [streaming, setStreaming] = useState(false);
    const [connecting, setConnecting] = useState(false);
    const [error, setError] = useState('');
    const logRef = useRef<HTMLDivElement>(null);
    const autoScroll = useRef(true);
    const historyTail = useRef(INITIAL_LOG_TAIL);
    const loadingHistory = useRef(false);
    const hasMoreHistory = useRef(true);
    const [historyLoading, setHistoryLoading] = useState(false);
    const pendingRef = useRef<LogEntry[]>([]);
    const flushScheduledRef = useRef(false);
    const {deploying} = useBuildLog(nodeId);
    const {loading: linkLoading, isLinked, linkInfo, targetNodeId} = useLinkedServiceTarget(nodeId);

    useEffect(() => {
        setLines([]);
        setError('');
        setStreaming(false);
        setConnecting(false);
        autoScroll.current = true;
        historyTail.current = INITIAL_LOG_TAIL;
        loadingHistory.current = false;
        hasMoreHistory.current = true;
        setHistoryLoading(false);
        pendingRef.current = [];
        flushScheduledRef.current = false;
    }, [nodeId, targetNodeId]);

    useEffect(() => {
        if (linkLoading) {
            setConnecting(true);
            setStreaming(false);
            setError('');
            return;
        }
        let cancelled = false;
        let retryTimer: number | null = null;

        const flushPending = () => {
            flushScheduledRef.current = false;
            if (cancelled || pendingRef.current.length === 0) return;
            const batch = pendingRef.current;
            pendingRef.current = [];
            setLines(prev => {
                const next = prev.length === 0 ? batch : prev.concat(batch);
                return next.length > MAX_RENDERED_LOG_LINES ? next.slice(-MAX_RENDERED_LOG_LINES) : next;
            });
        };

        const start = async (attempt = 0) => {
            if (deploying) {
                setConnecting(false);
                setStreaming(false);
                setError('');
                return;
            }
            setConnecting(true);
            setError('');
            try {
                await acquireLogStream(targetNodeId);
                if (cancelled) return;
                setStreaming(true);
                setConnecting(false);
                setError('');
            } catch (e: any) {
                releaseLogStream(targetNodeId);
                if (cancelled) return;
                const msg = getErrorMessage(e);
                if (msg.includes('no active container')) {
                    setError('');
                } else if (isTransientConnectError(msg) && attempt < 2) {
                    retryTimer = window.setTimeout(() => {
                        void start(attempt + 1);
                    }, 300 * (attempt + 1));
                    setError('');
                    setStreaming(false);
                    setConnecting(true);
                } else {
                    setError('Could not connect to container logs.');
                    setStreaming(false);
                    setConnecting(false);
                }
            }
        };

        void start();

        const eventName = 'container:log:' + targetNodeId;
        const unsubscribe = EventsOn(eventName, (ev: any) => {
            pendingRef.current.push({line: ev.line, stream: ev.stream, timestamp: ev.timestamp});
            if (!flushScheduledRef.current) {
                flushScheduledRef.current = true;
                requestAnimationFrame(flushPending);
            }
        });

        return () => {
            cancelled = true;
            if (retryTimer !== null) {
                window.clearTimeout(retryTimer);
            }
            unsubscribe();
            releaseLogStream(targetNodeId);
            setStreaming(false);
            setConnecting(false);
        };
    }, [targetNodeId, deploying, linkLoading]);

    useEffect(() => {
        if (autoScroll.current && logRef.current) {
            logRef.current.scrollTop = logRef.current.scrollHeight;
        }
    }, [lines]);

    const loadOlderLogs = useCallback(async () => {
        const viewer = logRef.current;
        if (!viewer || loadingHistory.current || !hasMoreHistory.current) return;
        loadingHistory.current = true;
        setHistoryLoading(true);
        const previousHeight = viewer.scrollHeight;
        const previousTop = viewer.scrollTop;
        const nextTail = Math.min(historyTail.current + LOG_HISTORY_PAGE_SIZE, MAX_RENDERED_LOG_LINES);
        try {
            const history = await GetContainerLogHistory(targetNodeId, nextTail);
            historyTail.current = nextTail;
            hasMoreHistory.current = history.hasMore && nextTail < MAX_RENDERED_LOG_LINES;
            setLines(current => mergeHistory(history.lines, current));
            requestAnimationFrame(() => {
                if (logRef.current) {
                    logRef.current.scrollTop = previousTop + logRef.current.scrollHeight - previousHeight;
                }
            });
        } catch {
            // The live stream remains useful if a short-lived container exits
            // before its older output can be read.
        } finally {
            loadingHistory.current = false;
            setHistoryLoading(false);
        }
    }, [targetNodeId]);

    const handleScroll = () => {
        if (!logRef.current) return;
        const {scrollTop, scrollHeight, clientHeight} = logRef.current;
        autoScroll.current = scrollHeight - scrollTop - clientHeight < 40;
        if (scrollTop < 24) {
            void loadOlderLogs();
        }
    };

    return (
        <div className="logs-tab">
            <div className="logs-header">
                <span className={`logs-status ${error ? 'logs-status--error' : streaming ? 'logs-status--streaming' : ''}`}>
                    {streaming ? 'Streaming' : deploying ? 'Deployment starting...' : connecting ? 'Connecting...' : error || 'Not connected'}
                </span>
                {lines.length > 0 && (
                    <button className="btn btn-ghost btn-sm" onClick={() => setLines([])}>
                        Clear
                    </button>
                )}
            </div>
            {isLinked && (
                <div className="overview-staged-banner">
                    Showing logs from the shared root service in <strong>{linkInfo?.rootEnvName || 'another environment'}</strong>
                    {linkInfo?.rootLabel ? ` · ${linkInfo.rootLabel}` : ''}.
                </div>
            )}
            <div className="log-viewer log-viewer--full" ref={logRef} onScroll={handleScroll}>
                {historyLoading && <div className="log-history-loading">Loading older logs...</div>}
                {lines.length === 0 && !error && (
                    <span className="deploy-empty">
                        {deploying ? 'Waiting for the container to start...' : connecting ? 'Connecting to container logs...' : 'Waiting for output...'}
                    </span>
                )}
                {lines.map((entry, i) => (
                    <LogLine key={i} entry={entry} />
                ))}
            </div>
        </div>
    );
}
