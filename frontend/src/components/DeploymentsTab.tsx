import {ChevronDown, ChevronRight, AlertCircle, RotateCw} from 'lucide-react';
import {useEffect, useRef, useState} from 'react';
import {GetDeployments, GetBuildLog, RollbackDeployment, RollbackEligibility} from '../../wailsjs/go/main/App';
import {store, deploy} from '../../wailsjs/go/models';
import {useBuildLog} from './BuildLogProvider';
import StatusBadge from './StatusBadge';

export default function DeploymentsTab({nodeId}: {nodeId: string}) {
    const [deployments, setDeployments] = useState<store.Deployment[]>([]);
    const [expandedId, setExpandedId] = useState<number | null>(null);
    const [buildLog, setBuildLog] = useState('');
    const [eligibility, setEligibility] = useState<Record<number, deploy.RollbackEligibility>>({});
    const [rollingBack, setRollingBack] = useState<number | null>(null);
    const [rollbackError, setRollbackError] = useState<string | null>(null);
    const buildLogRef = useRef<HTMLDivElement>(null);
    const {lines: liveBuildLines, deploying, version} = useBuildLog(nodeId);

    const refreshEligibility = () => {
        RollbackEligibility(nodeId)
            .then((items) => {
                const map: Record<number, deploy.RollbackEligibility> = {};
                for (const item of items ?? []) map[item.deploymentId] = item;
                setEligibility(map);
            })
            .catch(() => setEligibility({}));
    };

    useEffect(() => {
        GetDeployments(nodeId).then(d => setDeployments(d || []));
        refreshEligibility();
    }, [nodeId]);

    useEffect(() => {
        if (version === 0) return;
        GetDeployments(nodeId).then(d => setDeployments(d || []));
        refreshEligibility();
    }, [nodeId, version]);

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
        setBuildLog(log);
    };

    const handleRollback = (dep: store.Deployment) => {
        const elig = eligibility[dep.id];
        if (elig && !elig.eligible) return;
        if (!window.confirm(`Roll back to deployment #${dep.sequence ?? dep.id}? A new deployment will run this image and replace the current one.`)) return;
        setRollingBack(dep.id);
        setRollbackError(null);
        RollbackDeployment(dep.id)
            .then(() => setRollingBack(null))
            .catch((e: any) => {
                setRollingBack(null);
                setRollbackError(typeof e === 'string' ? e : e?.message || 'rollback failed');
            });
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
                <h4 className="deploy-history-title">History</h4>
                {rollbackError && (
                    <div className="deploy-entry-error"><AlertCircle size={12}/> {rollbackError}</div>
                )}
                {deployments.length === 0 && (
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
                    return (
                    <div key={dep.id} className="deploy-entry">
                        <div className="deploy-entry-header" onClick={() => toggleExpand(dep)}>
                            <div className="deploy-entry-leading">
                                {expandedId === dep.id ? <ChevronDown size={13} /> : <ChevronRight size={13} />}
                                <StatusBadge status={dep.status} />
                                <span className="deploy-entry-tag mono">{dep.imageTag || `#${dep.id}`}</span>
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
                                    onClick={(e) => { e.stopPropagation(); handleRollback(dep); }}
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
            </div>
        </div>
    );
}
