import {Activity, AlertCircle, Clock3, HeartPulse, Package, RotateCcw, ServerCrash, Wifi} from 'lucide-react';
import {useCallback, useEffect, useRef, useState, type ReactNode} from 'react';
import {GetServiceMetrics} from '../../wailsjs/go/main/App';
import {deploy} from '../../wailsjs/go/models';
import StatusBadge from './StatusBadge';
import {useLinkedServiceTarget} from '../lib/linkedService';
import {Skeleton, SkeletonStats} from './Skeleton';
import './MetricsTab.css';

function MetricsSkeleton() {
    return (
        <div className="metrics-tab">
            <div className="metrics-header">
                <div className="metrics-header-copy">
                    <div className="metrics-title-row">
                        <Skeleton width={140} height={16} />
                        <Skeleton width={64} height={20} variant="pill" />
                    </div>
                    <Skeleton width={200} height={11} style={{marginTop: 8}} />
                </div>
            </div>
            <SkeletonStats count={6} columns={3} />
            <div className="metrics-charts">
                <Skeleton height={140} />
                <Skeleton height={140} />
            </div>
        </div>
    );
}

export default function MetricsTab({nodeId}: {nodeId: string}) {
    const [metrics, setMetrics] = useState<deploy.ServiceMetrics | null>(null);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState('');
    const {loading: linkLoading, isLinked, linkInfo, targetNodeId} = useLinkedServiceTarget(nodeId);

    useEffect(() => {
        setMetrics(null);
        setLoading(true);
        setError('');
    }, [nodeId, targetNodeId]);

    useEffect(() => {
        if (linkLoading) return;
        let cancelled = false;
        let intervalId: number | null = null;

        const load = async (initial = false) => {
            if (initial) setLoading(true);
            try {
                const next = await GetServiceMetrics(targetNodeId);
                if (cancelled) return;
                setMetrics(next);
                setError('');
            } catch (e: any) {
                if (cancelled) return;
                setError(typeof e === 'string' ? e : e?.message || 'Could not load metrics.');
            } finally {
                if (!cancelled && initial) setLoading(false);
            }
        };

        void load(true);
        intervalId = window.setInterval(() => void load(false), 3000);
        return () => {
            cancelled = true;
            if (intervalId !== null) window.clearInterval(intervalId);
        };
    }, [targetNodeId, linkLoading]);

    if (loading && !metrics) {
        return <MetricsSkeleton />;
    }

    if (error && !metrics) {
        return (
            <div className="metrics-tab metrics-tab--empty">
                <div className="metrics-callout metrics-callout--error">
                    <AlertCircle size={14} />
                    <span>{error}</span>
                </div>
            </div>
        );
    }

    if (!metrics) {
        return <div className="metrics-tab metrics-tab--empty">No metrics available.</div>;
    }

    // Defensive fallbacks: when metrics are off, unavailable, or partial, the
    // backend may omit nested objects/arrays (nil slices serialize to null),
    // and .map()/.length/property access on those would crash the whole tab.
    const summary = metrics.deploymentSummary ?? ({} as deploy.DeploymentHistorySummary);
    const reachability = metrics.reachability ?? ({status: 'not_running'} as deploy.ReachabilityCheck);
    const livePoints = metrics.livePoints ?? [];
    const events = metrics.events ?? [];
    const recentDeployments = metrics.recentDeployments ?? [];
    const meaningful = (summary.successCount ?? 0) + (summary.failureCount ?? 0);
    const successRate = meaningful > 0
        ? Math.round(((summary.successCount ?? 0) / meaningful) * 100)
        : 0;

    return (
        <div className="metrics-tab">
            <div className="metrics-header">
                <div className="metrics-header-copy">
                    <div className="metrics-title-row">
                        <span className="metrics-title">Runtime Metrics</span>
                        <StatusBadge status={metrics.status} />
                    </div>
                    <span className="metrics-subtitle">
                        {metrics.serviceType} service · {metrics.currentDeployment ? `deployment #${metrics.currentDeployment.id}` : 'no active deployment'}
                    </span>
                </div>
                {reachability.status === 'healthy' && (
                    <div className="metrics-health-chip">
                        <HeartPulse size={13} />
                        Reachable in {reachability.latencyMs}ms
                    </div>
                )}
            </div>

            {isLinked && (
                <div className="metrics-callout">
                    <AlertCircle size={14} />
                    <span>
                        Showing runtime metrics from the shared root service in {linkInfo?.rootEnvName || 'another environment'}
                        {linkInfo?.rootLabel ? ` · ${linkInfo.rootLabel}` : ''}.
                    </span>
                </div>
            )}

            {error && (
                <div className="metrics-callout metrics-callout--error">
                    <AlertCircle size={14} />
                    <span>{error}</span>
                </div>
            )}

            {metrics.liveMetricsError && metrics.status === 'running' && (
                <div className="metrics-callout">
                    <AlertCircle size={14} />
                    <span>{metrics.liveMetricsError}</span>
                </div>
            )}

            <div className="metrics-kpis">
                <MetricCard
                    icon={<Activity size={14} />}
                    label="CPU"
                    value={metrics.latestPoint ? formatPercent(metrics.latestPoint.cpuPercent) : 'No data'}
                    note={metrics.latestPoint ? 'Current container usage' : 'Available while running'}
                />
                <MetricCard
                    icon={<ServerCrash size={14} />}
                    label="Memory"
                    value={metrics.latestPoint ? formatBytes(metrics.latestPoint.memoryBytes) : 'No data'}
                    note={metrics.latestPoint && metrics.latestPoint.memoryLimitBytes > 0
                        ? `${Math.round((metrics.latestPoint.memoryBytes / metrics.latestPoint.memoryLimitBytes) * 100)}% of limit`
                        : 'Available while running'}
                />
                <MetricCard
                    icon={<Clock3 size={14} />}
                    label="Uptime"
                    value={metrics.uptimeMs > 0 ? formatDuration(metrics.uptimeMs) : 'Not running'}
                    note={metrics.currentDeployment?.containerStartedAt ? `Since ${new Date(metrics.currentDeployment.containerStartedAt).toLocaleString()}` : 'Last-known runtime'}
                />
                <MetricCard
                    icon={<Wifi size={14} />}
                    label="Reachability"
                    value={reachabilityLabel(reachability)}
                    note={reachabilityNote(metrics)}
                />
                <MetricCard
                    icon={<RotateCcw size={14} />}
                    label="Recent Success"
                    value={`${successRate}%`}
                    note={`${summary.successCount} succeeded / ${summary.failureCount} failed${summary.interruptedCount ? ` / ${summary.interruptedCount} interrupted` : ''} in last ${summary.recentWindow}`}
                />
                <MetricCard
                    icon={<Package size={14} />}
                    label="Container"
                    value={metrics.currentDeployment?.containerId ? metrics.currentDeployment.containerId.slice(0, 12) : 'None'}
                    note={metrics.hostPort > 0 ? `127.0.0.1:${metrics.hostPort}` : metrics.hostname || 'No route yet'}
                />
            </div>

            <div className="metrics-charts">
                <MetricChart
                    title="CPU Usage"
                    value={metrics.latestPoint ? formatPercent(metrics.latestPoint.cpuPercent) : 'No live CPU data'}
                    points={livePoints}
                    pickValue={(point) => point.cpuPercent}
                    formatValue={formatPercent}
                    tone="cpu"
                />
                <MetricChart
                    title="Memory Usage"
                    value={metrics.latestPoint ? formatBytes(metrics.latestPoint.memoryBytes) : 'No live memory data'}
                    points={livePoints}
                    pickValue={(point) => point.memoryBytes}
                    formatValue={formatBytes}
                    tone="memory"
                />
                <MetricChart
                    title="Network Throughput"
                    value={metrics.latestPoint ? `${formatRate(metrics.latestPoint.networkRxRateBps)} in · ${formatRate(metrics.latestPoint.networkTxRateBps)} out` : 'No live network data'}
                    points={livePoints}
                    pickValue={(point) => point.networkRxRateBps + point.networkTxRateBps}
                    formatValue={formatRate}
                    tone="network"
                />
            </div>

            <div className="metrics-grid">
                <section className="metrics-panel">
                    <div className="metrics-panel-header">
                        <span>Health & Route</span>
                    </div>
                    <div className="metrics-facts">
                        <FactRow label="Reachability" value={reachabilityLabel(reachability)} />
                        <FactRow label="Target URL" value={reachability.targetUrl || metrics.publicUrl || metrics.internalUrl || 'None'} mono />
                        <FactRow label="HTTP Status" value={reachability.statusCode ? String(reachability.statusCode) : '—'} mono />
                        <FactRow label="Docker Health" value={metrics.dockerHealth || 'No healthcheck'} />
                        <FactRow label="Hostname" value={metrics.hostname || 'None'} mono />
                        <FactRow label="Desired Port" value={metrics.desiredPort ? String(metrics.desiredPort) : 'Unset'} mono />
                        <FactRow label="Host Port" value={metrics.hostPort ? String(metrics.hostPort) : 'Unassigned'} mono />
                    </div>
                </section>

                <section className="metrics-panel">
                    <div className="metrics-panel-header">
                        <span>Deployment Quality</span>
                    </div>
                    <div className="metrics-facts">
                        <FactRow label="Last Build" value={formatMaybeDuration(summary.lastBuildDurationMs)} />
                        <FactRow label="Last Boot" value={formatMaybeDuration(summary.lastBootDurationMs)} />
                        <FactRow label="Last Run" value={formatMaybeDuration(summary.lastRunDurationMs)} />
                        <FactRow label="Total Deployments" value={String(summary.totalDeployments)} mono />
                        <FactRow label="Last Failure" value={summary.lastFailureAt ? relativeTime(summary.lastFailureAt) : 'None'} />
                        <FactRow label="Failure Reason" value={summary.lastFailureReason || '—'} />
                    </div>
                </section>

                <section className="metrics-panel">
                    <div className="metrics-panel-header">
                        <span>Container Facts</span>
                    </div>
                    <div className="metrics-facts">
                        <FactRow label="Restarts" value={String(metrics.restartCount)} mono />
                        <FactRow label="Exit Code" value={metrics.exitCode !== undefined ? String(metrics.exitCode) : '—'} mono />
                        <FactRow label="OOM Killed" value={metrics.oomKilled ? 'Yes' : 'No'} />
                        <FactRow label="Image Size" value={metrics.imageSizeBytes > 0 ? formatBytes(metrics.imageSizeBytes) : '—'} />
                        <FactRow label="Writable Layer" value={metrics.writableSizeBytes > 0 ? formatBytes(metrics.writableSizeBytes) : '—'} />
                        <FactRow label="Image Tag" value={metrics.currentDeployment?.imageTag || '—'} mono />
                    </div>
                </section>
            </div>

            <div className="metrics-grid metrics-grid--bottom">
                <section className="metrics-panel">
                    <div className="metrics-panel-header">
                        <span>Recent Runtime Events</span>
                    </div>
                    {events.length === 0 ? (
                        <span className="metrics-empty">Events will appear after the first deployment.</span>
                    ) : (
                        <div className="metrics-events">
                            {events.map((event, index) => (
                                <div key={`${event.kind}-${event.at}-${index}`} className={`metrics-event metrics-event--${event.severity}`}>
                                    <div className="metrics-event-dot" />
                                    <div className="metrics-event-copy">
                                        <span className="metrics-event-summary">{event.summary}</span>
                                        <span className="metrics-event-time">{new Date(event.at).toLocaleString()}</span>
                                    </div>
                                </div>
                            ))}
                        </div>
                    )}
                </section>

                <section className="metrics-panel">
                    <div className="metrics-panel-header">
                        <span>Recent Deployments</span>
                    </div>
                    {recentDeployments.length === 0 ? (
                        <span className="metrics-empty">No deployments yet.</span>
                    ) : (
                        <div className="metrics-deployments">
                            {recentDeployments.map((deployment) => (
                                <div key={deployment.deploymentId} className="metrics-deployment-row">
                                    <div className="metrics-deployment-main">
                                        <div className="metrics-deployment-title">
                                            <StatusBadge status={deployment.status} />
                                            <span className="metrics-deployment-tag mono">{deployment.imageTag || `#${deployment.deploymentId}`}</span>
                                        </div>
                                        <span className="metrics-deployment-time">{new Date(deployment.createdAt).toLocaleString()}</span>
                                    </div>
                                    <div className="metrics-deployment-stats">
                                        <span>{formatMaybeDuration(deployment.buildDurationMs)} build</span>
                                        <span>{formatMaybeDuration(deployment.bootDurationMs)} boot</span>
                                        <span>{formatMaybeDuration(deployment.runDurationMs)} run</span>
                                        {deployment.exitCode !== undefined && <span>exit {deployment.exitCode}</span>}
                                    </div>
                                    {deployment.error && (
                                        <span className="metrics-deployment-error">{deployment.error}</span>
                                    )}
                                </div>
                            ))}
                        </div>
                    )}
                </section>
            </div>
        </div>
    );
}

function MetricCard({icon, label, value, note}: {icon: ReactNode; label: string; value: string; note: string}) {
    return (
        <div className="metrics-card">
            <div className="metrics-card-label">
                {icon}
                <span>{label}</span>
            </div>
            <span className="metrics-card-value">{value}</span>
            <span className="metrics-card-note">{note}</span>
        </div>
    );
}

function MetricChart({
    title,
    value,
    points,
    pickValue,
    formatValue,
    tone,
}: {
    title: string;
    value: string;
    points: deploy.MetricPoint[];
    pickValue: (point: deploy.MetricPoint) => number;
    formatValue: (v: number) => string;
    tone: 'cpu' | 'memory' | 'network';
}) {
    const chartPoints = (points ?? []).map((point) => pickValue(point));
    const path = toSparkline(chartPoints, 260, 96);
    const svgRef = useRef<SVGSVGElement>(null);
    const [hover, setHover] = useState<{x: number; index: number} | null>(null);

    const handleMouseMove = useCallback((e: React.MouseEvent<SVGSVGElement>) => {
        const svg = svgRef.current;
        if (!svg || chartPoints.length < 2) return;
        const rect = svg.getBoundingClientRect();
        const relX = (e.clientX - rect.left) / rect.width;
        const index = Math.round(relX * (chartPoints.length - 1));
        const clampedIndex = Math.max(0, Math.min(chartPoints.length - 1, index));
        const x = (clampedIndex / (chartPoints.length - 1)) * 260;
        setHover({x, index: clampedIndex});
    }, [chartPoints.length]);

    const handleMouseLeave = useCallback(() => setHover(null), []);

    const hoveredValue = hover !== null ? chartPoints[hover.index] : null;

    return (
        <section className="metrics-panel metrics-panel--chart">
            <div className="metrics-panel-header">
                <span>{title}</span>
                <span className="metrics-chart-value">
                    {hoveredValue !== null ? formatValue(hoveredValue) : value}
                </span>
            </div>
            {path ? (
                <svg
                    ref={svgRef}
                    className={`metrics-chart metrics-chart--${tone}`}
                    viewBox="0 0 260 96"
                    preserveAspectRatio="none"
                    onMouseMove={handleMouseMove}
                    onMouseLeave={handleMouseLeave}
                >
                    <path className="metrics-chart-area" d={`${path} L260 96 L0 96 Z`} />
                    <path className="metrics-chart-line" d={path} />
                    {hover !== null && (
                        <line
                            className="metrics-chart-crosshair"
                            x1={hover.x}
                            y1={0}
                            x2={hover.x}
                            y2={96}
                        />
                    )}
                    {hover !== null && (
                        <circle
                            className={`metrics-chart-dot metrics-chart-dot--${tone}`}
                            cx={hover.x}
                            cy={getYForIndex(chartPoints, hover.index, 260, 96)}
                            r={4}
                        />
                    )}
                </svg>
            ) : (
                <div className="metrics-chart-empty">Live metrics appear while the container is running.</div>
            )}
        </section>
    );
}

function getYForIndex(values: number[], index: number, _width: number, height: number): number {
    const max = Math.max(...values, 1);
    const min = Math.min(...values, 0);
    const range = max - min || 1;
    return height - ((values[index] - min) / range) * (height - 6) - 3;
}

function FactRow({label, value, mono = false}: {label: string; value: string; mono?: boolean}) {
    return (
        <div className="metrics-fact-row">
            <span className="metrics-fact-label">{label}</span>
            <span className={`metrics-fact-value ${mono ? 'mono' : ''}`}>{value}</span>
        </div>
    );
}

function toSparkline(values: number[], width: number, height: number) {
    if (values.length < 2) return '';
    const max = Math.max(...values, 1);
    const min = Math.min(...values, 0);
    const range = max - min || 1;
    return values.map((value, index) => {
        const x = (index / (values.length - 1)) * width;
        const y = height - ((value - min) / range) * (height - 6) - 3;
        return `${index === 0 ? 'M' : 'L'}${x.toFixed(2)} ${y.toFixed(2)}`;
    }).join(' ');
}

function formatPercent(value: number) {
    return `${value.toFixed(value >= 10 ? 0 : 1)}%`;
}

function formatBytes(value: number) {
    if (!value) return '0 B';
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    let next = value;
    let unit = 0;
    while (next >= 1024 && unit < units.length - 1) {
        next /= 1024;
        unit++;
    }
    return `${next.toFixed(next >= 10 || unit === 0 ? 0 : 1)} ${units[unit]}`;
}

function formatRate(value: number) {
    return `${formatBytes(value)}/s`;
}

function formatDuration(valueMs: number) {
    const totalSeconds = Math.max(0, Math.floor(valueMs / 1000));
    const hours = Math.floor(totalSeconds / 3600);
    const minutes = Math.floor((totalSeconds % 3600) / 60);
    const seconds = totalSeconds % 60;
    if (hours > 0) return `${hours}h ${minutes}m`;
    if (minutes > 0) return `${minutes}m ${seconds}s`;
    return `${seconds}s`;
}

function formatMaybeDuration(valueMs: number) {
    return valueMs > 0 ? formatDuration(valueMs) : '—';
}

function relativeTime(value: any) {
    const date = new Date(value);
    const delta = Date.now() - date.getTime();
    const minutes = Math.floor(delta / 60000);
    if (minutes < 1) return 'Just now';
    if (minutes < 60) return `${minutes}m ago`;
    const hours = Math.floor(minutes / 60);
    if (hours < 24) return `${hours}h ago`;
    const days = Math.floor(hours / 24);
    return `${days}d ago`;
}

function reachabilityLabel(reachability: deploy.ReachabilityCheck) {
    switch (reachability.status) {
        case 'healthy':
            return 'Healthy';
        case 'degraded':
            return 'Responding with errors';
        case 'reachable':
            return 'Reachable';
        case 'not_applicable':
            return 'Not applicable';
        case 'not_running':
            return 'Not running';
        default:
            return 'Unreachable';
    }
}

function reachabilityNote(metrics: deploy.ServiceMetrics) {
    const reachability = metrics.reachability ?? ({status: 'not_running'} as deploy.ReachabilityCheck);
    if (reachability.status === 'healthy') {
        return `${reachability.latencyMs}ms from localhost`;
    }
    if (reachability.status === 'degraded') {
        return reachability.statusCode ? `HTTP ${reachability.statusCode}` : 'Request completed with an error status';
    }
    if (reachability.status === 'reachable') {
        return 'Port is open (TCP), no HTTP response';
    }
    if (reachability.status === 'not_applicable') {
        return 'No published port to probe';
    }
    if (reachability.error) {
        return reachability.error;
    }
    return metrics.publicUrl || metrics.internalUrl || 'No reachable endpoint';
}
