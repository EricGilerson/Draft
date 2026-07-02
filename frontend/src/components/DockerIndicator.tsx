import {useEffect, useState} from 'react';
import {CheckDocker, StartDocker} from '../../wailsjs/go/main/App';
import {EventsOn} from '../../wailsjs/runtime/runtime';
import {dockerwatch} from '../../wailsjs/go/models';
import './DockerIndicator.css';

type DockerState = 'checking' | 'running' | 'stopped' | 'starting';

export default function DockerIndicator() {
    const [state, setState] = useState<DockerState>('checking');
    const [detail, setDetail] = useState<string>('');
    const [startingError, setStartingError] = useState<string>('');

    useEffect(() => {
        const apply = (status?: dockerwatch.DaemonStatus) => {
            if (!status || !status.state) return;
            if (status.state === 'running') {
                setState('running');
                setDetail(status.apiVersion ? `Docker API v${status.apiVersion}` : 'Docker running');
                setStartingError('');
            } else {
                setState('stopped');
                setDetail(status.error || 'Docker daemon not reachable');
            }
        };

        // Subscribe first so no change is missed, then fetch the current value
        // once. After that the backend pushes updates only when state changes —
        // no polling.
        const unsubscribe = EventsOn('docker:status', (status: dockerwatch.DaemonStatus) => apply(status));
        CheckDocker()
            .then(apply)
            .catch((e) => {
                setState('stopped');
                setDetail(String(e));
            });

        return () => {
            unsubscribe();
        };
    }, []);

    const handleStart = () => {
        setState('starting');
        setStartingError('');
        setDetail('Starting Docker...');
        StartDocker()
            .catch((e) => {
                setState('stopped');
                setStartingError(String(e));
                setDetail(String(e));
            });
    };

    const label =
        state === 'running' ? 'Docker running'
            : state === 'starting' ? 'Starting Docker...'
            : state === 'stopped' ? 'Docker off'
                : 'Checking…';

    return (
        <div className={'docker-indicator ' + state} title={detail || label}>
            <span className="docker-dot"/>
            <span className="docker-label">{label}</span>
            {state === 'running' && detail && <span className="docker-detail">{detail.replace('Docker API ', '')}</span>}
            {state === 'stopped' && (
                <button className="docker-start-button" onClick={handleStart} type="button">
                    Start
                </button>
            )}
            {startingError && <span className="docker-error" title={startingError}>Start failed</span>}
        </div>
    );
}
