import type {ComponentType} from 'react';
import {LayoutDashboard, FolderOpen, Box, LayoutTemplate, HardDrive, Route, Settings2, KeyRound, Container, SquareTerminal, RefreshCw, LoaderCircle} from 'lucide-react';
import type {LucideProps} from 'lucide-react';
import brandMark from '../assets/logo-mark-trans-cream.png';
import './Sidebar.css';

type IconType = ComponentType<LucideProps>;

export type NavId = 'overview' | 'projects' | 'templates' | 'secrets' | 'volumes' | 'docker' | 'routes' | 'sandboxes' | 'agents' | 'settings';

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
    {id: 'agents', label: 'Agents', icon: SquareTerminal},
];

const FOOTER: NavItem[] = [
    {id: 'settings', label: 'Settings', icon: Settings2},
];

type SidebarProps = {
    active: NavId;
    onSelect: (id: NavId) => void;
    compact?: boolean;
    update?: {state: string; version?: string};
    onRestartToUpdate?: () => void;
};

function BrandMark() {
    return (
        <img
            className="brand-mark"
            src={brandMark}
            width={32}
            height={32}
            alt=""
            draggable={false}
        />
    );
}

export default function Sidebar({active, onSelect, compact, update, onRestartToUpdate}: SidebarProps) {
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
                title={item.label}
            >
                <Icon className="nav-icon" size={14} strokeWidth={2}/>
                <span className="nav-label">{item.label}</span>
            </button>
        );
    };

    return (
        <aside className={'sidebar' + (compact ? ' sidebar--compact' : '')}>
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
                {update?.state === 'ready' && (
                    <button
                        type="button"
                        className="nav-item nav-item--update"
                        onClick={onRestartToUpdate}
                        title={`Restart to install Draft ${update.version ?? ''}`.trim()}
                    >
                        <RefreshCw className="nav-icon" size={14} strokeWidth={2}/>
                        <span className="nav-label">Restart to update</span>
                    </button>
                )}
                {(update?.state === 'checking' || update?.state === 'downloading') && (
                    <div className="nav-item nav-item--update-progress" title={update.state === 'downloading' ? 'Downloading update in the background' : 'Checking for updates'}>
                        <LoaderCircle className="nav-icon spin" size={14} strokeWidth={2}/>
                        <span className="nav-label">{update.state === 'downloading' ? 'Downloading update…' : 'Checking for updates…'}</span>
                    </div>
                )}
                {FOOTER.map(renderItem)}
            </div>
        </aside>
    );
}
