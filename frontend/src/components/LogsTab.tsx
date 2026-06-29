import {useEffect, useRef, useState} from 'react';
import {StartLogStream, StopLogStream} from '../../wailsjs/go/main/App';
import {EventsOn, EventsOff} from '../../wailsjs/runtime/runtime';
import {useBuildLog} from './BuildLogProvider';

export default function LogsTab({nodeId}: {nodeId: string}) {
    const [lines, setLines] = useState<{line: string; stream: string}[]>([]);
    const [streaming, setStreaming] = useState(false);
    const [connecting, setConnecting] = useState(false);
    const [error, setError] = useState('');
    const logRef = useRef<HTMLDivElement>(null);
    const autoScroll = useRef(true);
    const {deploying, version} = useBuildLog(nodeId);

    useEffect(() => {
        let cancelled = false;
        const start = () => {
            if (deploying) {
                setConnecting(false);
                setStreaming(false);
                setError('');
                return;
            }
            setConnecting(true);
            setError('');
            StartLogStream(nodeId).then(() => {
                if (cancelled) return;
                setStreaming(true);
                setConnecting(false);
            }).catch((e: any) => {
                if (cancelled) return;
                const msg = typeof e === 'string' ? e : e?.message || 'Could not start log stream';
                if (msg.includes('no active container')) {
                    setError('');
                } else {
                    setError(msg);
                }
                setStreaming(false);
                setConnecting(false);
            });
        };

        start();

        const eventName = 'container:log:' + nodeId;
        EventsOn(eventName, (ev: any) => {
            setLines(prev => {
                const next = [...prev, {line: ev.line, stream: ev.stream}];
                return next.length > 5000 ? next.slice(-4000) : next;
            });
        });

        return () => {
            cancelled = true;
            EventsOff(eventName);
            StopLogStream(nodeId);
            setStreaming(false);
            setConnecting(false);
        };
    }, [nodeId, deploying, version]);

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
                <span className="logs-status">
                    {streaming ? 'Streaming' : deploying ? 'Deployment starting...' : connecting ? 'Connecting...' : error || 'Not connected'}
                </span>
                {lines.length > 0 && (
                    <button className="btn btn-ghost btn-sm" onClick={() => setLines([])}>
                        Clear
                    </button>
                )}
            </div>
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
