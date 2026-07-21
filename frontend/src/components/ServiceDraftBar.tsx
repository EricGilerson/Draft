import {AlertTriangle, Loader2} from 'lucide-react';
import {useCallback, useMemo, useState} from 'react';
import {useServiceConfigEditor} from '../lib/serviceConfigEditor';
import {isImmediateSetting, settingsValuesEqual} from '../lib/settingStaging';
import {committedEnvByKey, normalizeEnvDraft} from '../lib/envStaging';
import Dialog from './Dialog';
import DiscardChangesDialog, {
    envChangeDetail,
    humanizeSettingKey,
    settingChangeDetail,
    type DiscardChangeItem,
} from './DiscardChangesDialog';
import './ServiceDraftBar.css';

type ServiceDraftBarProps = {
    onStaged?: () => void;
    onDeploy?: () => void;
};

type DiscardMode = 'session' | 'staged';

function parseDiscardId(id: string): {kind: 'setting' | 'env'; key: string} | null {
    const idx = id.indexOf(':');
    if (idx <= 0) return null;
    const kind = id.slice(0, idx);
    const key = id.slice(idx + 1);
    if ((kind !== 'setting' && kind !== 'env') || !key) return null;
    return {kind, key};
}

function splitSelected(ids: string[]): {settingKeys: string[]; envKeys: string[]} {
    const settingKeys: string[] = [];
    const envKeys: string[] = [];
    for (const id of ids) {
        const parsed = parseDiscardId(id);
        if (!parsed) continue;
        if (parsed.kind === 'setting') settingKeys.push(parsed.key);
        else envKeys.push(parsed.key);
    }
    return {settingKeys, envKeys};
}

export default function ServiceDraftBar({onStaged, onDeploy}: ServiceDraftBarProps) {
    const {
        isSessionDirty,
        hasStagedChanges,
        staging,
        draftSettings,
        envDraft,
        appliedSettings,
        stagedSettings,
        stagedEnvChanges,
        appliedEnvVars,
        committedSettings,
        discardSessionDraftPartial,
        discardStagedPartial,
        previewStage,
        stageChanges,
        stageAndDeploy,
    } = useServiceConfigEditor();

    const [confirmOpen, setConfirmOpen] = useState(false);
    const [confirmWarnings, setConfirmWarnings] = useState<string[]>([]);
    const [confirmErrors, setConfirmErrors] = useState<string[]>([]);
    const [pendingDeploy, setPendingDeploy] = useState(false);
    const [error, setError] = useState('');
    const [discardMode, setDiscardMode] = useState<DiscardMode | null>(null);

    const appliedEnvByKey = useMemo(() => {
        const m = new Map<string, string>();
        for (const v of appliedEnvVars) m.set(v.key, v.value);
        return m;
    }, [appliedEnvVars]);

    // Applied + staged env (what session draft diffs against).
    const committedEnv = useMemo(
        () => committedEnvByKey(appliedEnvVars, stagedEnvChanges),
        [appliedEnvVars, stagedEnvChanges],
    );

    const sessionItems = useMemo((): DiscardChangeItem[] => {
        const items: DiscardChangeItem[] = [];
        const settingKeys = Object.keys(draftSettings)
            .filter((k) => !isImmediateSetting(k))
            .filter((k) => !settingsValuesEqual(draftSettings[k] ?? '', committedSettings[k] ?? ''))
            .sort((a, b) => a.localeCompare(b));
        for (const key of settingKeys) {
            const from = committedSettings[key] ?? '';
            const to = draftSettings[key] ?? '';
            items.push({
                id: `setting:${key}`,
                kind: 'setting',
                key,
                label: humanizeSettingKey(key),
                detail: settingChangeDetail(key, from, to),
            });
        }

        const normalized = normalizeEnvDraft(envDraft, committedEnv);
        const envKeys = [
            ...Object.keys(normalized.upserts),
            ...normalized.deleteKeys,
        ].sort((a, b) => a.localeCompare(b));
        for (const key of envKeys) {
            const isDelete = normalized.deleteKeys.includes(key);
            const upsert = normalized.upserts[key];
            const from = key in committedEnv ? committedEnv[key].value : undefined;
            const to = isDelete ? undefined : upsert?.value;
            items.push({
                id: `env:${key}`,
                kind: 'env',
                key,
                label: key,
                badge: isDelete ? 'delete' : from === undefined ? 'new' : 'edit',
                detail: envChangeDetail(from, to, isDelete),
            });
        }
        return items;
    }, [draftSettings, envDraft, committedSettings, committedEnv]);

    const stagedItems = useMemo((): DiscardChangeItem[] => {
        const items: DiscardChangeItem[] = [];
        const settingKeys = Object.keys(stagedSettings)
            .filter((k) => !settingsValuesEqual(stagedSettings[k] ?? '', appliedSettings[k] ?? ''))
            .sort((a, b) => a.localeCompare(b));
        for (const key of settingKeys) {
            const from = appliedSettings[key] ?? '';
            const to = stagedSettings[key] ?? '';
            items.push({
                id: `setting:${key}`,
                kind: 'setting',
                key,
                label: humanizeSettingKey(key),
                detail: settingChangeDetail(key, from, to),
            });
        }
        const envSorted = [...(stagedEnvChanges || [])].sort((a, b) => a.key.localeCompare(b.key));
        for (const ch of envSorted) {
            const from = appliedEnvByKey.has(ch.key) ? appliedEnvByKey.get(ch.key) : undefined;
            items.push({
                id: `env:${ch.key}`,
                kind: 'env',
                key: ch.key,
                label: ch.key,
                badge: ch.delete ? 'delete' : from === undefined ? 'new' : 'edit',
                detail: envChangeDetail(from, ch.delete ? undefined : ch.value, !!ch.delete),
            });
        }
        return items;
    }, [stagedSettings, stagedEnvChanges, appliedSettings, appliedEnvByKey]);

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

    const handleDiscardSelected = useCallback(async (ids: string[]) => {
        const {settingKeys, envKeys} = splitSelected(ids);
        if (discardMode === 'session') {
            discardSessionDraftPartial(settingKeys, envKeys);
            return;
        }
        if (discardMode === 'staged') {
            await discardStagedPartial(settingKeys, envKeys);
            onStaged?.();
        }
    }, [discardMode, discardSessionDraftPartial, discardStagedPartial, onStaged]);

    if (!isSessionDirty && !hasStagedChanges) {
        return null;
    }

    const discardItems = discardMode === 'session' ? sessionItems : discardMode === 'staged' ? stagedItems : [];

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
                            onClick={() => setDiscardMode('session')}
                        >
                            Discard edits…
                        </button>
                    )}
                    {hasStagedChanges && (
                        <button
                            type="button"
                            className="btn btn-ghost btn-sm"
                            disabled={staging}
                            onClick={() => setDiscardMode('staged')}
                        >
                            Discard staged…
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

            {discardMode && (
                <DiscardChangesDialog
                    title={discardMode === 'session' ? 'Discard unsaved edits' : 'Discard staged changes'}
                    description={
                        discardMode === 'session'
                            ? 'Choose which unsaved edits to drop. Remaining edits stay in the draft bar until you stage or discard them.'
                            : 'Choose which staged changes to drop. Applied (live) config is unchanged. Remaining staged rows still apply on the next deploy.'
                    }
                    items={discardItems}
                    confirmLabel="Discard selected"
                    onClose={() => setDiscardMode(null)}
                    onDiscard={handleDiscardSelected}
                />
            )}
        </>
    );
}
