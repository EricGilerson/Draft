import {Boxes, LayoutTemplate, Plus, Search, Trash2} from 'lucide-react';
import {useCallback, useEffect, useMemo, useState} from 'react';
import {ListServiceTemplates, DeleteServiceTemplate} from '../../wailsjs/go/main/App';
import {store} from '../../wailsjs/go/models';
import PageHeader from '../components/PageHeader';
import TemplateIcon from '../components/TemplateIcon';
import TemplateEditorDialog from '../components/TemplateEditorDialog';
import {useAppDialog} from '../components/AppDialogProvider';
import {Skeleton, SkeletonGridCards} from '../components/Skeleton';
import {templateRouteProtocol, parseDefaultSettings} from '../utils/templateDefaults';
import './TemplatesView.css';

function TemplatesSkeleton() {
    return (
        <>
            <section className="templates-section">
                <Skeleton width={90} height={13} style={{marginBottom: 12}} />
                <SkeletonGridCards count={8} />
            </section>
            <section className="templates-section">
                <Skeleton width={110} height={13} style={{marginBottom: 12}} />
                <SkeletonGridCards count={3} />
            </section>
        </>
    );
}

type EditorState =
    | {mode: 'closed'}
    | {mode: 'create'}
    | {mode: 'edit'; template: store.ServiceTemplate}
    | {mode: 'view'; template: store.ServiceTemplate};

const CATEGORY_LABELS: Record<string, string> = {
    web: 'Web',
    datastore: 'Datastore',
    language: 'Language',
    tooling: 'Tooling',
    image: 'Image',
};

const CATEGORY_ORDER = ['web', 'datastore', 'language', 'tooling', 'image'];

function groupByCategory(list: store.ServiceTemplate[]): Array<[string, store.ServiceTemplate[]]> {
    const groups = new Map<string, store.ServiceTemplate[]>();
    for (const t of list) {
        const key = t.category || 'other';
        if (!groups.has(key)) groups.set(key, []);
        groups.get(key)!.push(t);
    }
    const order = [...CATEGORY_ORDER, ...[...groups.keys()].filter((k) => !CATEGORY_ORDER.includes(k))];
    return order.filter((k) => groups.has(k)).map((k) => [k, groups.get(k)!]);
}

export default function TemplatesView() {
    const [templates, setTemplates] = useState<store.ServiceTemplate[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState('');
    const [query, setQuery] = useState('');
    const [editor, setEditor] = useState<EditorState>({mode: 'closed'});
    const {confirm} = useAppDialog();

    const refresh = useCallback(() => {
        setError('');
        ListServiceTemplates()
            .then((list) => setTemplates(list ?? []))
            .catch((e) => setError(typeof e === 'string' ? e : e?.message || 'Failed to load templates'))
            .finally(() => setLoading(false));
    }, []);

    useEffect(() => {
        refresh();
    }, [refresh]);

    const filtered = useMemo(() => {
        const q = query.trim().toLowerCase();
        if (!q) return templates;
        return templates.filter((t) =>
            t.name.toLowerCase().includes(q) ||
            (t.description || '').toLowerCase().includes(q) ||
            (CATEGORY_LABELS[t.category] || t.category || '').toLowerCase().includes(q)
        );
    }, [templates, query]);

    const builtinGroups = useMemo(() => groupByCategory(filtered.filter((t) => t.builtin)), [filtered]);
    const userGroups = useMemo(() => groupByCategory(filtered.filter((t) => !t.builtin)), [filtered]);

    const handleDelete = async (template: store.ServiceTemplate) => {
        if (!await confirm({
            title: 'Delete template?',
            message: `Delete template "${template.name}"?`,
            detail: 'This cannot be undone.',
            confirmLabel: 'Delete',
            danger: true,
        })) return;
        DeleteServiceTemplate(template.id)
            .then(refresh)
            .catch((e) => setError(typeof e === 'string' ? e : e?.message || 'Failed to delete template'));
    };

    const handleEditorClose = () => setEditor({mode: 'closed'});
    const handleEditorSaved = () => {
        setEditor({mode: 'closed'});
        refresh();
    };

    const renderCard = (template: store.ServiceTemplate) => {
        const isBuiltin = template.builtin;
        const portLabel = template.mode === 'image' && template.image
            ? template.image
            : `:${template.port || '—'}`;
        const route = templateRouteProtocol(template.defaultSettings);
        const defaults = parseDefaultSettings(template.defaultSettings);
        const hostPort = defaults.host_port;
        const routeTitle = route === 'tcp'
            ? `TCP routing${hostPort ? ` · preferred host port ${hostPort}` : ''}`
            : 'HTTP routing (Draft reverse proxy)';
        return (
            <article key={template.id} className="template-card" data-builtin={isBuiltin || undefined}>
                <button
                    className="template-card-main"
                    onClick={() => setEditor({mode: isBuiltin ? 'view' : 'edit', template})}
                    title={template.description || template.name}
                >
                    <div className="template-card-icon">
                        <TemplateIcon slug={template.icon} color={template.color || 'currentColor'} size={18}/>
                    </div>
                    <div className="template-card-copy">
                        <span className="template-card-name">{template.name}</span>
                        <span className="template-card-meta">
                            {portLabel}
                            <span className="template-card-sep">·</span>
                            {template.mode === 'image' ? 'image' : 'build'}
                            <span className="template-card-sep">·</span>
                            <span
                                className={`template-card-route template-card-route--${route}`}
                                title={routeTitle}
                            >
                                {route === 'tcp' ? (hostPort ? `TCP :${hostPort}` : 'TCP') : 'HTTP'}
                            </span>
                        </span>
                    </div>
                </button>
                {!isBuiltin && (
                    <button
                        className="btn btn-ghost template-card-delete"
                        onClick={() => { void handleDelete(template); }}
                        title="Delete template"
                    >
                        <Trash2 size={13}/>
                    </button>
                )}
            </article>
        );
    };

    const renderGroups = (groups: Array<[string, store.ServiceTemplate[]]>, emptyLabel: string) => {
        if (groups.length === 0) {
            return <span className="templates-section-empty">{emptyLabel}</span>;
        }
        return groups.map(([category, items]) => (
            <div className="templates-category" key={category}>
                <h4 className="templates-category-title">
                    {CATEGORY_LABELS[category] || category}
                    <span className="templates-category-count">{items.length}</span>
                </h4>
                <div className="templates-grid">
                    {items.map(renderCard)}
                </div>
            </div>
        ));
    };

    return (
        <div className="templates-view">
            <PageHeader
                title="Templates"
                description="Reusable service blueprints. Built-ins are locked — clone one to customize it, or create your own."
                action={
                    <button className="btn btn-primary" onClick={() => setEditor({mode: 'create'})}>
                        <Plus size={15}/> New template
                    </button>
                }
            />

            <div className="templates-layout">
                {error && <p className="form-error">{error}</p>}

                {!loading && templates.length > 0 && (
                    <div className="templates-search">
                        <Search size={14}/>
                        <input
                            type="text"
                            placeholder="Filter templates…"
                            value={query}
                            onChange={(e) => setQuery(e.target.value)}
                        />
                    </div>
                )}

                {loading ? (
                    <TemplatesSkeleton />
                ) : templates.length === 0 ? (
                    <div className="templates-empty">
                        <LayoutTemplate size={20}/>
                        <div className="templates-empty-copy">
                            <strong>No templates yet.</strong>
                            <span>Create a template to give your services a head start with a Dockerfile, port, and default env.</span>
                        </div>
                    </div>
                ) : (
                    <>
                        <section className="templates-section">
                            <h3 className="templates-section-title">
                                <Boxes size={13}/> Built-in
                            </h3>
                            {renderGroups(builtinGroups, 'No built-in templates match your filter.')}
                        </section>

                        <section className="templates-section">
                            <h3 className="templates-section-title">
                                <LayoutTemplate size={13}/> Your templates
                            </h3>
                            {renderGroups(
                                userGroups,
                                templates.some((t) => !t.builtin)
                                    ? 'No custom templates match your filter.'
                                    : 'No custom templates yet. Clone a built-in or click “New template”.'
                            )}
                        </section>
                    </>
                )}
            </div>

            {editor.mode !== 'closed' && (
                <TemplateEditorDialog
                    mode={editor.mode}
                    template={editor.mode === 'create' ? undefined : editor.template}
                    onClose={handleEditorClose}
                    onSaved={handleEditorSaved}
                />
            )}
        </div>
    );
}
