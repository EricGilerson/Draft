import type {ComponentType} from 'react';
import {LayoutDashboard, Boxes, FlaskConical, Settings} from 'lucide-react';
import type {LucideProps} from 'lucide-react';
import './Sidebar.css';

type IconType = ComponentType<LucideProps>;

export type NavId = 'overview' | 'projects' | 'sandboxes' | 'settings';

type NavItem = {
    id: NavId;
    label: string;
    icon: IconType;
};

const WORKSPACE: NavItem[] = [
    {id: 'overview', label: 'Overview', icon: LayoutDashboard},
    {id: 'projects', label: 'Projects', icon: Boxes},
    {id: 'sandboxes', label: 'Sandboxes', icon: FlaskConical},
];

const FOOTER: NavItem[] = [
    {id: 'settings', label: 'Settings', icon: Settings},
];

type SidebarProps = {
    active: NavId;
    onSelect: (id: NavId) => void;
};

function BrandMark() {
    // Two connected nodes — a nod to the visual service graph.
    return (
        <svg className="brand-mark" width="22" height="22" viewBox="0 0 24 24" fill="none" aria-hidden="true">
            <circle cx="6.5" cy="6.5" r="3" fill="currentColor"/>
            <circle cx="17.5" cy="17.5" r="3" fill="currentColor"/>
            <path d="M8.6 8.6 L15.4 15.4" stroke="currentColor" strokeWidth="2" strokeLinecap="round"/>
        </svg>
    );
}

export default function Sidebar({active, onSelect}: SidebarProps) {
    const renderItem = (item: NavItem) => {
        const Icon = item.icon;
        const isActive = active === item.id;
        return (
            <button
                key={item.id}
                type="button"
                className={'nav-item' + (isActive ? ' active' : '')}
                onClick={() => onSelect(item.id)}
                aria-current={isActive ? 'page' : undefined}
            >
                <Icon className="nav-icon" size={18} strokeWidth={2}/>
                <span className="nav-label">{item.label}</span>
            </button>
        );
    };

    return (
        <aside className="sidebar">
            <div className="brand">
                <BrandMark/>
                <span className="brand-name">Draft</span>
            </div>

            <nav className="nav">
                <div className="nav-section-label">Workspace</div>
                {WORKSPACE.map(renderItem)}
            </nav>

            <div className="sidebar-footer">
                {FOOTER.map(renderItem)}
            </div>
        </aside>
    );
}
