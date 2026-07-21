import {Play, Square, RotateCcw, ExternalLink, AlertCircle, Loader2, Terminal} from 'lucide-react';
import {useEffect, useRef, useState} from 'react';
import {BrowserOpenURL, EventsOn} from '../../wailsjs/runtime/runtime';
import {
    DeployService, StopService, RestartService,
    GetActiveDeployment, GetBuildLog, GetDeployments, GetLocalDomainStatus, GetNodeConfigStatus, GetServiceStaleness,
    GetLinkedServiceInfo, GetNodeHealth, PromoteLinkedService, UnlinkService,
    RunCommand,
} from '../../wailsjs/go/main/App';
import {networking, store, deploy} from '../../wailsjs/go/models';
import {useBuildLog} from './BuildLogProvider';
import StatusBadge from './StatusBadge';
import {useAppDialog} from './AppDialogProvider';
import Dialog from './Dialog';
import {bestPublicDeploymentURL, bestPublicEndpoint} from '../lib/localDomainUrls';

type CloneConsistency = 'consistent' | 'quick';

type OverviewTabProps = {
    nodeId: string;
    onServicesChanged?: () => void;
};

/** Parse a persisted Docker build-log file into display lines (JSON stream / raw). */
function parseBuildLogText(log: string, maxLines = 1500): string[] {
    if (!log) return [];
    const lines = log.split('\n').filter(Boolean).map((line) => {
        try {
            const obj = JSON.parse(line);
            return String(obj.error || obj.stream || obj.status || line).replace(/\n$/, '');
        } catch {
            return line;
        }
    });
    return lines.length > maxLines ? lines.slice(-maxLines) : lines;
}

function truncateOutput(text: string, maxChars = 200_000): string {
    if (!text || text.length <= maxChars) return text;
    return text.slice(text.length - maxChars) + '\n==> output truncated (display limit)\n';
}

/** Load runtime status for Overview. Linked aliases have no local deployment —
 *  status/hostnames come from GetNodeHealth (root-mirrored); container/image
 *  details come from the root's active deployment when linked.
 *  latest is the most recent deploy attempt (may be failed while active still runs). */
async function loadLinkedAwareRuntime(nodeId: string): Promise<{
    linkInfo: deploy.LinkedServiceInfo | null;
    health: deploy.NodeHealth | null;
    deployment: store.Deployment | null;
    latest: store.Deployment | null;
}> {
    let linkInfo: deploy.LinkedServiceInfo | null = null;
    try {
        linkInfo = await GetLinkedServiceInfo(nodeId);
    } catch {
        linkInfo = null;
    }
    let health: deploy.NodeHealth | null = null;
    try {
        health = await GetNodeHealth(nodeId);
    } catch {
        health = null;
    }
    const depNodeId = linkInfo?.isLinked && linkInfo.rootNodeId
        ? linkInfo.rootNodeId
        : nodeId;
    let deployment: store.Deployment | null = null;
    try {
        deployment = (await GetActiveDeployment(depNodeId)) || null;
    } catch {
        deployment = null;
    }
    let latest: store.Deployment | null = null;
    try {
        const deps = await GetDeployments(depNodeId);
        latest = deps?.[0] || null;
    } catch {
        latest = null;
    }
    // Prefer health's last-deploy fields when list is empty but health knows.
    if (!latest && health?.lastDeploymentId) {
        latest = {
            id: health.lastDeploymentId,
            status: health.lastDeployStatus || 'failed',
            error: health.lastDeployError || '',
            sequence: health.lastDeploySequence || 0,
        } as store.Deployment;
    }
    return {linkInfo, health, deployment, latest};
}

export default function OverviewTab({nodeId, onServicesChanged}: OverviewTabProps) {
    const [deployment, setDeployment] = useState<store.Deployment | null>(null);
    const [latestDeployment, setLatestDeployment] = useState<store.Deployment | null>(null);
    const [failedBuildLines, setFailedBuildLines] = useState<string[]>([]);
    const [health, setHealth] = useState<deploy.NodeHealth | null>(null);
    const [runtimeReady, setRuntimeReady] = useState(false);
    const [error, setError] = useState('');
    const [settings, setSettings] = useState<Record<string, string>>({});
    const [hasStagedChanges, setHasStagedChanges] = useState(false);
    const [staleness, setStaleness] = useState<deploy.ServiceStaleness | null>(null);
    const [localDomain, setLocalDomain] = useState<networking.LocalDomainStatus | null>(null);
    const [linkInfo, setLinkInfo] = useState<deploy.LinkedServiceInfo | null>(null);
    const {confirm} = useAppDialog();
    const buildLogRef = useRef<HTMLDivElement>(null);
    const failedLogRef = useRef<HTMLDivElement>(null);
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

    const [promoteCloneOpen, setPromoteCloneOpen] = useState(false);
    const [promoteConsistency, setPromoteConsistency] = useState<CloneConsistency>('consistent');
    const [unlinking, setUnlinking] = useState(false);
    const [promoting, setPromoting] = useState(false);
    const actionsBusy = !!pendingAction || promoting || unlinking;

    const applyRuntime = async (
        nodeIdToLoad: string,
        opts?: {clearActionError?: boolean; cancelled?: () => boolean},
    ) => {
        const isCancelled = () => opts?.cancelled?.() === true;
        const {linkInfo: link, health: h, deployment: d, latest} = await loadLinkedAwareRuntime(nodeIdToLoad);
        if (isCancelled()) return;

        setLinkInfo(link);
        setHealth(h);
        setDeployment(d);
        setLatestDeployment(latest);
        setRuntimeReady(true);

        const lastFailed = !!(h?.lastDeployFailed || latest?.status === 'failed');
        if (lastFailed) {
            const msg = latest?.error || h?.lastDeployError || 'Deployment failed';
            setError(msg);
            const failedId = latest?.id || h?.lastDeploymentId;
            if (failedId) {
                try {
                    const log = await GetBuildLog(failedId);
                    if (isCancelled()) return;
                    setFailedBuildLines(parseBuildLogText(log));
                } catch {
                    if (!isCancelled()) setFailedBuildLines([]);
                }
            } else {
                setFailedBuildLines([]);
            }
        } else {
            if (opts?.clearActionError !== false) {
                setError('');
            }
            setFailedBuildLines([]);
        }
    };

    useEffect(() => {
        let cancelled = false;
        setRuntimeReady(false);
        setHealth(null);
        setDeployment(null);
        setLatestDeployment(null);
        setFailedBuildLines([]);
        setLinkInfo(null);
        setError('');
        void applyRuntime(nodeId, {cancelled: () => cancelled});
        GetNodeConfigStatus(nodeId).then((status) => {
            if (cancelled) return;
            const applied = status?.appliedSettings || {};
            const staged = status?.stagedSettings || {};
            setSettings({...applied, ...staged});
            setHasStagedChanges(!!status?.hasStagedChanges);
        });
        GetServiceStaleness(nodeId).then(setStaleness).catch(() => setStaleness(null));
        GetLocalDomainStatus().then((s) => {
            if (!cancelled) setLocalDomain(s);
        }).catch(() => {
            if (!cancelled) setLocalDomain(null);
        });
        return () => { cancelled = true; };
    }, [nodeId]);

    useEffect(() => {
        if (version === 0) return;
        let cancelled = false;
        void applyRuntime(nodeId, {cancelled: () => cancelled}).then(() => {
            if (cancelled) return;
            GetLocalDomainStatus().then(setLocalDomain).catch(() => {});
        });
        GetNodeConfigStatus(nodeId).then((status) => {
            if (cancelled) return;
            const applied = status?.appliedSettings || {};
            const staged = status?.stagedSettings || {};
            setSettings({...applied, ...staged});
            setHasStagedChanges(!!status?.hasStagedChanges);
        });
        GetServiceStaleness(nodeId).then(setStaleness).catch(() => setStaleness(null));
        return () => { cancelled = true; };
    }, [nodeId, version]);

    // Keep linked Overview in sync when the *root* (or this node) changes status,
    // even if this panel never received a local deploy event.
    useEffect(() => {
        const rootId = linkInfo?.isLinked ? linkInfo.rootNodeId : '';
        const unsubscribe = EventsOn('deploy:status', (payload: any) => {
            const id: string | undefined = payload?.nodeId;
            if (!id) return;
            if (id !== nodeId && !(rootId && id === rootId)) return;
            void applyRuntime(nodeId);
        });
        return () => { unsubscribe(); };
    }, [nodeId, linkInfo?.isLinked, linkInfo?.rootNodeId]);

    useEffect(() => {
        if (autoScroll.current && buildLogRef.current) {
            buildLogRef.current.scrollTop = buildLogRef.current.scrollHeight;
        }
    }, [buildLines]);

    useEffect(() => {
        if (failedLogRef.current) {
            failedLogRef.current.scrollTop = failedLogRef.current.scrollHeight;
        }
    }, [failedBuildLines]);

    const handleBuildLogScroll = () => {
        if (!buildLogRef.current) return;
        const {scrollTop, scrollHeight, clientHeight} = buildLogRef.current;
        autoScroll.current = scrollHeight - scrollTop - clientHeight < 40;
    };

    // Build-mode services need a Dockerfile + port; image-mode services need an
    // image + port. Either path is deployable. Linked aliases use ensure-link deploy.
    const isLinked = !!linkInfo?.isLinked;
    const canDeploy = isLinked || (!!settings.service_port && (!!settings.dockerfile || !!settings.image));

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

    const runPromote = async (seed: 'empty' | 'clone', consistency: CloneConsistency) => {
        setError('');
        setPromoting(true);
        try {
            await PromoteLinkedService(nodeId, seed, consistency);
            await applyRuntime(nodeId);
            onServicesChanged?.();
            setPromoteCloneOpen(false);
        } catch (e: any) {
            setError(typeof e === 'string' ? e : e?.message || 'Promote failed');
        } finally {
            setPromoting(false);
        }
    };

    const handlePromoteEmpty = async () => {
        if (!await confirm({
            title: 'Promote to local service?',
            message: 'Stop sharing the root and make this an independent local service. You can deploy it afterward.',
            confirmLabel: 'Promote',
        })) return;
        await runPromote('empty', 'consistent');
    };

    const handlePromoteCloneConfirm = async () => {
        await runPromote('clone', promoteConsistency);
    };

    const runUnlink = async (become: 'fresh' | 'delete') => {
        setError('');
        setUnlinking(true);
        try {
            await UnlinkService(nodeId, become);
            onServicesChanged?.();
            if (become === 'fresh') {
                await applyRuntime(nodeId);
            }
        } catch (e: any) {
            setError(typeof e === 'string' ? e : e?.message || 'Unlink failed');
        } finally {
            setUnlinking(false);
        }
    };

    const handleUnlinkFresh = async () => {
        if (!await confirm({
            title: 'Stop sharing?',
            message: 'Disconnect from the root and keep this node as an empty local service. Deploy afterward to run your own container.',
            confirmLabel: 'Become independent',
        })) return;
        await runUnlink('fresh');
    };

    const handleUnlinkDelete = async () => {
        if (!await confirm({
            title: 'Remove linked service?',
            message: 'Delete this alias from the canvas. The root service in the other environment is not affected.',
            confirmLabel: 'Delete alias',
            danger: true,
        })) return;
        await runUnlink('delete');
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

    // Prefer GetNodeHealth: for linked aliases it mirrors the root and uses
    // alias DNS names; for normal nodes it matches the active deployment.
    // Until the first load finishes, avoid defaulting to "stopped" — that flash
    // is wrong when a service is already running.
    // Status is the live container; lastDeployFailed is a separate signal so a
    // failed rebuild does not hide an older still-running instance.
    const status = runtimeReady
        ? (health?.status || deployment?.status || 'stopped')
        : '';
    const lastDeployFailed = !!(
        health?.lastDeployFailed
        || latestDeployment?.status === 'failed'
    );
    const lastDeployError = latestDeployment?.error || health?.lastDeployError || error || 'Deployment failed';
    const isRunning = status === 'running';
    const isActive = status === 'building' || status === 'starting' || status === 'running';
    const previousStillRunning = lastDeployFailed && isRunning;
    const routeProtocol = (health?.routeProtocol || settings.route_protocol || 'http').toLowerCase();
    const isTCP = routeProtocol === 'tcp';
    const hostPort = health?.hostPort || deployment?.hostPort || 0;
    const displayHost = health?.hostname || deployment?.hostname || '';
    // Prefer LocalDomainStatus-backed URLs: they carry cached .draft vs resolv.sh
    // and the user's URL preference. health.publicUrl is the daemon fallback.
    const publicURL = bestPublicEndpoint(displayHost, hostPort, localDomain, routeProtocol)
        || health?.publicUrl
        || bestPublicDeploymentURL(deployment, localDomain, routeProtocol);
    // TCP: scheme-less loopback endpoint (wire clients). HTTP: browser-openable URL.
    const localURL = hostPort > 0
        ? (isTCP ? `127.0.0.1:${hostPort}` : `http://127.0.0.1:${hostPort}`)
        : '';
    const displayHostname = displayHost;

    // Live stream while building; otherwise prefer the failed attempt's log so
    // Overview still shows what went wrong after reload / when a prior container runs.
    const showLiveBuild = deploying || (buildLines.length > 0 && !lastDeployFailed);
    const showFailedBuild = lastDeployFailed && !deploying;
    const failedLogLines = failedBuildLines.length > 0
        ? failedBuildLines
        : (lastDeployFailed && buildLines.length > 0 ? buildLines : []);
    const failedSeq = latestDeployment?.sequence || health?.lastDeploySequence || 0;

    const handleOpenDeployment = () => {
        if (!publicURL || isTCP) return;
        BrowserOpenURL(publicURL.startsWith('http') ? publicURL : `http://${publicURL}`);
    };

    const handleOpenLocal = () => {
        if (!localURL || isTCP) return;
        BrowserOpenURL(localURL.startsWith('http') ? localURL : `http://${localURL}`);
    };

    const displayEndpoint = (url: string) => url.replace(/^https?:\/\//, '');

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
                setRunOutput(truncateOutput(res.output || ''));
                setRunExit(res.exitCode);
                if (res.error) setRunOutput((prev) => truncateOutput((prev ? prev + '\n' : '') + `[error] ${res.error}`));
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
                {runtimeReady && status ? (
                    <>
                        <StatusBadge status={status} />
                        {lastDeployFailed && status !== 'failed' && (
                            <StatusBadge status="failed" />
                        )}
                    </>
                ) : (
                    <span className="status-badge status-badge--pending" aria-busy="true">
                        <Loader2 size={11} className="spin" />
                        loading
                    </span>
                )}
                {publicURL && isRunning && (
                    isTCP ? (
                        <span
                            className="overview-hostname"
                            title={`TCP endpoint (not HTTP). Connect with a protocol client: ${publicURL}`}
                        >
                            {displayEndpoint(publicURL)}
                        </span>
                    ) : (
                        <button
                            type="button"
                            className="overview-hostname"
                            title={publicURL}
                            onClick={handleOpenDeployment}
                        >
                            <ExternalLink size={11} />
                            {displayEndpoint(publicURL)}
                        </button>
                    )
                )}
            </div>

            {hasStagedChanges && (
                <div className="overview-staged-banner">
                    Staged settings will apply on the next deploy.
                </div>
            )}

            {staleness?.stale && (
                <div className="overview-staged-banner overview-stale-banner">
                    <AlertCircle size={13} />
                    <span>
                        Running with stale {staleness.needsRebuild ? 'build or runtime' : 'runtime'} configuration
                        {staleness.inputs.length ? `: ${staleness.inputs.map((input) => input.key).join(', ')}` : ''}.
                        {' '}Restart will not apply these values; deploy this service instead.
                    </span>
                </div>
            )}

            {lastDeployFailed && (
                <div className="overview-deploy-failure">
                    <div className="overview-error">
                        <AlertCircle size={13} />
                        <span>
                            {previousStillRunning
                                ? `Latest deploy failed${failedSeq ? ` (#${failedSeq})` : ''}; previous instance is still running. `
                                : `Deploy failed${failedSeq ? ` (#${failedSeq})` : ''}. `}
                            {lastDeployError}
                        </span>
                    </div>
                    {showFailedBuild && (
                        <div className="overview-build-log overview-build-log--failed">
                            <h4 className="deploy-log-title">Failed build output</h4>
                            <div className="log-viewer log-viewer--build" ref={failedLogRef}>
                                {failedLogLines.length === 0 ? (
                                    <span className="deploy-empty">No build log available for this failure.</span>
                                ) : (
                                    failedLogLines.map((line, i) => (
                                        <div
                                            key={i}
                                            className={`log-line${/error|failed|ERROR/i.test(line) ? ' log-line--error' : ''}`}
                                        >
                                            {line}
                                        </div>
                                    ))
                                )}
                            </div>
                        </div>
                    )}
                </div>
            )}

            {!lastDeployFailed && error && (
                <div className="overview-error">
                    <AlertCircle size={13} />
                    <span>{error}</span>
                </div>
            )}

            {isLinked && (
                <div className="overview-staged-banner">
                    Linked to <strong>{linkInfo?.rootEnvName || 'another environment'}</strong>
                    {linkInfo?.rootLabel ? ` · ${linkInfo.rootLabel}` : ''}. No local container —
                    health and runtime follow the root. Deploy re-attaches networks only.
                </div>
            )}

            {!isLinked && (linkInfo?.linkers?.length ?? 0) > 0 && (
                <div className="overview-staged-banner">
                    Shared root for <strong>{linkInfo!.linkers!.length}</strong> alias
                    {linkInfo!.linkers!.length === 1 ? '' : 'es'}:{' '}
                    {linkInfo!.linkers!.map((l) => `${l.environment || 'env'} · ${l.label}`).join(', ')}.
                    Delete or promote those aliases before removing this service.
                </div>
            )}

            <div className="overview-actions">
                {isLinked && (
                    <>
                        <button className="btn btn-ghost" onClick={handleDeploy} disabled={actionsBusy} title="Re-attach root to this environment network">
                            {pendingAction === 'deploying' ? <Loader2 size={13} className="spin" /> : <Play size={13} />}
                            Sync link
                        </button>
                        <button className="btn btn-primary" onClick={() => void handlePromoteEmpty()} disabled={actionsBusy}>
                            {promoting && !promoteCloneOpen ? <Loader2 size={13} className="spin" /> : null}
                            {promoting && !promoteCloneOpen ? 'Promoting…' : 'Promote'}
                        </button>
                        {!!linkInfo?.hasVolumes && (
                            <button
                                className="btn btn-ghost"
                                onClick={() => {
                                    setPromoteConsistency('consistent');
                                    setPromoteCloneOpen(true);
                                }}
                                disabled={actionsBusy}
                            >
                                {promoting && promoteCloneOpen ? <Loader2 size={13} className="spin" /> : null}
                                {promoting && promoteCloneOpen ? 'Cloning…' : 'Promote + clone data'}
                            </button>
                        )}
                        <button className="btn btn-ghost" onClick={() => void handleUnlinkFresh()} disabled={actionsBusy} title="Disconnect and keep an empty local service">
                            {unlinking ? <Loader2 size={13} className="spin" /> : null}
                            {unlinking ? 'Unlinking…' : 'Unlink'}
                        </button>
                        <button className="btn btn-ghost" onClick={() => void handleUnlinkDelete()} disabled={actionsBusy} title="Remove this alias from the canvas">
                            Delete alias
                        </button>
                    </>
                )}
                {!isLinked && runtimeReady && !isActive && !deploying && (
                    <button
                        className="btn btn-primary"
                        onClick={handleDeploy}
                        disabled={!canDeploy || actionsBusy}
                        title={!canDeploy ? 'Set an image or Dockerfile and a port in Settings first' : 'Deploy service'}
                    >
                        {pendingAction === 'deploying' ? <Loader2 size={13} className="spin" /> : <Play size={13} />}
                        {pendingAction === 'deploying' ? 'Deploying…' : 'Deploy'}
                    </button>
                )}
                {deploying && (
                    <button className="btn btn-ghost" onClick={handleStop} disabled={actionsBusy}>
                        {pendingAction === 'stopping' ? <Loader2 size={13} className="spin" /> : <Square size={13} />}
                        {pendingAction === 'stopping' ? 'Cancelling…' : 'Cancel Build'}
                    </button>
                )}
                {!isLinked && runtimeReady && isActive && !deploying && (
                    <>
                        <button className="btn btn-ghost" onClick={handleStop} disabled={actionsBusy}>
                            {pendingAction === 'stopping' ? <Loader2 size={13} className="spin" /> : <Square size={13} />}
                            {pendingAction === 'stopping' ? 'Stopping…' : 'Stop'}
                        </button>
                        <button className="btn btn-ghost" onClick={handleRestart} disabled={actionsBusy}>
                            {pendingAction === 'restarting' ? <Loader2 size={13} className="spin" /> : <RotateCcw size={13} />}
                            {pendingAction === 'restarting' ? 'Restarting…' : 'Restart'}
                        </button>
                        <button className="btn btn-primary" onClick={handleDeploy} disabled={actionsBusy}>
                            {pendingAction === 'deploying' ? <Loader2 size={13} className="spin" /> : <Play size={13} />}
                            {pendingAction === 'deploying' ? 'Rebuilding…' : 'Rebuild'}
                        </button>
                    </>
                )}
            </div>

            {!isLinked && isRunning && (
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

            {showLiveBuild && (
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

            {(deployment || displayHostname || hostPort > 0) && (
                <div className="overview-details">
                    {deployment?.imageTag && (
                        <div className="overview-detail-row">
                            <span className="overview-detail-label">Image</span>
                            <span className="overview-detail-value mono">{deployment.imageTag}</span>
                        </div>
                    )}
                    {deployment?.containerId && (
                        <div className="overview-detail-row">
                            <span className="overview-detail-label">Container</span>
                            <span className="overview-detail-value mono">{deployment.containerId.slice(0, 12)}</span>
                        </div>
                    )}
                    {isLinked && linkInfo?.rootEnvName && (
                        <div className="overview-detail-row">
                            <span className="overview-detail-label">Shared Root</span>
                            <span className="overview-detail-value mono">
                                {linkInfo.rootEnvName}{linkInfo.rootLabel ? ` · ${linkInfo.rootLabel}` : ''}
                            </span>
                        </div>
                    )}
                    {hostPort > 0 && (
                        <div className="overview-detail-row">
                            <span className="overview-detail-label">Mapped Host Port</span>
                            <span className="overview-detail-value mono">{hostPort}</span>
                        </div>
                    )}
                    {displayHostname && (
                        <div className="overview-detail-row">
                            <span className="overview-detail-label">Internal Hostname</span>
                            <span className="overview-detail-value mono">{displayHostname}</span>
                        </div>
                    )}
                    {publicURL && (
                        <div className="overview-detail-row">
                            <span className="overview-detail-label">{isTCP ? 'Public Endpoint' : 'Public URL'}</span>
                            <span className="overview-detail-value mono">{displayEndpoint(publicURL)}</span>
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
                            <span className="overview-detail-label">{isTCP ? 'Local Endpoint' : 'Local URL'}</span>
                            {isTCP ? (
                                <span
                                    className="overview-detail-value mono"
                                    title="TCP host port. Use with postgres://, redis://, etc. — not a browser URL."
                                >
                                    {displayEndpoint(localURL)}
                                </span>
                            ) : (
                                <button
                                    type="button"
                                    className="overview-detail-value mono overview-detail-link"
                                    title="Direct loopback address. Works without internet — use this for apps running on your machine (e.g. a desktop client)."
                                    onClick={handleOpenLocal}
                                >
                                    {displayEndpoint(localURL)}
                                </button>
                            )}
                        </div>
                    )}
                    {localDomain?.hostsError && !isTCP && (
                        <div className="overview-domain-note">
                            Internal Draft names are not host-resolved. Public access uses the configured Draft public hostname with the proxy port.
                        </div>
                    )}
                    {isTCP && (
                        <div className="overview-domain-note">
                            TCP service: connect via hostname:port (or a protocol DSN). Traffic is not HTTP-proxied.
                        </div>
                    )}
                </div>
            )}

            {promoteCloneOpen && (
                <Dialog
                    title="Promote + clone data"
                    onClose={() => {
                        if (!promoting) setPromoteCloneOpen(false);
                    }}
                    footer={
                        <>
                            <button
                                className="btn btn-ghost"
                                onClick={() => setPromoteCloneOpen(false)}
                                disabled={promoting}
                            >
                                Cancel
                            </button>
                            <button
                                className="btn btn-primary"
                                onClick={() => void handlePromoteCloneConfirm()}
                                disabled={promoting}
                            >
                                {promoting ? <Loader2 size={13} className="spin" /> : null}
                                {promoting ? 'Cloning…' : 'Promote'}
                            </button>
                        </>
                    }
                >
                    <div className="dialog-copy">
                        <p className="dialog-message">
                            Create local volumes and copy data from{' '}
                            <strong>{linkInfo?.rootLabel || 'the root service'}</strong>
                            {linkInfo?.rootEnvName ? ` (${linkInfo.rootEnvName})` : ''}.
                            Deploy afterward to start an independent container.
                        </p>
                        <div className="form-field" style={{marginTop: 12}}>
                            <label className="form-label" htmlFor="promote-clone-consistency">Consistency</label>
                            <select
                                id="promote-clone-consistency"
                                className="input settings-select"
                                value={promoteConsistency}
                                disabled={promoting}
                                onChange={(e) => setPromoteConsistency(e.target.value as CloneConsistency)}
                            >
                                <option value="consistent">Consistent (stop source during copy)</option>
                                <option value="quick">Quick (source may keep running)</option>
                            </select>
                            <span className="settings-hint">
                                Consistent briefly stops the shared root so the copy is point-in-time.
                                Quick keeps the root running and may copy mid-write.
                            </span>
                        </div>
                    </div>
                </Dialog>
            )}
        </div>
    );
}
