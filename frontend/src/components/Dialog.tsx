import {ReactNode, useEffect} from 'react';
import {X} from 'lucide-react';
import './Dialog.css';

type DialogProps = {
    title: string;
    children: ReactNode;
    footer?: ReactNode;
    onClose: () => void;
    /** Wider layout for dense content (e.g. sync preview tables). */
    wide?: boolean;
};

export default function Dialog({title, children, footer, onClose, wide}: DialogProps) {
    useEffect(() => {
        const onKey = (e: KeyboardEvent) => {
            if (e.key === 'Escape') onClose();
        };
        window.addEventListener('keydown', onKey);
        return () => window.removeEventListener('keydown', onKey);
    }, [onClose]);

    return (
        <div className="dialog-overlay" onClick={onClose}>
            <div
                className={'dialog' + (wide ? ' dialog--wide' : '')}
                role="dialog"
                aria-modal="true"
                onClick={(e) => e.stopPropagation()}
            >
                <div className="dialog-header">
                    <h2 className="dialog-title">{title}</h2>
                    <button className="dialog-close" onClick={onClose} aria-label="Close">
                        <X size={18}/>
                    </button>
                </div>
                <div className="dialog-body">{children}</div>
                {footer && <div className="dialog-footer">{footer}</div>}
            </div>
        </div>
    );
}
