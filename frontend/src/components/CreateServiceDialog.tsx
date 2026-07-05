import {useCallback, useEffect, useMemo, useState} from 'react';
import {
    CreateNode,
    CreateNodeFromTemplate,
    ListServiceTemplates,
    SelectServiceRoot,
} from '../../wailsjs/go/main/App';
import {deploy, store} from '../../wailsjs/go/models';
import Dialog from './Dialog';
import TemplateIcon from './TemplateIcon';
import {buildImageOptions, CUSTOM_IMAGE_VALUE} from '../utils/imageRef';
import VolumeEditor, {VolumeEntry, parseVolumeEntries, serializeVolumeEntries} from './VolumeEditor';
import './CreateServiceDialog.css';

type VolumeCapability = {show?: boolean; editable?: boolean};

type TemplateSchema = {
    serviceRoot?: 'optional' | 'hidden';
    dockerfile?: 'optional' | 'hidden';
    wizardSteps?: {id: string; title: string}[];
    settings?: Record<string, {default?: string; hidden?: boolean; label?: string; type?: string; options?: string[]}>;
    hideSections?: string[];
    volumes?: VolumeCapability;
};

function parseSchema(raw: string): TemplateSchema {
    if (!raw) return {};
    try {
        return JSON.parse(raw) as TemplateSchema;
    } catch {
        return {};
    }
}

const CATEGORY_LABELS: Record<string, string> = {
    web: 'Web',
    datastore: 'Datastore',
    language: 'Language',
};

const CATEGORY_ORDER = ['web', 'datastore', 'language'];

type Step = 'template' | 'identity' | 'source' | 'volumes' | 'review';

type Props = {
    projectId: number;
    onClose: () => void;
    onCreated: (node: store.CanvasNode, template?: store.ServiceTemplate) => void;
    /** Position for the new node on the canvas. */
    position: {x: number; y: number};
};

export default function CreateServiceDialog({projectId, onClose, onCreated, position}: Props) {
    const [templates, setTemplates] = useState<store.ServiceTemplate[]>([]);
    const [loading, setLoading] = useState(true);
    const [loadError, setLoadError] = useState('');

    const [selected, setSelected] = useState<store.ServiceTemplate | null>(null);
    const [blankMode, setBlankMode] = useState(false);
    const [search, setSearch] = useState('');

    const [label, setLabel] = useState('');
    const [serviceRoot, setServiceRoot] = useState('');
    const [overrides, setOverrides] = useState<Record<string, string>>({});
    // imageChoice is the select's value: a full ref from the curated list, or
    // CUSTOM_IMAGE_VALUE when the user picks "Custom…". imageCustom holds the
    // free-text ref when in custom mode. Effective ref is computed in create().
    const [imageChoice, setImageChoice] = useState('');
    const [imageCustom, setImageCustom] = useState('');
    const [volumes, setVolumes] = useState<VolumeEntry[]>([]);
    const [step, setStep] = useState<Step>('template');
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState('');
    const [warnings, setWarnings] = useState<string[]>([]);

    useEffect(() => {
        ListServiceTemplates()
            .then((list) => {
                setTemplates(list ?? []);
                setLoading(false);
            })
            .catch((e) => {
                setLoadError(typeof e === 'string' ? e : e?.message || 'Failed to load templates');
                setLoading(false);
            });
    }, []);

    const schema = useMemo<TemplateSchema>(() => {
        if (!selected) return {};
        return parseSchema(selected.schema);
    }, [selected]);

    // Curated version options for image-mode templates. Empty for build-mode or
    // templates without a curated list — the picker falls back to free-text.
    const imageOptions = useMemo(() => {
        if (!selected || selected.mode !== 'image' || !selected.image) return [];
        return buildImageOptions(selected.image, selected.imageTags);
    }, [selected]);

    const isImageMode = !!selected && selected.mode === 'image';

    // Steps come from the template schema; fall back to a sensible default.
    const steps = useMemo<Step[]>(() => {
        if (blankMode) return ['template', 'identity', 'review'];
        const ids = (schema.wizardSteps?.map((s) => s.id) ?? []) as Step[];
        if (ids.length === 0) {
            return ['template', 'identity', 'source', 'review'];
        }
        return ids;
    }, [schema, blankMode]);

    const showSourceStep = !blankMode && schema.serviceRoot !== 'hidden' && steps.includes('source');
    const showVolumesStep = !blankMode && !!schema.volumes?.show && steps.includes('volumes');
    const volumesEditable = !!schema.volumes?.editable;
    const effectiveSteps: Step[] = useMemo(() => {
        let s = steps;
        if (!showSourceStep) s = s.filter((x) => x !== 'source');
        if (!showVolumesStep) s = s.filter((x) => x !== 'volumes');
        return s;
    }, [steps, showSourceStep, showVolumesStep]);

    const currentIdx = effectiveSteps.indexOf(step);
    const canGoBack = currentIdx > 0;
    const canGoNext = currentIdx >= 0 && currentIdx < effectiveSteps.length - 1;

    const grouped = useMemo(() => {
        const q = search.trim().toLowerCase();
        const filtered = templates.filter((t) => {
            if (!q) return true;
            return (
                t.name.toLowerCase().includes(q) ||
                (t.description || '').toLowerCase().includes(q) ||
                (t.category || '').toLowerCase().includes(q)
            );
        });
        const groups: {category: string; items: store.ServiceTemplate[]}[] = [];
        for (const cat of CATEGORY_ORDER) {
            const items = filtered.filter((t) => t.category === cat);
            if (items.length) groups.push({category: cat, items});
        }
        const rest = filtered.filter((t) => !CATEGORY_ORDER.includes(t.category));
        if (rest.length) groups.push({category: 'other', items: rest});
        return groups;
    }, [templates, search]);

    const pickTemplate = (t: store.ServiceTemplate) => {
        setSelected(t);
        setBlankMode(false);
        setLabel('');
        setServiceRoot('');
        setOverrides({});
        setError('');
        setWarnings([]);
        // Default the version picker to the template's default image ref; clear
        // any prior custom text. For build-mode templates these stay empty and
        // the picker isn't rendered.
        setImageChoice(t.mode === 'image' && t.image ? t.image : '');
        setImageCustom('');
        // Seed the volumes editor from the template's defaults (e.g. the
        // Postgres /var/lib/postgresql/data named volume). The user edits these
        // on the Volumes step; if the template has no Volumes capability the
        // step is skipped and an empty list is stamped.
        setVolumes(parseVolumeEntries(t.volumes));
        // Default overrides from the schema's field defaults.
        const seeded: Record<string, string> = {};
        if (schema?.settings) {
            for (const [key, spec] of Object.entries(schema.settings)) {
                if (spec?.hidden) continue;
                if (spec?.default !== undefined && spec.default !== '') seeded[key] = spec.default;
            }
        }
        setOverrides(seeded);
        setStep('identity');
    };

    const pickBlank = () => {
        setSelected(null);
        setBlankMode(true);
        setLabel('');
        setServiceRoot('');
        setOverrides({});
        setError('');
        setWarnings([]);
        setImageChoice('');
        setImageCustom('');
        setVolumes([]);
        setStep('identity');
    };

    const browseRoot = useCallback(async () => {
        try {
            const picked = await SelectServiceRoot(projectId);
            if (picked) setServiceRoot(picked);
        } catch {
            /* user cancelled */
        }
    }, [projectId]);

    const next = () => {
        setError('');
        const idx = effectiveSteps.indexOf(step);
        if (idx < 0 || idx >= effectiveSteps.length - 1) return;
        setStep(effectiveSteps[idx + 1]);
    };

    const back = () => {
        const idx = effectiveSteps.indexOf(step);
        if (idx <= 0) return;
        setStep(effectiveSteps[idx - 1]);
    };

    const create = useCallback(async () => {
        setBusy(true);
        setError('');
        setWarnings([]);
        try {
            const id = generateId();
            const name = label.trim() || id;
            if (blankMode) {
                const node = await CreateNode(id, name, projectId, position.x, position.y);
                onCreated(node, undefined);
                return;
            }
            if (!selected) {
                setError('Pick a template first.');
                return;
            }
            // Compose the effective image ref from the version picker: a curated
            // ref, the custom free-text, or fall back to the template default.
            let effectiveOverrides = overrides;
            if (isImageMode) {
                let imageRef = '';
                if (imageChoice === CUSTOM_IMAGE_VALUE) {
                    imageRef = imageCustom.trim();
                } else if (imageChoice) {
                    imageRef = imageChoice;
                } else if (selected.image) {
                    imageRef = selected.image;
                }
                if (imageRef) {
                    effectiveOverrides = {...overrides, image: imageRef};
                }
            }
            // Only send volume_mounts when the wizard actually exposed the
            // Volumes step; otherwise let stamp.go apply the template default
            // (which is empty for templates without a Volumes capability).
            if (showVolumesStep) {
                effectiveOverrides = {...effectiveOverrides, volume_mounts: serializeVolumeEntries(volumes)};
            }
            const req = new deploy.CreateNodeFromTemplateRequest({
                id,
                label: name,
                projectId,
                x: position.x,
                y: position.y,
                templateId: selected.id,
                serviceRoot: serviceRoot.trim(),
                overrides: effectiveOverrides,
            });
            const res = await CreateNodeFromTemplate(req);
            onCreated(res.node, selected);
            if (res.warnings && res.warnings.length) {
                setWarnings(res.warnings);
            }
        } catch (e) {
            const msg = typeof e === 'string' ? e : (e as {message?: string})?.message || 'Failed to create service';
            setError(msg);
        } finally {
            setBusy(false);
        }
    }, [blankMode, selected, label, projectId, position, serviceRoot, overrides, onCreated, isImageMode, imageChoice, imageCustom, showVolumesStep, volumes]);

    const close = () => {
        if (busy) return;
        onClose();
    };

    const footer = (
        <div className="csd-footer">
            <div className="csd-footer-stepper">
                {effectiveSteps.map((s, i) => (
                    <span
                        key={s}
                        className={`csd-step-dot ${i === currentIdx ? 'is-active' : ''} ${i < currentIdx ? 'is-done' : ''}`}
                    />
                ))}
            </div>
            <div className="csd-footer-actions">
                {warnings.length > 0 && (
                    <span className="csd-warnings" title={warnings.join('\n')}>
                        {warnings.length} warning{warnings.length > 1 ? 's' : ''}
                    </span>
                )}
                <button className="btn btn-ghost" onClick={close} disabled={busy}>
                    Cancel
                </button>
                {canGoBack && (
                    <button className="btn btn-ghost" onClick={back} disabled={busy}>
                        Back
                    </button>
                )}
                {canGoNext ? (
                    <button className="btn btn-primary" onClick={next} disabled={busy}>
                        Next
                    </button>
                ) : (
                    <button className="btn btn-primary" onClick={create} disabled={busy}>
                        {busy ? 'Creating…' : 'Create'}
                    </button>
                )}
            </div>
        </div>
    );

    const title = blankMode ? 'New service (blank)' : selected ? `New service · ${selected.name}` : 'New service';

    return (
        <Dialog title={title} onClose={close} footer={footer}>
            <div className="csd-body">
                {error && <p className="form-error">{error}</p>}

                {step === 'template' && (
                    <div className="csd-template-step">
                        <div className="csd-search-row">
                            <input
                                className="input"
                                placeholder="Search templates"
                                value={search}
                                onChange={(e) => setSearch(e.target.value)}
                            />
                        </div>
                        {loading ? (
                            <div className="csd-loading">Loading templates…</div>
                        ) : loadError ? (
                            <p className="form-error">{loadError}</p>
                        ) : (
                            <div className="csd-template-groups">
                                {grouped.map((group) => (
                                    <section key={group.category} className="csd-template-group">
                                        <h3 className="csd-group-title">{CATEGORY_LABELS[group.category] || group.category}</h3>
                                        <div className="csd-template-grid">
                                            {group.items.map((t) => (
                                                <button
                                                    key={t.id}
                                                    className={`csd-template-card ${selected?.id === t.id ? 'is-selected' : ''}`}
                                                    onClick={() => pickTemplate(t)}
                                                    type="button"
                                                >
                                                    <div className="csd-template-card-icon">
                                                        <TemplateIcon slug={t.icon} color={t.color || 'currentColor'} size={22}/>
                                                    </div>
                                                    <div className="csd-template-card-copy">
                                                        <span className="csd-template-card-name">{t.name}</span>
                                                        <span className="csd-template-card-sub">
                                                            {t.mode === 'image' && t.image
                                                                ? t.image
                                                                : `:${t.port || '—'}`}
                                                        </span>
                                                    </div>
                                                </button>
                                            ))}
                                        </div>
                                    </section>
                                ))}
                                <button className="csd-blank-card" onClick={pickBlank} type="button">
                                    <strong>Start blank</strong>
                                    <span>Configure everything yourself in the Settings tab.</span>
                                </button>
                            </div>
                        )}
                    </div>
                )}

                {step === 'identity' && (
                    <div className="csd-identity-step">
                        <label className="csd-field">
                            <span className="csd-field-label">Service name</span>
                            <input
                                className="input"
                                placeholder={blankMode ? 'Service name (optional)' : 'Service name'}
                                value={label}
                                onChange={(e) => setLabel(e.target.value)}
                                autoFocus
                            />
                        </label>
                        {!blankMode && selected && (
                            <div className="csd-template-summary">
                                <div className="csd-template-card-icon">
                                    <TemplateIcon slug={selected.icon} color={selected.color || 'currentColor'} size={20}/>
                                </div>
                                <div>
                                    <div className="csd-summary-name">{selected.name}</div>
                                    <div className="csd-summary-sub">
                                        {selected.mode === 'image'
                                            ? `Image · ${selected.image || '—'}`
                                            : `Build · :${selected.port || '—'}`}
                                    </div>
                                </div>
                            </div>
                        )}
                        {!blankMode && isImageMode && imageOptions.length > 0 && (
                            <label className="csd-field">
                                <span className="csd-field-label">Version</span>
                                <select
                                    className="input select-styled"
                                    value={imageChoice}
                                    onChange={(e) => setImageChoice(e.target.value)}
                                >
                                    {imageOptions.map((opt) => (
                                        <option key={opt.ref} value={opt.ref}>{opt.label}</option>
                                    ))}
                                    <option value={CUSTOM_IMAGE_VALUE}>Custom…</option>
                                </select>
                                {imageChoice === CUSTOM_IMAGE_VALUE && (
                                    <input
                                        className="input csd-custom-image-input"
                                        placeholder="e.g. postgres:15-alpine"
                                        value={imageCustom}
                                        onChange={(e) => setImageCustom(e.target.value)}
                                        autoFocus
                                    />
                                )}
                                <span className="csd-hint">
                                    Pick a curated version or choose Custom to type any image ref.
                                </span>
                            </label>
                        )}
                        {!blankMode && isImageMode && imageOptions.length === 0 && (
                            <label className="csd-field">
                                <span className="csd-field-label">Image</span>
                                <input
                                    className="input"
                                    placeholder={selected?.image || 'e.g. postgres:16-alpine'}
                                    value={imageCustom}
                                    onChange={(e) => {
                                        setImageChoice(CUSTOM_IMAGE_VALUE);
                                        setImageCustom(e.target.value);
                                    }}
                                />
                                <span className="csd-hint">No curated versions for this template — type any image ref.</span>
                            </label>
                        )}
                        {!blankMode && schema.serviceRoot !== 'hidden' && (
                            <label className="csd-field">
                                <span className="csd-field-label">
                                    Service root <span className="csd-optional">(optional)</span>
                                </span>
                                <div className="csd-root-row">
                                    <input
                                        className="input"
                                        placeholder="Project root (default)"
                                        value={serviceRoot}
                                        onChange={(e) => setServiceRoot(e.target.value)}
                                    />
                                    <button className="btn btn-ghost" type="button" onClick={browseRoot}>
                                        Browse
                                    </button>
                                </div>
                                <span className="csd-hint">Leave blank to build from the project root.</span>
                            </label>
                        )}
                        {!blankMode && schema.settings && Object.keys(schema.settings).length > 0 && (
                            <div className="csd-overrides">
                                {Object.entries(schema.settings).map(([key, spec]) => {
                                    if (spec?.hidden) return null;
                                    if (key === 'service_port' || key === 'dockerfile') return null;
                                    return (
                                        <label key={key} className="csd-field">
                                            <span className="csd-field-label">{spec?.label || key}</span>
                                            {spec?.type === 'select' && spec.options ? (
                                                <select
                                                    className="input"
                                                    value={overrides[key] ?? spec.default ?? ''}
                                                    onChange={(e) =>
                                                        setOverrides((o) => ({...o, [key]: e.target.value}))
                                                    }
                                                >
                                                    {spec.options.map((opt) => (
                                                        <option key={opt} value={opt}>{opt}</option>
                                                    ))}
                                                </select>
                                            ) : (
                                                <input
                                                    className="input"
                                                    type={spec?.type === 'number' ? 'number' : 'text'}
                                                    placeholder={spec?.default || ''}
                                                    value={overrides[key] ?? ''}
                                                    onChange={(e) =>
                                                        setOverrides((o) => ({...o, [key]: e.target.value}))
                                                    }
                                                />
                                            )}
                                        </label>
                                    );
                                })}
                            </div>
                        )}
                    </div>
                )}

                {step === 'source' && (
                    <div className="csd-source-step">
                        <label className="csd-field">
                            <span className="csd-field-label">
                                Service root <span className="csd-optional">(optional)</span>
                            </span>
                            <div className="csd-root-row">
                                <input
                                    className="input"
                                    placeholder="Project root (default)"
                                    value={serviceRoot}
                                    onChange={(e) => setServiceRoot(e.target.value)}
                                />
                                <button className="btn btn-ghost" type="button" onClick={browseRoot}>
                                    Browse
                                </button>
                            </div>
                            <span className="csd-hint">
                                Pick the directory your service lives in. Leave blank to use the project root.
                            </span>
                        </label>
                    </div>
                )}

                {step === 'volumes' && (
                    <div className="csd-volumes-step">
                        <p className="csd-step-intro">
                            Persistent storage for this service. Named volumes are Docker-managed — Draft mints
                            a stable name from this service's identity on first deploy, so data survives
                            redeploys. Edit the defaults below or add more.
                        </p>
                        <VolumeEditor
                            entries={volumes}
                            onChange={setVolumes}
                            editable={volumesEditable}
                        />
                        {!volumesEditable && volumes.length === 0 && (
                            <span className="csd-hint">This template defines no volumes.</span>
                        )}
                    </div>
                )}

                {step === 'review' && (
                    <div className="csd-review-step">
                        <div className="csd-review-row">
                            <span className="csd-review-key">Name</span>
                            <span className="csd-review-val">{label.trim() || '(auto)'}</span>
                        </div>
                        {blankMode ? (
                            <div className="csd-review-row">
                                <span className="csd-review-key">Template</span>
                                <span className="csd-review-val">Blank — configure in Settings</span>
                            </div>
                        ) : (
                            <>
                                <div className="csd-review-row">
                                    <span className="csd-review-key">Template</span>
                                    <span className="csd-review-val">{selected?.name}</span>
                                </div>
                                <div className="csd-review-row">
                                    <span className="csd-review-key">Mode</span>
                                    <span className="csd-review-val">
                                        {selected?.mode === 'image' ? `Image · ${selected?.image || '—'}` : 'Build'}
                                    </span>
                                </div>
                                {isImageMode && (
                                    <div className="csd-review-row">
                                        <span className="csd-review-key">Image</span>
                                        <span className="csd-review-val">
                                            {imageChoice === CUSTOM_IMAGE_VALUE
                                                ? (imageCustom.trim() || selected?.image || '—')
                                                : (imageChoice || selected?.image || '—')}
                                        </span>
                                    </div>
                                )}
                                {schema.serviceRoot !== 'hidden' && (
                                    <div className="csd-review-row">
                                        <span className="csd-review-key">Service root</span>
                                        <span className="csd-review-val">{serviceRoot.trim() || 'Project root (default)'}</span>
                                    </div>
                                )}
                                <div className="csd-review-row">
                                    <span className="csd-review-key">Port</span>
                                    <span className="csd-review-val">
                                        {overrides.service_port || selected?.port || '—'}
                                    </span>
                                </div>
                                {showVolumesStep && (
                                    <div className="csd-review-row">
                                        <span className="csd-review-key">Volumes</span>
                                        <span className="csd-review-val">
                                            {volumes.length === 0
                                                ? 'None (ephemeral)'
                                                : volumes.map((v) => `${v.type === 'volume' ? 'vol' : 'bind'}:${v.containerPath}`).join(', ')}
                                        </span>
                                    </div>
                                )}
                            </>
                        )}
                        {warnings.length > 0 && (
                            <div className="csd-review-warnings">
                                {warnings.map((w, i) => (
                                    <p key={i} className="csd-warning-line">{w}</p>
                                ))}
                            </div>
                        )}
                    </div>
                )}
            </div>
        </Dialog>
    );
}

let nodeCounter = 0;

function generateId(): string {
    nodeCounter++;
    const hex = nodeCounter.toString(16).padStart(4, '0');
    const rand = Math.random().toString(16).slice(2, 10);
    return `svc-${hex}-${rand}`;
}
