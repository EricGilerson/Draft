import type {ComponentType} from 'react';
import {LayoutDashboard, FolderOpen, Box, LayoutTemplate, HardDrive, Route, Settings2, KeyRound, Container} from 'lucide-react';
import type {LucideProps} from 'lucide-react';
import './Sidebar.css';

type IconType = ComponentType<LucideProps>;

export type NavId = 'overview' | 'projects' | 'templates' | 'secrets' | 'volumes' | 'docker' | 'routes' | 'sandboxes' | 'settings';

type NavItem = {
    id: NavId;
    label: string;
    icon: IconType;
};

const WORKSPACE: NavItem[] = [
    {id: 'overview', label: 'Overview', icon: LayoutDashboard},
    {id: 'projects', label: 'Projects', icon: FolderOpen},
    {id: 'templates', label: 'Templates', icon: LayoutTemplate},
    {id: 'secrets', label: 'Secrets', icon: KeyRound},
    {id: 'volumes', label: 'Volumes', icon: HardDrive},
    {id: 'docker', label: 'Docker', icon: Container},
    {id: 'routes', label: 'Routes', icon: Route},
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
        <svg className="brand-mark" width="32" height="32" viewBox="0 0 32 32" fill="none" aria-hidden="true">
            <rect className="brand-mark-tile" x="3.5" y="3.5" width="25" height="25" rx="7"/>
            <path className="brand-mark-route" d="M10 21.5v-8h6.4l5.6-4.8"/>
            <path className="brand-mark-route brand-mark-route-secondary" d="M16.4 13.5v7.7H23"/>
            <circle className="brand-mark-node" cx="10" cy="21.5" r="2.35"/>
            <circle className="brand-mark-node brand-mark-node-primary" cx="16.4" cy="13.5" r="2.35"/>
            <circle className="brand-mark-node" cx="23" cy="21.2" r="2.35"/>
            <path className="brand-mark-launch" d="M21.2 7.7 25.6 6.4 24.3 10.8"/>
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
