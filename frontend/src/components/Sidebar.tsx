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
        <svg className="brand-mark" width="28" height="28" viewBox="0 0 28 28" fill="none" aria-hidden="true">
            <path className="brand-mark-frame" d="M4.5 5.5h19v17h-19z"/>
            <path className="brand-mark-route" d="M8 18.5V10h6.2v8.5H20"/>
            <path className="brand-mark-cursor" d="M13.4 8.2 20.8 5.6l-2.8 7.3-1.5-3.1-3.1-1.6Z"/>
            <circle className="brand-mark-port" cx="8" cy="18.5" r="1.8"/>
            <circle className="brand-mark-port" cx="20" cy="18.5" r="1.8"/>
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
