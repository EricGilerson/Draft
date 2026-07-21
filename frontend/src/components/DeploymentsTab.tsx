import {ChevronDown, ChevronRight, AlertCircle, GitCommitHorizontal, RotateCw} from 'lucide-react';
import {useCallback, useEffect, useRef, useState} from 'react';
import {GetDeploymentsPage, GetBuildLog, RollbackDeployment, RollbackEligibility} from '../../wailsjs/go/main/App';
import {store, deploy} from '../../wailsjs/go/models';
import {useAppDialog} from './AppDialogProvider';
import {useBuildLog} from './BuildLogProvider';
import StatusBadge from './StatusBadge';
import {Skeleton} from './Skeleton';

const PAGE_SIZE = 50;

/** Short form for list rows; full SHA stays in title/tooltip. */
function shortSha(sha?: string): string {
    return (sha ?? '').trim().slice(0, 7);
}

function DeployHistorySkeleton() {
    return (
        <>
            {Array.from({length: 4}, (_, i) => (
                <div key={i} className="deploy-entry">
                    <div className="deploy-entry-header">
                        <div className="deploy-entry-leading">
                            <Skeleton width={13} height={13} />
                            <Skeleton width={64} height={20} variant="pill" />
                            <Skeleton width={i % 2 === 0 ? 100 : 70} height={12} />
                        </div>
                        <div className="deploy-entry-actions">
                            <Skeleton width={90} height={12} />
                            <Skeleton width={84} height={26} variant="pill" />
                        </div>
                    </div>
                </div>
            ))}
        </>
    );
}

export default function DeploymentsTab({nodeId}: {nodeId: string}) {
    const [deployments, setDeployments] = useState<store.Deployment[]>([]);
    const [total, setTotal] = useState(0);
    const [hasMore, setHasMore] = useState(false);
    const [loading, setLoading] = useState(true);
    const [loadingMore, setLoadingMore] = useState(false);
    const [expandedId, setExpandedId] = useState<number | null>(null);
    const [buildLog, setBuildLog] = useState('');
    const [eligibility, setEligibility] = useState<Record<number, deploy.RollbackEligibility>>({});
    const [rollingBack, setRollingBack] = useState<number | null>(null);
    const [rollbackError, setRollbackError] = useState<string | null>(null);
    const buildLogRef = useRef<HTMLDivElement>(null);
    const {lines: liveBuildLines, deploying, version} = useBuildLog(nodeId);
    const {confirm} = useAppDialog();

    const refreshEligibility = useCallback(() => {
        RollbackEligibility(nodeId)
            .then((items) => {
                const map: Record<number, deploy.RollbackEligibility> = {};
                for (const item of items ?? []) map[item.deploymentId] = item;
                setEligibility(map);
            })
            .catch(() => setEligibility({}));
    }, [nodeId]);

    const applyPage = useCallback((page: deploy.DeploymentListPage, append: boolean) => {
        const rows = page.deployments ?? [];
        setDeployments((prev) => {
            if (!append) return rows;
            const seen = new Set(prev.map((d) => d.id));
            return [...prev, ...rows.filter((d) => !seen.has(d.id))];
        });
        setTotal(page.total ?? 0);
        setHasMore(!!page.hasMore);
    }, []);

    const loadPage = useCallback(async (offset: number, append: boolean) => {
        if (append) setLoadingMore(true);
        else setLoading(true);
        try {
            const page = await GetDeploymentsPage(nodeId, PAGE_SIZE, offset);
            applyPage(page ?? deploy.DeploymentListPage.createFrom({deployments: [], total: 0, limit: PAGE_SIZE, offset, hasMore: false}), append);
        } finally {
            if (append) setLoadingMore(false);
            else setLoading(false);
        }
    }, [nodeId, applyPage]);

    useEffect(() => {
        setDeployments([]);
        setTotal(0);
        setHasMore(false);
        setExpandedId(null);
        setBuildLog('');
        void loadPage(0, false);
        refreshEligibility();
    }, [nodeId, loadPage, refreshEligibility]);

    useEffect(() => {
        if (version === 0) return;
        void loadPage(0, false);
        refreshEligibility();
    }, [nodeId, version, loadPage, refreshEligibility]);

    useEffect(() => {
        if (buildLogRef.current) {
            buildLogRef.current.scrollTop = buildLogRef.current.scrollHeight;
        }
    }, [liveBuildLines]);

    const isBuilding = deploying || deployments.some(d => d.status === 'building');

    const toggleExpand = async (dep: store.Deployment) => {
        if (expandedId === dep.id) {
            setExpandedId(null);
            setBuildLog('');
            return;
        }
        setExpandedId(dep.id);
        setBuildLog('');
        const log = await GetBuildLog(dep.id);
        // Cap rendered history so expanding an old fat build does not freeze the tab.
        const lines = (log || '').split('\n').filter(Boolean);
        setBuildLog(lines.length > 1500 ? lines.slice(-1500).join('\n') : (log || ''));
    };

    const handleRollback = async (dep: store.Deployment) => {
        const elig = eligibility[dep.id];
        if (elig && !elig.eligible) return;
        if (!await confirm({
            title: 'Redeploy this version?',
            message: `Roll back to deployment #${dep.sequence ?? dep.id}?`,
            detail: elig?.method === 'rebuild-sha'
                ? `Image was not retained. Draft will rebuild from commit ${shortSha(dep.sourceSha) || 'unknown'} using current service settings.`
                : elig?.method === 're-pull'
                    ? 'Draft will re-pull this image and replace the current deployment.'
                    : 'A new deployment will run this image and replace the current one.',
            confirmLabel: 'Redeploy',
        })) return;
        setRollingBack(dep.id);
        setRollbackError(null);
        RollbackDeployment(dep.id)
            .then(() => setRollingBack(null))
            .catch((e: any) => {
                setRollingBack(null);
                setRollbackError(typeof e === 'string' ? e : e?.message || 'rollback failed');
            });
    };

    const loadMore = () => {
        if (loadingMore || !hasMore) return;
        void loadPage(deployments.length, true);
    };

    return (
        <div className="deployments-tab">
            {isBuilding && liveBuildLines.length > 0 && (
                <div className="deploy-live-log">
                    <h4 className="deploy-log-title">Build output</h4>
                    <div className="log-viewer" ref={buildLogRef}>
                        {liveBuildLines.map((line, i) => (
                            <div key={i} className="log-line">{line}</div>
                        ))}
                    </div>
                </div>
            )}

            <div className="deploy-history">
                <h4 className="deploy-history-title">
                    History
                    {total > 0 && (
                        <span className="deploy-history-count">
                            {deployments.length} of {total}
                        </span>
                    )}
                </h4>
                {rollbackError && (
                    <div className="deploy-entry-error"><AlertCircle size={12}/> {rollbackError}</div>
                )}
                {loading && deployments.length === 0 && <DeployHistorySkeleton />}
                {!loading && deployments.length === 0 && (
                    <span className="deploy-empty">No deployments yet.</span>
                )}
                {deployments.map((dep) => {
                    const elig = eligibility[dep.id];
                    const canRollback = !!elig?.eligible && dep.status !== 'building' && !isBuilding;
                    const rollbackTitle = elig
                        ? elig.eligible
                            ? 'Roll back to this deployment'
                            : `Not rollback-able: ${elig.reason}`
                        : 'Checking rollback eligibility…';
                    const sha = (dep.sourceSha || '').trim();
                    const shaShort = shortSha(sha);
                    return (
                    <div key={dep.id} className="deploy-entry">
                        <div className="deploy-entry-header" onClick={() => toggleExpand(dep)}>
                            <div className="deploy-entry-leading">
                                {expandedId === dep.id ? <ChevronDown size={13} /> : <ChevronRight size={13} />}
                                <StatusBadge status={dep.status} />
                                <span className="deploy-entry-tag mono">{dep.imageTag || `#${dep.id}`}</span>
                                {shaShort && (
                                    <span className="deploy-entry-sha mono" title={sha}>
                                        <GitCommitHorizontal size={12} aria-hidden />
                                        {shaShort}
                                    </span>
                                )}
                            </div>
                            <div className="deploy-entry-actions">
                                <span className="deploy-entry-time" title={new Date(dep.createdAt).toLocaleString()}>
                                    {new Date(dep.createdAt).toLocaleString(undefined, {
                                        month: 'numeric',
                                        day: 'numeric',
                                        hour: 'numeric',
                                        minute: '2-digit',
                                    })}
                                </span>
                                <button
                                    className="btn btn-ghost deploy-entry-rollback"
                                    onClick={(e) => { e.stopPropagation(); void handleRollback(dep); }}
                                    disabled={!canRollback || rollingBack === dep.id}
                                    title={rollbackTitle}
                                >
                                    <RotateCw size={12} className={rollingBack === dep.id ? 'spin' : ''}/>
                                    Redeploy
                                </button>
                            </div>
                        </div>
                        {expandedId === dep.id && (
                            <div className="deploy-entry-detail">
                                {dep.error && (
                                    <div className="deploy-entry-error">
                                        <AlertCircle size={12} />
                                        {dep.error}
                                    </div>
                                )}
                                <div className="deploy-entry-meta">
                                    {sha ? (
                                        <div className="deploy-entry-meta-row">
                                            <span className="deploy-entry-meta-label">Commit</span>
                                            <code className="deploy-entry-meta-value mono" title={sha}>{sha}</code>
                                        </div>
                                    ) : (
                                        <div className="deploy-entry-meta-row">
                                            <span className="deploy-entry-meta-label">Source</span>
                                            <span className="deploy-entry-meta-value deploy-entry-meta-value--muted">
                                                Working tree (no commit recorded)
                                            </span>
                                        </div>
                                    )}
                                </div>
                                {buildLog ? (
                                    <div className="log-viewer log-viewer--history">
                                        {buildLog.split('\n').filter(Boolean).map((line, i) => {
                                            let parsed = line;
                                            try {
                                                const obj = JSON.parse(line);
                                                parsed = obj.error || obj.stream || line;
                                            } catch { /* raw line */ }
                                            return <div key={i} className={`log-line ${line.includes('"error"') ? 'log-line--error' : ''}`}>{parsed.replace(/\n$/, '')}</div>;
                                        })}
                                    </div>
                                ) : dep.status === 'building' ? (
                                    <span className="deploy-empty">Build in progress...</span>
                                ) : (
                                    <span className="deploy-empty">No build log available.</span>
                                )}
                            </div>
                        )}
                    </div>
                    );
                })}
                {hasMore && (
                    <div className="deploy-history-more">
                        <button
                            type="button"
                            className="btn btn-ghost btn-sm"
                            onClick={loadMore}
                            disabled={loadingMore}
                        >
                            {loadingMore ? 'Loading…' : 'Load more'}
                        </button>
                    </div>
                )}
            </div>
        </div>
    );
}
