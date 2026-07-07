import {Play, Square, RotateCcw, ExternalLink, AlertCircle, Loader2, Terminal} from 'lucide-react';
import {useEffect, useRef, useState} from 'react';
import {BrowserOpenURL} from '../../wailsjs/runtime/runtime';
import {
    DeployService, StopService, RestartService,
    GetActiveDeployment, GetLocalDomainStatus, GetNodeConfigStatus,
    RunCommand,
} from '../../wailsjs/go/main/App';
import {networking, store, deploy} from '../../wailsjs/go/models';
import {useBuildLog} from './BuildLogProvider';
import StatusBadge from './StatusBadge';

type OverviewTabProps = {
    nodeId: string;
    onServicesChanged?: () => void;
};

export default function OverviewTab({nodeId, onServicesChanged}: OverviewTabProps) {
    const [deployment, setDeployment] = useState<store.Deployment | null>(null);
    const [error, setError] = useState('');
    const [settings, setSettings] = useState<Record<string, string>>({});
    const [hasStagedChanges, setHasStagedChanges] = useState(false);
    const [localDomain, setLocalDomain] = useState<networking.LocalDomainStatus | null>(null);
    const buildLogRef = useRef<HTMLDivElement>(null);
    const autoScroll = useRef(true);
    const {lines: buildLines, deploying, version, pendingAction, setPendingAction, uploadProgress} = useBuildLog(nodeId);

    // One-shot "Run" bar: execute a command in the running container without
    // switching to the Shell tab (handy for `npm run migrate`, `rails db:seed`).
    const [runCmd, setRunCmd] = useState('');
    const [runWorkDir, setRunWorkDir] = useState('');
    const [runRunning, setRunRunning] = useState(false);
    const [runOutput, setRunOutput] = useState('');
    const [runExit, setRunExit] = useState<number | null>(null);
    const [showRunOutput, setShowRunOutput] = useState(false);

    useEffect(() => {
        GetActiveDeployment(nodeId).then(d => setDeployment(d || null));
        GetNodeConfigStatus(nodeId).then((status) => {
            const applied = status?.appliedSettings || {};
            const staged = status?.stagedSettings || {};
            setSettings({...applied, ...staged});
            setHasStagedChanges(!!status?.hasStagedChanges);
        });
        GetLocalDomainStatus().then(setLocalDomain).catch(() => setLocalDomain(null));
    }, [nodeId]);

    useEffect(() => {
        if (version === 0) return;
        GetActiveDeployment(nodeId).then(d => {
            setDeployment(d || null);
            GetLocalDomainStatus().then(setLocalDomain).catch(() => {});
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

    // Build-mode services need a Dockerfile + port; image-mode services need an
    // image + port. Either path is deployable.
    const canDeploy = !!settings.service_port && (!!settings.dockerfile || !!settings.image);

    const handleDeploy = async () => {
        setError('');
        setPendingAction('deploying');
        try {
            await DeployService(nodeId);
            onServicesChanged?.();
        } catch (e: any) {
            setPendingAction(null);
            setError(typeof e === 'string' ? e : e?.message || 'Deploy failed');
        }
    };

    const handleStop = async () => {
        setPendingAction('stopping');
        try {
            await StopService(nodeId);
            onServicesChanged?.();
        } catch (e: any) {
            setPendingAction(null);
            setError(typeof e === 'string' ? e : e?.message || 'Stop failed');
        }
    };

    const handleRestart = async () => {
        setPendingAction('restarting');
        try {
            await RestartService(nodeId);
            onServicesChanged?.();
        } catch (e: any) {
            setPendingAction(null);
            setError(typeof e === 'string' ? e : e?.message || 'Restart failed');
        }
    };

    const status = deployment?.status || 'stopped';
    const isRunning = status === 'running';
    const isActive = status === 'building' || status === 'starting' || status === 'running';
    const publicURL = bestPublicDeploymentURL(deployment, localDomain);
    const localURL = deployment && deployment.hostPort > 0 ? `http://127.0.0.1:${deployment.hostPort}` : '';

    const handleOpenDeployment = () => {
        if (!publicURL) return;
        BrowserOpenURL(publicURL);
    };

    const handleOpenLocal = () => {
        if (!localURL) return;
        BrowserOpenURL(localURL);
    };

    const handleRunCommand = () => {
        const trimmed = runCmd.trim();
        if (!trimmed || runRunning) return;
        const argv = trimmed.split(/\s+/).filter(Boolean);
        setRunRunning(true);
        setRunOutput('');
        setRunExit(null);
        setShowRunOutput(true);
        RunCommand(nodeId, argv, runWorkDir.trim())
            .then((res: deploy.RunCommandResult) => {
                setRunRunning(false);
                setRunOutput(res.output || '');
                setRunExit(res.exitCode);
                if (res.error) setRunOutput((prev) => (prev ? prev + '\n' : '') + `[error] ${res.error}`);
            })
            .catch((e: any) => {
                setRunRunning(false);
                const msg = typeof e === 'string' ? e : e?.message || 'run failed';
                setRunOutput(`[error] ${msg}`);
                setRunExit(null);
            });
    };

    return (
        <div className="overview-tab">
            <div className="overview-status-row">
                <StatusBadge status={status} />
                {publicURL && isRunning && (
                    <button
                        type="button"
                        className="overview-hostname"
                        title={publicURL}
                        onClick={handleOpenDeployment}
                    >
                        <ExternalLink size={11} />
                        {publicURL.replace('http://', '')}
                    </button>
                )}
            </div>

            {hasStagedChanges && (
                <div className="overview-staged-banner">
                    Staged settings will apply on the next deploy.
                </div>
            )}

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
                        disabled={!canDeploy || !!pendingAction}
                        title={!canDeploy ? 'Set an image or Dockerfile and a port in Settings first' : 'Deploy service'}
                    >
                        {pendingAction === 'deploying' ? <Loader2 size={13} className="spin" /> : <Play size={13} />}
                        {pendingAction === 'deploying' ? 'Deploying…' : 'Deploy'}
                    </button>
                )}
                {deploying && (
                    <button className="btn btn-ghost" onClick={handleStop} disabled={!!pendingAction}>
                        {pendingAction === 'stopping' ? <Loader2 size={13} className="spin" /> : <Square size={13} />}
                        {pendingAction === 'stopping' ? 'Cancelling…' : 'Cancel Build'}
                    </button>
                )}
                {isActive && !deploying && (
                    <>
                        <button className="btn btn-ghost" onClick={handleStop} disabled={!!pendingAction}>
                            {pendingAction === 'stopping' ? <Loader2 size={13} className="spin" /> : <Square size={13} />}
                            {pendingAction === 'stopping' ? 'Stopping…' : 'Stop'}
                        </button>
                        <button className="btn btn-ghost" onClick={handleRestart} disabled={!!pendingAction}>
                            {pendingAction === 'restarting' ? <Loader2 size={13} className="spin" /> : <RotateCcw size={13} />}
                            {pendingAction === 'restarting' ? 'Restarting…' : 'Restart'}
                        </button>
                        <button className="btn btn-primary" onClick={handleDeploy} disabled={!!pendingAction}>
                            {pendingAction === 'deploying' ? <Loader2 size={13} className="spin" /> : <Play size={13} />}
                            {pendingAction === 'deploying' ? 'Rebuilding…' : 'Rebuild'}
                        </button>
                    </>
                )}
            </div>

            {isRunning && (
                <div className="overview-runbar">
                    <Terminal size={13} className="overview-runbar-icon"/>
                    <input
                        className="input overview-runbar-input"
                        type="text"
                        value={runCmd}
                        onChange={(e) => setRunCmd(e.target.value)}
                        onKeyDown={(e) => { if (e.key === 'Enter') handleRunCommand(); }}
                        placeholder="Run a command in the container (e.g. npm run migrate)"
                        disabled={runRunning}
                    />
                    <input
                        className="input overview-runbar-workdir"
                        type="text"
                        value={runWorkDir}
                        onChange={(e) => setRunWorkDir(e.target.value)}
                        placeholder="workdir (optional)"
                        disabled={runRunning}
                        title="Container working directory (optional)"
                    />
                    <button
                        className="btn btn-primary"
                        onClick={handleRunCommand}
                        disabled={runRunning || !runCmd.trim()}
                        title="Run the command in the running container"
                    >
                        {runRunning ? <Loader2 size={13} className="spin"/> : <Play size={13}/>}
                        {runRunning ? 'Running…' : 'Run'}
                    </button>
                    <button
                        className="btn btn-ghost"
                        onClick={() => setShowRunOutput((s) => !s)}
                        disabled={!runOutput && !runRunning}
                    >
                        {showRunOutput ? 'Hide' : 'Show output'}
                    </button>
                    {showRunOutput && (runOutput || runRunning) && (
                        <div className="overview-runbar-output">
                            {runExit !== null && (
                                <div className={`overview-runbar-exit ${runExit === 0 ? 'overview-runbar-exit--ok' : 'overview-runbar-exit--fail'}`}>
                                    exit {runExit}
                                </div>
                            )}
                            <pre className="overview-runbar-pre">{runOutput || (runRunning ? 'Running…' : '')}</pre>
                        </div>
                    )}
                </div>
            )}

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
                        {uploadProgress && (
                            <div className="upload-progress">
                                <div className="upload-progress-bar">
                                    <div
                                        className={`upload-progress-fill${uploadProgress.indeterminate ? ' upload-progress-fill--indeterminate' : ''}`}
                                        style={uploadProgress.indeterminate ? undefined : {width: `${uploadProgress.percent}%`}}
                                    />
                                </div>
                                <span className="upload-progress-label">
                                    {uploadProgress.indeterminate
                                        ? `Streaming to Docker — ${(uploadProgress.sentBytes / 1024 / 1024).toFixed(1)} MB`
                                        : `Uploading ${uploadProgress.percent}% — ${(uploadProgress.sentBytes / 1024 / 1024).toFixed(1)} / ${(uploadProgress.totalBytes / 1024 / 1024).toFixed(1)} MB`}
                                </span>
                            </div>
                        )}
                    </div>
                </div>
            )}

            {!canDeploy && (
                <span className="overview-hint">
                    Set an image (or Dockerfile) and a port in the Settings tab before deploying.
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
                            <span className="overview-detail-label">Mapped Host Port</span>
                            <span className="overview-detail-value mono">{deployment.hostPort}</span>
                        </div>
                    )}
                    {deployment.hostname && (
                        <div className="overview-detail-row">
                            <span className="overview-detail-label">Internal Hostname</span>
                            <span className="overview-detail-value mono">{deployment.hostname}</span>
                        </div>
                    )}
                    {publicURL && (
                        <div className="overview-detail-row">
                            <span className="overview-detail-label">Public URL</span>
                            <span className="overview-detail-value mono">{publicURL.replace('http://', '')}</span>
                        </div>
                    )}
                    {localDomain?.mode && (
                        <div className="overview-detail-row">
                            <span className="overview-detail-label">Public Access Mode</span>
                            <span className="overview-detail-value mono">{localDomain.mode}</span>
                        </div>
                    )}
                    {localURL && (
                        <div className="overview-detail-row">
                            <span className="overview-detail-label">Local URL</span>
                            <button
                                type="button"
                                className="overview-detail-value mono overview-detail-link"
                                title="Direct loopback address. Works without internet — use this for apps running on your machine (e.g. a desktop client)."
                                onClick={handleOpenLocal}
                            >
                                {localURL.replace('http://', '')}
                            </button>
                        </div>
                    )}
                    {localDomain?.hostsError && (
                        <div className="overview-domain-note">
                            Internal Draft names are not host-resolved. Public access uses the configured Draft public hostname with the proxy port.
                        </div>
                    )}
                </div>
            )}
        </div>
    );
}

function bestPublicDeploymentURL(
    deployment: store.Deployment | null,
    localDomain: networking.LocalDomainStatus | null,
): string {
    if (!deployment) return '';
    if (deployment.hostname && localDomain?.proxyPort) {
        const publicHostname = hostnameWithSuffix(
            deployment.hostname,
            localDomain.publicSuffix || localDomain.loopbackSuffix,
        );
        if (publicHostname) {
            if (localDomain.proxyPort === 80) {
                return `http://${publicHostname}`;
            }
            return `http://${publicHostname}:${localDomain.proxyPort}`;
        }
    }
    if (deployment.hostPort > 0) {
        return `http://127.0.0.1:${deployment.hostPort}`;
    }
    return '';
}

function hostnameWithSuffix(hostname: string, suffix: string): string {
    if (!suffix) return '';
    return hostname.replace(/\.draft\.local\.?$/, `.${suffix}`);
}
