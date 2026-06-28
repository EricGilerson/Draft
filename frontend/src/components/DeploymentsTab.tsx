import {ChevronDown, ChevronRight, AlertCircle} from 'lucide-react';
import {useEffect, useRef, useState} from 'react';
import {GetDeployments, GetBuildLog} from '../../wailsjs/go/main/App';
import {store} from '../../wailsjs/go/models';
import {useBuildLog} from './BuildLogProvider';
import StatusBadge from './StatusBadge';

export default function DeploymentsTab({nodeId}: {nodeId: string}) {
    const [deployments, setDeployments] = useState<store.Deployment[]>([]);
    const [expandedId, setExpandedId] = useState<number | null>(null);
    const [buildLog, setBuildLog] = useState('');
    const buildLogRef = useRef<HTMLDivElement>(null);
    const {lines: liveBuildLines, deploying, version} = useBuildLog(nodeId);

    useEffect(() => {
        GetDeployments(nodeId).then(d => setDeployments(d || []));
    }, [nodeId]);

    useEffect(() => {
        if (version === 0) return;
        GetDeployments(nodeId).then(d => setDeployments(d || []));
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
                {deployments.length === 0 && (
                    <span className="deploy-empty">No deployments yet.</span>
                )}
                {deployments.map((dep) => (
                    <div key={dep.id} className="deploy-entry">
                        <button className="deploy-entry-header" onClick={() => toggleExpand(dep)}>
                            {expandedId === dep.id ? <ChevronDown size={13} /> : <ChevronRight size={13} />}
                            <StatusBadge status={dep.status} />
                            <span className="deploy-entry-tag mono">{dep.imageTag || `#${dep.id}`}</span>
                            <span className="deploy-entry-time">
                                {new Date(dep.createdAt).toLocaleString()}
                            </span>
                        </button>
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
                ))}
            </div>
        </div>
    );
}
