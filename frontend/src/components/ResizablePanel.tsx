import {type ReactNode, useCallback, useEffect, useRef, useState} from 'react';
import './ResizablePanel.css';

type ResizablePanelProps = {
    children: ReactNode;
    side: 'left' | 'right';
    defaultWidth: number;
    minWidth?: number;
    maxWidth?: number;
    className?: string;
};

export default function ResizablePanel({
    children,
    side,
    defaultWidth,
    minWidth = 280,
    maxWidth = 700,
    className = '',
}: ResizablePanelProps) {
    const [width, setWidth] = useState(defaultWidth);
    const dragging = useRef(false);
    const startX = useRef(0);
    const startWidth = useRef(0);

    const onMouseDown = useCallback(
        (e: React.MouseEvent) => {
            e.preventDefault();
            dragging.current = true;
            startX.current = e.clientX;
            startWidth.current = width;
            document.body.style.cursor = 'col-resize';
            document.body.style.userSelect = 'none';
        },
        [width],
    );

    useEffect(() => {
        const onMouseMove = (e: MouseEvent) => {
            if (!dragging.current) return;
            const delta = e.clientX - startX.current;
            const newWidth = side === 'right'
                ? startWidth.current - delta
                : startWidth.current + delta;
            setWidth(Math.min(maxWidth, Math.max(minWidth, newWidth)));
        };

        const onMouseUp = () => {
            if (!dragging.current) return;
            dragging.current = false;
            document.body.style.cursor = '';
            document.body.style.userSelect = '';
        };

        window.addEventListener('mousemove', onMouseMove);
        window.addEventListener('mouseup', onMouseUp);
        return () => {
            window.removeEventListener('mousemove', onMouseMove);
            window.removeEventListener('mouseup', onMouseUp);
        };
    }, [side, minWidth, maxWidth]);

    const handlePosition = side === 'right' ? 'resizable-handle--left' : 'resizable-handle--right';

    return (
        <div
            className={`resizable-panel ${className}`}
            style={{width, flexShrink: 0}}
        >
            <div
                className={`resizable-handle ${handlePosition}`}
                onMouseDown={onMouseDown}
            />
            {children}
        </div>
    );
}
