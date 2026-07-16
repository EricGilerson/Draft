import {useCallback, useEffect, useState} from 'react';
import {Eye, EyeOff, KeyRound, Pencil, Plus, Trash2} from 'lucide-react';
import {
    ListAppSecrets, SetAppSecret, DeleteAppSecret, ListAppSecretUsages,
    DeployService,
} from '../../wailsjs/go/main/App';
import {deploy, store} from '../../wailsjs/go/models';
import {useAppDialog} from '../components/AppDialogProvider';
import PageHeader from '../components/PageHeader';
import Dialog from '../components/Dialog';
import ScopedValueUsages from '../components/ScopedValueUsages';
import {SkeletonListCards} from '../components/Skeleton';
import './SecretsView.css';

type EditorMode =
    | {kind: 'closed'}
    | {kind: 'app-add'}
    | {kind: 'app-edit'; secret: store.AppSecret};

export default function SecretsView() {
    const [appSecrets, setAppSecrets] = useState<store.AppSecret[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState('');
    const [editor, setEditor] = useState<EditorMode>({kind: 'closed'});
    const {confirm} = useAppDialog();

    const refresh = useCallback(() => {
        setError('');
        ListAppSecrets()
            .then((app) => setAppSecrets(app ?? []))
            .catch((e) => setError(typeof e === 'string' ? e : e?.message || 'Failed to load secrets'))
            .finally(() => setLoading(false));
    }, []);

    useEffect(() => { refresh(); }, [refresh]);

    const handleDeleteApp = async (key: string) => {
        if (!await confirm({
            title: 'Delete app secret?',
            message: `Delete app secret ${key}?`,
            confirmLabel: 'Delete',
            danger: true,
        })) return;
        try {
            await DeleteAppSecret(key);
            refresh();
        } catch (e: any) {
            setError(typeof e === 'string' ? e : e?.message || 'Delete failed');
        }
    };

    return (
        <div className="secrets-view">
            <PageHeader
                title="Secrets"
                description="Manage app-wide secrets. Reference them from any service as {{secret.KEY}}."
            />
            <div className="secrets-layout">
                {error && <p className="secrets-error">{error}</p>}

                <section className="secrets-section">
                    <div className="secrets-section-head">
                        <h2 className="secrets-section-title">
                            <KeyRound size={14}/> App secrets
                        </h2>
                        <button type="button" className="btn btn-primary" onClick={() => setEditor({kind: 'app-add'})}>
                            <Plus size={14}/> Add
                        </button>
                    </div>
                    <p className="secrets-section-hint">Referenced from any service as <code>{'{{secret.KEY}}'}</code></p>
                    {loading ? (
                        <SkeletonListCards count={5} withActions />
                    ) : appSecrets.length === 0 ? (
                        <div className="secrets-empty">No app secrets yet.</div>
                    ) : (
                        <div className="secrets-list">
                            {appSecrets.map((s) => (
                                <div key={s.key} className="secrets-row">
                                    <div className="secrets-row-main">
                                        <span className="secrets-key">{s.key}</span>
                                        {s.description && <span className="secrets-desc">{s.description}</span>}
                                    </div>
                                    <div className="secrets-row-actions">
                                        <button type="button" className="btn btn-ghost" onClick={() => setEditor({kind: 'app-edit', secret: s})} title="Edit">
                                            <Pencil size={14}/>
                                        </button>
                                        <button type="button" className="btn btn-ghost secrets-delete" onClick={() => { void handleDeleteApp(s.key); }} title="Delete">
                                            <Trash2 size={14}/>
                                        </button>
                                    </div>
                                </div>
                            ))}
                        </div>
                    )}
                </section>
            </div>

            {editor.kind !== 'closed' && (
                <SecretEditorDialog
                    mode={editor}
                    onClose={() => setEditor({kind: 'closed'})}
                    onSaved={refresh}
                />
            )}
        </div>
    );
}

function SecretEditorDialog({mode, onClose, onSaved}: {
    mode: Exclude<EditorMode, {kind: 'closed'}>;
    onClose: () => void;
    onSaved: () => void;
}) {
    const isAdd = mode.kind === 'app-add';

    const [key, setKey] = useState(mode.kind === 'app-edit' ? mode.secret.key : '');
    const [value, setValue] = useState(mode.kind === 'app-edit' ? mode.secret.value : '');
    const [description, setDescription] = useState(mode.kind === 'app-edit' ? (mode.secret.description || '') : '');
    const [revealed, setRevealed] = useState(false);
    const [saving, setSaving] = useState(false);
    const [error, setError] = useState('');
    const [usages, setUsages] = useState<deploy.SecretUsage[]>([]);
    const [loadingUsages, setLoadingUsages] = useState(false);
    const [redeploying, setRedeploying] = useState<string | null>(null);

    const title = isAdd ? 'Add app secret' : `Edit ${key}`;

    const loadUsages = useCallback(() => {
        if (isAdd) return;
        setLoadingUsages(true);
        ListAppSecretUsages(key)
            .then((list) => setUsages(list ?? []))
            .catch(() => setUsages([]))
            .finally(() => setLoadingUsages(false));
    }, [isAdd, key]);

    useEffect(() => { loadUsages(); }, [loadUsages]);

    const save = async () => {
        const trimmedKey = key.trim();
        if (!trimmedKey) return;
        setSaving(true);
        setError('');
        try {
            await SetAppSecret(trimmedKey, value, description);
            onSaved();
            if (!isAdd) {
                loadUsages();
            } else {
                onClose();
            }
        } catch (e: any) {
            setError(typeof e === 'string' ? e : e?.message || 'Save failed');
        } finally {
            setSaving(false);
        }
    };

    const redeploy = async (nodeId: string) => {
        setRedeploying(nodeId);
        try {
            await DeployService(nodeId);
            loadUsages();
        } catch (e: any) {
            setError(typeof e === 'string' ? e : e?.message || 'Redeploy failed');
        } finally {
            setRedeploying(null);
        }
    };

    const redeployAllRunning = async () => {
        for (const u of usages.filter((usage) => usage.isRunning)) {
            await redeploy(u.nodeId);
        }
    };

    return (
        <Dialog title={title} onClose={onClose}>
            <div className="secret-editor">
                {error && <p className="form-error">{error}</p>}

                <div className="form-field">
                    <label className="form-label">Key</label>
                    <input className="input" value={key} onChange={(e) => setKey(e.target.value)} disabled={!isAdd} placeholder="OPENAI_API_KEY" />
                </div>

                <div className="form-field">
                    <label className="form-label">Value</label>
                    <div className="secret-value-row">
                        <input
                            className="input"
                            type={revealed ? 'text' : 'password'}
                            value={value}
                            onChange={(e) => setValue(e.target.value)}
                            autoComplete="off"
                        />
                        <button type="button" className="btn btn-ghost" onClick={() => setRevealed((r) => !r)}>
                            {revealed ? <EyeOff size={14}/> : <Eye size={14}/>}
                        </button>
                    </div>
                </div>

                <div className="form-field">
                    <label className="form-label">Description</label>
                    <input className="input" value={description} onChange={(e) => setDescription(e.target.value)} placeholder="Optional" />
                </div>

                <div className="secret-editor-actions">
                    <button type="button" className="btn btn-primary" onClick={save} disabled={saving || !key.trim()}>
                        {saving ? 'Saving…' : 'Save'}
                    </button>
                    <button type="button" className="btn btn-ghost" onClick={onClose}>Cancel</button>
                </div>

                {!isAdd && (
                    <ScopedValueUsages
                        usages={usages}
                        loading={loadingUsages}
                        redeploying={redeploying}
                        onRedeploy={redeploy}
                        onRedeployAllRunning={redeployAllRunning}
                        emptyMessage="No services reference this secret yet."
                    />
                )}
            </div>
        </Dialog>
    );
}
