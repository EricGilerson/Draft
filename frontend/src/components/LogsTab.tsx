import {useEffect, useRef, useState} from 'react';
import {EventsOn} from '../../wailsjs/runtime/runtime';
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

export default function LogsTab({nodeId}: {nodeId: string}) {
    const [lines, setLines] = useState<{line: string; stream: string}[]>([]);
    const [streaming, setStreaming] = useState(false);
    const [connecting, setConnecting] = useState(false);
    const [error, setError] = useState('');
    const logRef = useRef<HTMLDivElement>(null);
    const autoScroll = useRef(true);
    const {deploying} = useBuildLog(nodeId);
    const {loading: linkLoading, isLinked, linkInfo, targetNodeId} = useLinkedServiceTarget(nodeId);

    useEffect(() => {
        setLines([]);
        setError('');
        setStreaming(false);
        setConnecting(false);
        autoScroll.current = true;
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
            setLines(prev => {
                const next = [...prev, {line: ev.line, stream: ev.stream}];
                return next.length > 5000 ? next.slice(-4000) : next;
            });
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

    const handleScroll = () => {
        if (!logRef.current) return;
        const {scrollTop, scrollHeight, clientHeight} = logRef.current;
        autoScroll.current = scrollHeight - scrollTop - clientHeight < 40;
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
                {lines.length === 0 && !error && (
                    <span className="deploy-empty">
                        {deploying ? 'Waiting for the container to start...' : connecting ? 'Connecting to container logs...' : 'Waiting for output...'}
                    </span>
                )}
                {lines.map((entry, i) => (
                    <div key={i} className={`log-line ${entry.stream === 'stderr' ? 'log-line--error' : ''}`}>
                        {entry.line}
                    </div>
                ))}
            </div>
        </div>
    );
}
