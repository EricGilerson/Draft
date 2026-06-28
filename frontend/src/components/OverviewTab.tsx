import {Play, Square, RotateCcw, ExternalLink, AlertCircle} from 'lucide-react';
import {useEffect, useState} from 'react';
import {
    GetNodeSettings,
    DeployService, StopService, RestartService,
    GetActiveDeployment,
} from '../../wailsjs/go/main/App';
import {store} from '../../wailsjs/go/models';
import {EventsOn, EventsOff} from '../../wailsjs/runtime/runtime';
import StatusBadge from './StatusBadge';

export default function OverviewTab({nodeId}: {nodeId: string}) {
    const [deployment, setDeployment] = useState<store.Deployment | null>(null);
    const [deploying, setDeploying] = useState(false);
    const [error, setError] = useState('');
    const [settings, setSettings] = useState<Record<string, string>>({});

    useEffect(() => {
        GetActiveDeployment(nodeId).then(d => setDeployment(d || null));
        GetNodeSettings(nodeId).then(s => setSettings(s || {}));
    }, [nodeId]);

    useEffect(() => {
        const eventName = 'deploy:status:' + nodeId;
        EventsOn(eventName, (ev: any) => {
            setDeploying(ev.status === 'building' || ev.status === 'starting');
            if (ev.status === 'failed') {
                setError(ev.error || 'Deployment failed');
            } else {
                setError('');
            }
            GetActiveDeployment(nodeId).then(d => setDeployment(d || null));
        });
        return () => { EventsOff(eventName); };
    }, [nodeId]);

    const canDeploy = settings.dockerfile && settings.service_port;

    const handleDeploy = async () => {
        setError('');
        setDeploying(true);
        try {
            await DeployService(nodeId);
        } catch (e: any) {
            setError(typeof e === 'string' ? e : e?.message || 'Deploy failed');
            setDeploying(false);
        }
    };

    const handleStop = async () => {
        try {
            await StopService(nodeId);
        } catch (e: any) {
            setError(typeof e === 'string' ? e : e?.message || 'Stop failed');
        }
    };

    const handleRestart = async () => {
        try {
            await RestartService(nodeId);
        } catch (e: any) {
            setError(typeof e === 'string' ? e : e?.message || 'Restart failed');
        }
    };

    const status = deployment?.status || 'stopped';
    const isRunning = status === 'running';
    const isActive = status === 'building' || status === 'starting' || status === 'running';

    return (
        <div className="overview-tab">
            <div className="overview-status-row">
                <StatusBadge status={status} />
                {deployment?.hostname && isRunning && (
                    <span className="overview-hostname" title={deployment.hostname}>
                        <ExternalLink size={11} />
                        {deployment.hostname}
                    </span>
                )}
            </div>

            {error && (
                <div className="overview-error">
                    <AlertCircle size={13} />
                    <span>{error}</span>
                </div>
            )}

            {deployment?.error && status === 'failed' && !error && (
                <div className="overview-error">
                    <AlertCircle size={13} />
                    <span>{deployment.error}</span>
                </div>
            )}

            <div className="overview-actions">
                {!isActive && (
                    <button
                        className="btn btn-primary"
                        onClick={handleDeploy}
                        disabled={!canDeploy || deploying}
                        title={!canDeploy ? 'Set Dockerfile and Port in Settings first' : 'Deploy service'}
                    >
                        <Play size={13} />
                        {deploying ? 'Deploying...' : 'Deploy'}
                    </button>
                )}
                {isActive && !deploying && (
                    <>
                        <button className="btn btn-ghost" onClick={handleStop}>
                            <Square size={13} /> Stop
                        </button>
                        <button className="btn btn-ghost" onClick={handleRestart}>
                            <RotateCcw size={13} /> Restart
                        </button>
                        <button className="btn btn-primary" onClick={handleDeploy}>
                            <Play size={13} /> Rebuild
                        </button>
                    </>
                )}
            </div>

            {!canDeploy && (
                <span className="overview-hint">
                    Configure Dockerfile and Port in the Settings tab before deploying.
                </span>
            )}

            {deployment && (
                <div className="overview-details">
                    {deployment.imageTag && (
                        <div className="overview-detail-row">
                            <span className="overview-detail-label">Image</span>
                            <span className="overview-detail-value mono">{deployment.imageTag}</span>
                        </div>
                    )}
                    {deployment.containerId && (
                        <div className="overview-detail-row">
                            <span className="overview-detail-label">Container</span>
                            <span className="overview-detail-value mono">{deployment.containerId.slice(0, 12)}</span>
                        </div>
                    )}
                    {deployment.hostPort > 0 && (
                        <div className="overview-detail-row">
                            <span className="overview-detail-label">Host Port</span>
                            <span className="overview-detail-value mono">{deployment.hostPort}</span>
                        </div>
                    )}
                </div>
            )}
        </div>
    );
}
