import {Boxes, LayoutTemplate, Plus, Trash2} from 'lucide-react';
import {useCallback, useEffect, useState} from 'react';
import {ListServiceTemplates, DeleteServiceTemplate} from '../../wailsjs/go/main/App';
import {store} from '../../wailsjs/go/models';
import PageHeader from '../components/PageHeader';
import TemplateIcon from '../components/TemplateIcon';
import TemplateEditorDialog from '../components/TemplateEditorDialog';
import './TemplatesView.css';

type EditorState =
    | {mode: 'closed'}
    | {mode: 'create'}
    | {mode: 'edit'; template: store.ServiceTemplate}
    | {mode: 'view'; template: store.ServiceTemplate};

const CATEGORY_LABELS: Record<string, string> = {
    web: 'Web',
    datastore: 'Datastore',
    language: 'Language',
};

export default function TemplatesView() {
    const [templates, setTemplates] = useState<store.ServiceTemplate[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState('');
    const [editor, setEditor] = useState<EditorState>({mode: 'closed'});

    const refresh = useCallback(() => {
        setLoading(true);
        setError('');
        ListServiceTemplates()
            .then((list) => setTemplates(list ?? []))
            .catch((e) => setError(typeof e === 'string' ? e : e?.message || 'Failed to load templates'))
            .finally(() => setLoading(false));
    }, []);

    useEffect(() => {
        refresh();
    }, [refresh]);

    const builtins = templates.filter((t) => t.builtin);
    const userTemplates = templates.filter((t) => !t.builtin);

    const handleDelete = (template: store.ServiceTemplate) => {
        if (!confirm(`Delete template “${template.name}”? This cannot be undone.`)) return;
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
        return (
            <article key={template.id} className="template-card" data-builtin={isBuiltin || undefined}>
                <button
                    className="template-card-main"
                    onClick={() => setEditor({mode: isBuiltin ? 'view' : 'edit', template})}
                >
                    <div className="template-card-icon">
                        <TemplateIcon slug={template.icon} color={template.color || 'currentColor'} size={26}/>
                    </div>
                    <div className="template-card-copy">
                        <div className="template-card-title-row">
                            <h2 className="template-card-name">{template.name}</h2>
                            {isBuiltin && <span className="template-card-builtin">Built-in</span>}
                        </div>
                        <p className="template-card-desc">{template.description || 'No description'}</p>
                        <div className="template-card-meta">
                            <span className="template-card-cat">{CATEGORY_LABELS[template.category] || template.category || '—'}</span>
                            <span className="template-card-sep">·</span>
                            <span className="template-card-port">{portLabel}</span>
                            <span className="template-card-sep">·</span>
                            <span className="template-card-mode">{template.mode === 'image' ? 'image' : 'build'}</span>
                        </div>
                    </div>
                </button>
                {!isBuiltin && (
                    <button
                        className="btn btn-ghost template-card-delete"
                        onClick={() => handleDelete(template)}
                        title="Delete template"
                    >
                        <Trash2 size={14}/>
                    </button>
                )}
            </article>
        );
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

                {loading ? (
                    <div className="templates-loading">Loading templates…</div>
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
                            <div className="templates-grid">
                                {builtins.length > 0 ? builtins.map(renderCard) : (
                                    <span className="templates-section-empty">No built-in templates.</span>
                                )}
                            </div>
                        </section>

                        <section className="templates-section">
                            <h3 className="templates-section-title">
                                <LayoutTemplate size={13}/> Your templates
                            </h3>
                            <div className="templates-grid">
                                {userTemplates.length > 0 ? userTemplates.map(renderCard) : (
                                    <span className="templates-section-empty">
                                        No custom templates yet. Clone a built-in or click “New template”.
                                    </span>
                                )}
                            </div>
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
