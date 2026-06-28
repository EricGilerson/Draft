import {useEffect, useRef, useState} from 'react';
import {StartLogStream, StopLogStream} from '../../wailsjs/go/main/App';
import {EventsOn, EventsOff} from '../../wailsjs/runtime/runtime';

export default function LogsTab({nodeId}: {nodeId: string}) {
    const [lines, setLines] = useState<{line: string; stream: string}[]>([]);
    const [streaming, setStreaming] = useState(false);
    const [error, setError] = useState('');
    const logRef = useRef<HTMLDivElement>(null);
    const autoScroll = useRef(true);

    useEffect(() => {
        StartLogStream(nodeId).then(() => {
            setStreaming(true);
        }).catch((e: any) => {
            setError(typeof e === 'string' ? e : e?.message || 'Could not start log stream');
        });

        const eventName = 'container:log:' + nodeId;
        EventsOn(eventName, (ev: any) => {
            setLines(prev => {
                const next = [...prev, {line: ev.line, stream: ev.stream}];
                return next.length > 5000 ? next.slice(-4000) : next;
            });
        });

        return () => {
            EventsOff(eventName);
            StopLogStream(nodeId);
            setStreaming(false);
        };
    }, [nodeId]);

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
                    {streaming ? 'Streaming' : error || 'Not connected'}
                </span>
                {lines.length > 0 && (
                    <button className="btn btn-ghost btn-sm" onClick={() => setLines([])}>
                        Clear
                    </button>
                )}
            </div>
            <div className="log-viewer log-viewer--full" ref={logRef} onScroll={handleScroll}>
                {lines.length === 0 && !error && (
                    <span className="deploy-empty">Waiting for output...</span>
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
