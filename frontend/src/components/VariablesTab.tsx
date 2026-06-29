import {useEffect, useState} from 'react';
import {Eye, EyeOff, Plus, FileSearch} from 'lucide-react';
import {
    GetEnvVars, SetEnvVar, GetNodeSettings, SetNodeSetting, SelectFile,
    GetServiceRoot, SuggestEnvFile
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
                await load();
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
            await load();
        } catch (e) {
            console.error(e);
        }
    };

    if (loading) {
        return <div className="variables-loading">Loading...</div>;
    }

    return (
        <div className="variables-tab">
            <div className="form-field">
                <label className="form-label">Environment file</label>
                <span className="settings-hint">Path to the .env file used for this service.</span>
                <div className="input-with-action">
                    <input
                        className="input"
                        value={envFile}
                        onChange={(e) => setEnvFile(e.target.value)}
                        onBlur={async () => {
                            await SetNodeSetting(nodeId, 'env_file', envFile.trim());
                            await load();
                        }}
                        placeholder=".env"
                    />
                    <button className="btn btn-ghost input-action-btn" onClick={pickEnv} title="Browse for .env">
                        <FileSearch size={14}/>
                    </button>
                </div>
                {!envFile && serviceRoot && (
                    <span className="settings-hint">No .env found in root directory.</span>
                )}
            </div>
            <div className="variables-list">
                {vars.length === 0 && (
                    <div className="variables-empty">No variables in .env yet.</div>
                )}
                {vars.map(v => (
                    <div key={v.key} className="var-row">
                        <div className="var-key">{v.key}</div>
                        <div className="var-value">
                            <input
                                type={visible[v.key] ? 'text' : 'password'}
                                value={v.value}
                                disabled={!visible[v.key]}
                                onChange={e => {
                                    const nv = [...vars];
                                    const idx = nv.findIndex(x => x.key === v.key);
                                    nv[idx] = {...v, value: e.target.value};
                                    setVars(nv);
                                    stageEdit(v.key, e.target.value);
                                }}
                            />
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
                    Save {Object.keys(edits).length} change{Object.keys(edits).length > 1 ? 's' : ''}
                </button>
            )}

            {showConfirm && (
                <Dialog title="Confirm variable changes" onClose={() => setShowConfirm(false)} footer={
                    <div style={{display:'flex',gap:8,justifyContent:'flex-end'}}>
                        <button className="btn btn-ghost" onClick={() => setShowConfirm(false)}>Cancel</button>
                        <button className="btn btn-primary" onClick={saveChanges}>Save changes</button>
                    </div>
                }>
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
