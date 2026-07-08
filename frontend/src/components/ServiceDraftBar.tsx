import {AlertTriangle, Loader2} from 'lucide-react';
import {useCallback, useState} from 'react';
import {useServiceConfigEditor} from '../lib/serviceConfigEditor';
import {isImmediateSetting} from '../lib/settingStaging';
import {useAppDialog} from './AppDialogProvider';
import Dialog from './Dialog';
import './ServiceDraftBar.css';

type ServiceDraftBarProps = {
    onStaged?: () => void;
    onDeploy?: () => void;
};

export default function ServiceDraftBar({onStaged, onDeploy}: ServiceDraftBarProps) {
    const {
        isSessionDirty,
        hasStagedChanges,
        staging,
        draftSettings,
        discardSessionDraft,
        discardStaged,
        previewStage,
        stageChanges,
        stageAndDeploy,
    } = useServiceConfigEditor();

    const [confirmOpen, setConfirmOpen] = useState(false);
    const [confirmWarnings, setConfirmWarnings] = useState<string[]>([]);
    const [confirmErrors, setConfirmErrors] = useState<string[]>([]);
    const [pendingDeploy, setPendingDeploy] = useState(false);
    const [error, setError] = useState('');
    const {confirm} = useAppDialog();

    const runStage = useCallback(async (deployAfter: boolean) => {
        setError('');
        try {
            const hasSettingsDraft = Object.keys(draftSettings).filter((k) => !isImmediateSetting(k)).length > 0;
            // Preview only applies to settings keys in the current batch; env-only edits stage directly.
            let errors: string[] = [];
            let warnings: string[] = [];
            if (hasSettingsDraft) {
                const preview = await previewStage();
                errors = (preview?.errors || []).map((e) => e.message).filter(Boolean);
                warnings = (preview?.warnings || []).map((w) => w.message).filter(Boolean);
            }
            if (errors.length > 0) {
                setConfirmErrors(errors);
                setConfirmWarnings(warnings);
                setPendingDeploy(deployAfter);
                setConfirmOpen(true);
                return;
            }
            if (warnings.length > 0) {
                setConfirmErrors([]);
                setConfirmWarnings(warnings);
                setPendingDeploy(deployAfter);
                setConfirmOpen(true);
                return;
            }
            if (deployAfter) {
                await stageAndDeploy();
                onDeploy?.();
            } else {
                await stageChanges();
            }
            onStaged?.();
        } catch (e) {
            setError(String(e));
        }
    }, [draftSettings, previewStage, stageChanges, stageAndDeploy, onStaged, onDeploy]);

    const confirmStage = useCallback(async () => {
        if (confirmErrors.length > 0) {
            setConfirmOpen(false);
            return;
        }
        setConfirmOpen(false);
        setError('');
        try {
            if (pendingDeploy) {
                await stageAndDeploy();
                onDeploy?.();
            } else {
                await stageChanges();
            }
            onStaged?.();
        } catch (e) {
            setError(String(e));
        }
    }, [confirmErrors, pendingDeploy, stageAndDeploy, stageChanges, onDeploy, onStaged]);

    if (!isSessionDirty && !hasStagedChanges) {
        return null;
    }

    return (
        <>
            <div className="service-draft-bar">
                {hasStagedChanges && (
                    <span className="service-draft-bar-staged">
                        Staged — will apply on next deploy
                    </span>
                )}
                {isSessionDirty && (
                    <span className="service-draft-bar-dirty">Unsaved edits</span>
                )}
                {error && <span className="service-draft-bar-error">{error}</span>}
                <div className="service-draft-bar-actions">
                    {isSessionDirty && (
                        <button
                            type="button"
                            className="btn btn-ghost btn-sm"
                            disabled={staging}
                            onClick={discardSessionDraft}
                        >
                            Discard edits
                        </button>
                    )}
                    {hasStagedChanges && (
                        <button
                            type="button"
                            className="btn btn-ghost btn-sm"
                            disabled={staging}
                            onClick={async () => {
                                if (await confirm({
                                    title: 'Discard staged changes?',
                                    message: 'Discard all staged changes?',
                                    detail: 'Applied settings stay unchanged until you redeploy.',
                                    confirmLabel: 'Discard',
                                    cancelLabel: 'Keep',
                                    danger: true,
                                })) {
                                    void discardStaged();
                                }
                            }}
                        >
                            Discard staged
                        </button>
                    )}
                    {isSessionDirty && (
                        <>
                            <button
                                type="button"
                                className="btn btn-primary btn-sm"
                                disabled={staging}
                                onClick={() => void runStage(false)}
                            >
                                {staging ? <Loader2 size={14} className="spin"/> : null}
                                Stage changes
                            </button>
                            <button
                                type="button"
                                className="btn btn-secondary btn-sm"
                                disabled={staging}
                                onClick={() => void runStage(true)}
                            >
                                Stage &amp; deploy
                            </button>
                        </>
                    )}
                </div>
            </div>

            {confirmOpen && (
                <Dialog title="Stage changes?" onClose={() => setConfirmOpen(false)}>
                    <div className="service-draft-confirm">
                        {confirmErrors.length > 0 && (
                            <div className="service-draft-confirm-block service-draft-confirm-block--error">
                                <AlertTriangle size={16}/>
                                <div>
                                    <strong>Cannot stage</strong>
                                    <ul>
                                        {confirmErrors.map((msg) => <li key={msg}>{msg}</li>)}
                                    </ul>
                                </div>
                            </div>
                        )}
                        {confirmWarnings.length > 0 && confirmErrors.length === 0 && (
                            <div className="service-draft-confirm-block">
                                <AlertTriangle size={16}/>
                                <div>
                                    <p>These changes apply on the next deploy:</p>
                                    <ul>
                                        {confirmWarnings.map((msg) => <li key={msg}>{msg}</li>)}
                                    </ul>
                                </div>
                            </div>
                        )}
                        <div className="service-draft-confirm-actions">
                            <button type="button" className="btn btn-ghost" onClick={() => setConfirmOpen(false)}>
                                Cancel
                            </button>
                            {confirmErrors.length === 0 && (
                                <button type="button" className="btn btn-primary" onClick={() => void confirmStage()}>
                                    {pendingDeploy ? 'Stage & deploy' : 'Stage changes'}
                                </button>
                            )}
                        </div>
                    </div>
                </Dialog>
            )}
        </>
    );
}
