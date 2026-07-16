import {deploy} from '../../wailsjs/go/models';
import {SkeletonListCards} from './Skeleton';
import '../views/SecretsView.css';

type ScopedValueUsagesProps = {
    usages: deploy.SecretUsage[];
    loading: boolean;
    redeploying: string | null;
    onRedeploy: (nodeId: string) => void;
    onRedeployAllRunning: () => void;
    emptyMessage?: string;
};

export default function ScopedValueUsages({
    usages,
    loading,
    redeploying,
    onRedeploy,
    onRedeployAllRunning,
    emptyMessage = 'No services reference this value yet.',
}: ScopedValueUsagesProps) {
    const runningCount = usages.filter((u) => u.isRunning).length;

    return (
        <div className="secret-usages">
            <div className="secret-usages-head">
                <h3 className="secret-usages-title">Usages</h3>
                {runningCount > 0 && (
                    <button type="button" className="btn btn-ghost" onClick={onRedeployAllRunning} disabled={!!redeploying}>
                        Redeploy all running ({runningCount})
                    </button>
                )}
            </div>
            <p className="settings-hint">Services not redeployed will pick up the new value on their next deploy.</p>
            {loading ? (
                <SkeletonListCards count={3} withActions />
            ) : usages.length === 0 ? (
                <div className="secrets-empty">{emptyMessage}</div>
            ) : (
                <div className="secret-usages-list">
                    {usages.map((u) => (
                        <div key={`${u.nodeId}:${u.varKey}`} className="secret-usage-row">
                            <div className="secret-usage-main">
                                <span className="secret-usage-service">{u.projectName} / {u.nodeLabel}</span>
                                <span className="secret-usage-var">{u.varKey}</span>
                                {u.overridden && <span className="secret-usage-badge">overridden</span>}
                                <span className={`secret-usage-status secret-usage-status--${u.isRunning ? 'running' : 'stopped'}`}>
                                    {u.isRunning ? 'running' : 'stopped'}
                                </span>
                            </div>
                            <button
                                type="button"
                                className="btn btn-ghost"
                                disabled={!u.isRunning || redeploying === u.nodeId}
                                onClick={() => onRedeploy(u.nodeId)}
                            >
                                {redeploying === u.nodeId ? 'Redeploying…' : 'Redeploy'}
                            </button>
                        </div>
                    ))}
                </div>
            )}
        </div>
    );
}
