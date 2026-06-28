import {Play, Square, RotateCcw, ExternalLink, AlertCircle} from 'lucide-react';
import {useEffect, useRef, useState} from 'react';
import {BrowserOpenURL} from '../../wailsjs/runtime/runtime';
import {
    GetNodeSettings,
    DeployService, StopService, RestartService,
    GetActiveDeployment,
} from '../../wailsjs/go/main/App';
import {store} from '../../wailsjs/go/models';
import {useBuildLog} from './BuildLogProvider';
import StatusBadge from './StatusBadge';

export default function OverviewTab({nodeId}: {nodeId: string}) {
    const [deployment, setDeployment] = useState<store.Deployment | null>(null);
    const [error, setError] = useState('');
    const [settings, setSettings] = useState<Record<string, string>>({});
    const buildLogRef = useRef<HTMLDivElement>(null);
    const autoScroll = useRef(true);
    const {lines: buildLines, deploying, version} = useBuildLog(nodeId);

    useEffect(() => {
        GetActiveDeployment(nodeId).then(d => setDeployment(d || null));
        GetNodeSettings(nodeId).then(s => setSettings(s || {}));
    }, [nodeId]);

    useEffect(() => {
        if (version === 0) return;
        GetActiveDeployment(nodeId).then(d => {
            setDeployment(d || null);
            if (d?.status === 'failed') {
                setError(d.error || 'Deployment failed');
            } else {
                setError('');
            }
        });
    }, [nodeId, version]);

    useEffect(() => {
        if (autoScroll.current && buildLogRef.current) {
            buildLogRef.current.scrollTop = buildLogRef.current.scrollHeight;
        }
    }, [buildLines]);

    const handleBuildLogScroll = () => {
        if (!buildLogRef.current) return;
        const {scrollTop, scrollHeight, clientHeight} = buildLogRef.current;
        autoScroll.current = scrollHeight - scrollTop - clientHeight < 40;
    };

    const canDeploy = settings.dockerfile && settings.service_port;

    const handleDeploy = async () => {
        setError('');
        try {
            await DeployService(nodeId);
        } catch (e: any) {
            setError(typeof e === 'string' ? e : e?.message || 'Deploy failed');
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
    const deploymentURL = deployment?.hostname ? `http://${deployment.hostname}` : '';

    const handleOpenDeployment = () => {
        if (!deploymentURL) return;
        BrowserOpenURL(deploymentURL);
    };

    return (
        <div className="overview-tab">
            <div className="overview-status-row">
                <StatusBadge status={status} />
                {deployment?.hostname && isRunning && (
                    <button
                        type="button"
                        className="overview-hostname"
                        title={deploymentURL}
                        onClick={handleOpenDeployment}
                    >
                        <ExternalLink size={11} />
                        {deployment.hostname}
                    </button>
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
                {!isActive && !deploying && (
                    <button
                        className="btn btn-primary"
                        onClick={handleDeploy}
                        disabled={!canDeploy}
                        title={!canDeploy ? 'Set Dockerfile and Port in Settings first' : 'Deploy service'}
                    >
                        <Play size={13} /> Deploy
                    </button>
                )}
                {deploying && (
                    <button className="btn btn-ghost" onClick={handleStop}>
                        <Square size={13} /> Cancel Build
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

            {(deploying || buildLines.length > 0) && (
                <div className="overview-build-log">
                    <h4 className="deploy-log-title">
                        {deploying ? 'Build output' : 'Last build output'}
                    </h4>
                    <div
                        className="log-viewer log-viewer--build"
                        ref={buildLogRef}
                        onScroll={handleBuildLogScroll}
                    >
                        {buildLines.length === 0 && deploying && (
                            <span className="deploy-empty">Waiting for build output...</span>
                        )}
                        {buildLines.map((line, i) => (
                            <div key={i} className="log-line">{line}</div>
                        ))}
                    </div>
                </div>
            )}

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
