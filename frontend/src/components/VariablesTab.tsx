import {useEffect, useState} from 'react';
import {Eye, EyeOff, Plus} from 'lucide-react';
import {GetEnvVars, SetEnvVar} from '../../wailsjs/go/main/App';
import {store} from '../../wailsjs/go/models';
import './VariablesTab.css';

type VariablesTabProps = {
    nodeId: string;
};

export default function VariablesTab({nodeId}: VariablesTabProps) {
    const [vars, setVars] = useState<store.EnvVar[]>([]);
    const [visible, setVisible] = useState<Record<string, boolean>>({});
    const [newKey, setNewKey] = useState('');
    const [newValue, setNewValue] = useState('');
    const [loading, setLoading] = useState(true);

    const load = async () => {
        try {
            const v = await GetEnvVars(nodeId);
            setVars(v || []);
        } catch (e) {
            console.error(e);
        } finally {
            setLoading(false);
        }
    };

    useEffect(() => {
        load();
    }, [nodeId]);

    const toggle = (key: string) => {
        setVisible(prev => ({...prev, [key]: !prev[key]}));
    };

    const update = async (key: string, value: string) => {
        try {
            await SetEnvVar(nodeId, key, value);
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
                                onChange={e => {
                                    const nv = [...vars];
                                    const idx = nv.findIndex(x => x.key === v.key);
                                    nv[idx] = {...v, value: e.target.value};
                                    setVars(nv);
                                }}
                                onBlur={e => update(v.key, e.target.value)}
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
        </div>
    );
}
