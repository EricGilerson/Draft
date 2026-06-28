import type {ComponentType} from 'react';
import type {LucideProps} from 'lucide-react';
import './EmptyState.css';

type EmptyStateProps = {
    icon: ComponentType<LucideProps>;
    title: string;
    description: string;
    chip?: string;
};

export default function EmptyState({icon: Icon, title, description, chip}: EmptyStateProps) {
    return (
        <div className="empty-state">
            <div className="empty-icon">
                <Icon size={26} strokeWidth={1.75}/>
            </div>
            <h2 className="empty-title">{title}</h2>
            <p className="empty-desc">{description}</p>
            {chip && <span className="empty-chip">{chip}</span>}
        </div>
    );
}
