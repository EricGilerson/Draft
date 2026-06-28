import {ReactNode} from 'react';
import './PageHeader.css';

type PageHeaderProps = {
    title: string;
    description: string;
    action?: ReactNode;
};

export default function PageHeader({title, description, action}: PageHeaderProps) {
    return (
        <div className="page-header">
            <div className="page-header-copy">
                <h1 className="page-header-title">{title}</h1>
                <p className="page-header-description">{description}</p>
            </div>
            {action && <div className="page-header-action">{action}</div>}
        </div>
    );
}

