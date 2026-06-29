import {useEffect, useState} from 'react';
import {Download, Eye, EyeOff, FileSearch, Plus, RefreshCw, Upload} from 'lucide-react';
import {
    GetEnvVars, SetEnvVar, GetNodeSettings, SetNodeSetting, SelectFile,
    GetServiceRoot, SuggestEnvFile, ImportEnvFile, RefreshEnvFile, ExportEnvFile,
} from '../../wailsjs/go/main/App';
import {store} from '../../wailsjs/go/models';
import Dialog from './Dialog';
import './VariablesTab.css';

type VariablesTabProps = {
    nodeId: string;
    projectId: number;
    projectPath: string;
};

export default function VariablesTab({nodeId, projectId, projectPath}: VariablesTabProps) {
    const [vars, setVars] = useState<store.EnvVar[]>([]);
    const [originals, setOriginals] = useState<Record<string, string>>({});
    const [edits, setEdits] = useState<Record<string, string>>({});
    const [visible, setVisible] = useState<Record<string, boolean>>({});
    const [newKey, setNewKey] = useState('');
    const [newValue, setNewValue] = useState('');
    const [loading, setLoading] = useState(true);
    const [envFile, setEnvFile] = useState('');
    const [serviceRoot, setServiceRoot] = useState('');
    const [showConfirm, setShowConfirm] = useState(false);
    const [syncing, setSyncing] = useState(false);
    const [syncResult, setSyncResult] = useState<store.EnvFileSyncResult | null>(null);
    const [syncError, setSyncError] = useState('');

    const load = async () => {
        try {
            const v = await GetEnvVars(nodeId);
            const list = v || [];
            setVars(list);
            const map: Record<string, string> = {};
            list.forEach(x => { map[x.key] = x.value; });
            setOriginals(map);
            setEdits({});
        } catch (e) {
            console.error(e);
        } finally {
            setLoading(false);
        }
    };

    const loadSettings = async () => {
        try {
            const [s, root] = await Promise.all([
                GetNodeSettings(nodeId),
                GetServiceRoot(nodeId, projectId)
            ]);
            setEnvFile(s?.env_file || '');
            setServiceRoot(root || projectPath);
            if (!s?.env_file) {
                const suggestion = await SuggestEnvFile(nodeId, projectId);
                if (suggestion) {
                    setEnvFile(suggestion);
                }
            }
        } catch (e) {
            console.error(e);
        }
    };

    useEffect(() => {
        load();
        loadSettings();
    }, [nodeId]);

    const pickEnv = async () => {
        try {
            const p = await SelectFile('Select .env file', '');
            if (p) {
                await SetNodeSetting(nodeId, 'env_file', p);
                setEnvFile(p);
                setSyncResult(null);
                setSyncError('');
            }
        } catch (e) {
            console.error(e);
        }
    };

    const toggle = (key: string) => {
        setVisible(prev => ({...prev, [key]: !prev[key]}));
    };

    const stageEdit = (key: string, value: string) => {
        const original = originals[key] ?? '';
        setEdits(prev => {
            const next = {...prev};
            if (value === original) {
                delete next[key];
            } else {
                next[key] = value;
            }
            return next;
        });
    };

    const pendingChanges = Object.entries(edits).map(([k, v]) => {
        const original = vars.find(x => x.key === k)?.value ?? '';
        return {key: k, from: original, to: v};
    });

    const saveChanges = async () => {
        try {
            for (const [k, v] of Object.entries(edits)) {
                await SetEnvVar(nodeId, k, v);
            }
            setEdits({});
            setShowConfirm(false);
            setSyncResult(null);
            setSyncError('');
            await load();
        } catch (e) {
            console.error(e);
        }
    };

    const add = async () => {
        if (!newKey.trim()) return;
        try {
            await SetEnvVar(nodeId, newKey.trim(), newValue);
            setNewKey('');
            setNewValue('');
            setSyncResult(null);
            setSyncError('');
            await load();
        } catch (e) {
            console.error(e);
        }
    };

    const persistEnvPath = async () => {
        await SetNodeSetting(nodeId, 'env_file', envFile.trim());
    };

    const runSync = async (action: 'import' | 'refresh' | 'export') => {
        setSyncing(true);
        setSyncError('');
        setSyncResult(null);
        try {
            await persistEnvPath();
            let result: store.EnvFileSyncResult;
            if (action === 'import') {
                result = await ImportEnvFile(nodeId, envFile.trim());
            } else if (action === 'refresh') {
                result = await RefreshEnvFile(nodeId);
            } else {
                result = await ExportEnvFile(nodeId);
            }
            setSyncResult(result);
            await load();
        } catch (e: any) {
            setSyncError(typeof e === 'string' ? e : e?.message || `${action} failed`);
        } finally {
            setSyncing(false);
        }
    };

    const conflicts = syncResult?.conflicts ?? [];
    const resultText = syncResult ? [
        syncResult.imported ? `${syncResult.imported} imported` : '',
        syncResult.updated ? `${syncResult.updated} updated` : '',
        syncResult.unchanged ? `${syncResult.unchanged} unchanged` : '',
        syncResult.exported ? `${syncResult.exported} exported` : '',
        syncResult.skipped ? `${syncResult.skipped} skipped` : '',
        conflicts.length ? `${conflicts.length} conflict${conflicts.length > 1 ? 's' : ''}` : '',
    ].filter(Boolean).join(' · ') : '';

    if (loading) {
        return <div className="variables-loading">Loading...</div>;
    }

    return (
        <div className="variables-tab">
            <div className="form-field">
                <label className="form-label">Linked environment file</label>
                <span className="settings-hint">Draft stores variables in SQLite. Use this file only for explicit import, refresh, or export.</span>
                <div className="input-with-action">
                    <input
                        className="input"
                        value={envFile}
                        onChange={(e) => setEnvFile(e.target.value)}
                        onBlur={async () => {
                            await persistEnvPath();
                        }}
                        placeholder=".env"
                    />
                    <button className="btn btn-ghost input-action-btn" onClick={pickEnv} title="Browse for .env">
                        <FileSearch size={14}/>
                    </button>
                </div>
                {!envFile && serviceRoot && (
                    <span className="settings-hint">No linked .env file yet. Draft variables can still be managed here.</span>
                )}
                <div className="env-sync-actions">
                    <button className="btn btn-ghost" onClick={() => runSync('import')} disabled={syncing || !envFile.trim()}>
                        <Upload size={13}/> Import
                    </button>
                    <button className="btn btn-ghost" onClick={() => runSync('refresh')} disabled={syncing}>
                        <RefreshCw size={13}/> Refresh
                    </button>
                    <button className="btn btn-ghost" onClick={() => runSync('export')} disabled={syncing}>
                        <Download size={13}/> Export
                    </button>
                </div>
                {(resultText || syncError) && (
                    <div className={`env-sync-status ${syncError ? 'env-sync-status--error' : ''}`}>
                        {syncError || resultText}
                    </div>
                )}
                {conflicts.length > 0 && (
                    <div className="env-conflicts">
                        {conflicts.map((conflict) => (
                            <div key={conflict.key} className="env-conflict-row">
                                <span className="var-key">{conflict.key}</span>
                                <span>Draft kept its value instead of overwriting it from file.</span>
                            </div>
                        ))}
                    </div>
                )}
            </div>
            <div className="variables-list">
                {vars.length === 0 && (
                    <div className="variables-empty">No Draft variables yet.</div>
                )}
                {vars.map(v => (
                    <div key={v.key} className="var-row">
                        <div className="var-key-cell">
                            <div className="var-key" title={v.key}>{v.key}</div>
                            <div className={`var-source var-source--${v.source || 'manual'}`}>
                                {v.source || 'manual'} · {v.scope || 'runtime'}
                            </div>
                        </div>
                        <div className="var-value">
                            {visible[v.key] ? (
                                <textarea
                                    className="var-value-editor"
                                    value={v.value}
                                    rows={v.value.includes('\n') || v.value.length > 160 ? 7 : 2}
                                    wrap="off"
                                    spellCheck={false}
                                    onChange={e => {
                                        const nv = [...vars];
                                        const idx = nv.findIndex(x => x.key === v.key);
                                        nv[idx] = store.EnvVar.createFrom({...v, value: e.target.value});
                                        setVars(nv);
                                        stageEdit(v.key, e.target.value);
                                    }}
                                />
                            ) : (
                                <input
                                    className="var-value-mask"
                                    type="password"
                                    value={v.value}
                                    disabled
                                    readOnly
                                />
                            )}
                            <button className="var-toggle" onClick={() => toggle(v.key)}>
                                {visible[v.key] ? <EyeOff size={14}/> : <Eye size={14}/>}
                            </button>
                        </div>
                    </div>
                ))}
            </div>

            <div className="var-add">
                <input
                    placeholder="KEY"
                    value={newKey}
                    onChange={e => setNewKey(e.target.value.toUpperCase())}
                />
                <input
                    placeholder="value"
                    value={newValue}
                    onChange={e => setNewValue(e.target.value)}
                />
                <button className="btn btn-primary" onClick={add}>
                    <Plus size={14}/> Add
                </button>
            </div>

            {Object.keys(edits).length > 0 && (
                <button className="btn btn-primary save-btn" onClick={() => setShowConfirm(true)}>
                    Save {Object.keys(edits).length} Draft change{Object.keys(edits).length > 1 ? 's' : ''}
                </button>
            )}

            {showConfirm && (
                <Dialog title="Confirm variable changes" onClose={() => setShowConfirm(false)} footer={
                    <div style={{display:'flex',gap:8,justifyContent:'flex-end'}}>
                        <button className="btn btn-ghost" onClick={() => setShowConfirm(false)}>Cancel</button>
                        <button className="btn btn-primary" onClick={saveChanges}>Save changes</button>
                    </div>
                }>
                    <p className="var-confirm-note">
                        These changes update Draft's database. Use Export when you want to write them back to the linked .env file.
                    </p>
                    <div className="var-diff">
                        {pendingChanges.map(c => (
                            <div key={c.key} className="var-diff-row">
                                <div className="var-key">{c.key}</div>
                                <div className="var-diff-values">
                                    <span className="old">{c.from || '(empty)'}</span>
                                    <span>→</span>
                                    <span className="new">{c.to}</span>
                                </div>
                            </div>
                        ))}
                    </div>
                </Dialog>
            )}
        </div>
    );
}
