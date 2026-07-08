import {AlertTriangle, ArrowLeftRight, Info, KeyRound} from 'lucide-react';
import {cloudconfig} from '../../wailsjs/go/models';
import './ConfigReport.css';

// ConfigReport renders a cloudconfig.Report (the fidelity report) grouped by
// note kind, so a user can see what mapped cleanly, what was ignored, what was
// transformed, and what needs a manual decision before deploying.

const KIND_META: Record<string, {label: string; icon: JSX.Element; className: string}> = {
    manual: {label: 'Needs your attention', icon: <KeyRound size={13}/>, className: 'config-note-manual'},
    transformed: {label: 'Transformed', icon: <ArrowLeftRight size={13}/>, className: 'config-note-transformed'},
    ignored: {label: 'Not applied locally', icon: <AlertTriangle size={13}/>, className: 'config-note-ignored'},
    info: {label: 'Notes', icon: <Info size={13}/>, className: 'config-note-info'},
};

const ORDER = ['manual', 'transformed', 'ignored', 'info'];

export default function ConfigReport({report}: {report?: cloudconfig.Report}) {
    const notes = report?.notes ?? [];
    if (notes.length === 0) {
        return <p className="config-report-empty">Everything mapped cleanly — no fidelity notes.</p>;
    }
    const groups = ORDER.map((kind) => ({
        kind,
        meta: KIND_META[kind] ?? KIND_META.info,
        notes: notes.filter((n) => n.kind === kind),
    })).filter((g) => g.notes.length > 0);

    return (
        <div className="config-report">
            {groups.map((g) => (
                <div key={g.kind} className={`config-note-group ${g.meta.className}`}>
                    <div className="config-note-group-header">
                        {g.meta.icon}
                        <span>{g.meta.label}</span>
                        <span className="config-note-count">{g.notes.length}</span>
                    </div>
                    <ul className="config-note-list">
                        {g.notes.map((n, i) => (
                            <li key={i}>
                                {n.field && <code className="config-note-field">{n.field}</code>}
                                <span>{n.message}</span>
                            </li>
                        ))}
                    </ul>
                </div>
            ))}
        </div>
    );
}
