import {Clock3, ExternalLink, FlaskConical, GitBranch, GitFork, Trash2} from 'lucide-react';
import PageHeader from '../components/PageHeader';
import ServicePill from '../components/ServicePill';
import {SandboxPreview, STATUS_COLORS} from '../lib/dashboardData';
import './WorkspaceViews.css';

type SandboxesViewProps = {
    sandboxes: SandboxPreview[];
};

export default function SandboxesView({sandboxes}: SandboxesViewProps) {
    return (
        <div className="workspace-view">
            <PageHeader
                title="Sandboxes"
                description="A styled staging area for ephemeral environments. This is UI scaffolding until sandbox APIs exist."
                action={
                    <button className="btn btn-primary" disabled>
                        <FlaskConical size={15}/> New sandbox
                    </button>
                }
            />

            <div className="workspace-body workspace-narrow">
                <div className="preview-banner">
                    <GitFork size={14}/>
                    <span>Sandbox actions are intentionally not wired yet. This pass establishes the structure and visual language.</span>
                </div>

                <div className="stack-list">
                    {sandboxes.map((sandbox) => (
                        <article key={sandbox.id} className="sandbox-card">
                            <div className="sandbox-card-main">
                                <div className="sandbox-branch-row">
                                    <span
                                        className="status-dot"
                                        style={{background: STATUS_COLORS[sandbox.status]}}
                                    />
                                    <GitBranch size={14}/>
                                    <span className="sandbox-branch">{sandbox.branch}</span>
                                    {sandbox.previewOnly && <span className="tag-pill">Preview only</span>}
                                </div>

                                <div className="sandbox-meta-row">
                                    <span>Forked from</span>
                                    <span className="sandbox-source">{sandbox.forkedFrom}</span>
                                    <span className="sandbox-meta-divider">·</span>
                                    <Clock3 size={12}/>
                                    <span>{sandbox.createdAt}</span>
                                </div>

                                <div className="sandbox-services">
                                    {sandbox.services.map((service) => (
                                        <ServicePill key={service.id} service={service}/>
                                    ))}
                                </div>
                            </div>

                            <div className="sandbox-actions">
                                <button className="btn btn-ghost" disabled={sandbox.previewOnly}>
                                    <ExternalLink size={14}/> Open
                                </button>
                                <button className="icon-button" disabled aria-label="Delete sandbox">
                                    <Trash2 size={14}/>
                                </button>
                            </div>
                        </article>
                    ))}
                </div>
            </div>
        </div>
    );
}

