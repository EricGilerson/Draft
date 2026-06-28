import type {ComponentType} from 'react';
import {LayoutDashboard, FolderOpen, Box, Settings2} from 'lucide-react';
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
    {id: 'projects', label: 'Projects', icon: FolderOpen},
    {id: 'sandboxes', label: 'Sandboxes', icon: Box},
];

const FOOTER: NavItem[] = [
    {id: 'settings', label: 'Settings', icon: Settings2},
];

type SidebarProps = {
    active: NavId;
    onSelect: (id: NavId) => void;
};

function BrandMark() {
    return (
        <svg className="brand-mark" width="20" height="20" viewBox="0 0 22 22" fill="none" aria-hidden="true">
            <circle cx="5.5" cy="11" r="3.5" fill="currentColor"/>
            <circle cx="17" cy="5.5" r="2.8" fill="currentColor" opacity="0.6"/>
            <circle cx="17" cy="16.5" r="2.8" fill="currentColor" opacity="0.6"/>
            <line x1="9" y1="9.3" x2="14.3" y2="7.1" stroke="currentColor" strokeWidth="1.3" opacity="0.38" strokeLinecap="round"/>
            <line x1="9" y1="12.7" x2="14.3" y2="14.9" stroke="currentColor" strokeWidth="1.3" opacity="0.38" strokeLinecap="round"/>
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
                <Icon className="nav-icon" size={14} strokeWidth={2}/>
                <span className="nav-label">{item.label}</span>
            </button>
        );
    };

    return (
        <aside className="sidebar">
            <div className="brand">
                <BrandMark/>
                <span className="brand-name">Draft</span>
                <span className="brand-badge">alpha</span>
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
