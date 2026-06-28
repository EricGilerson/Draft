import {type ReactNode, useCallback, useEffect, useRef, useState} from 'react';
import './ResizablePanel.css';

type ResizablePanelProps = {
    children: ReactNode;
    side: 'left' | 'right';
    defaultWidth: number;
    minWidth?: number;
    maxWidth?: number;
    storageKey?: string;
    className?: string;
};

function readStored(key: string | undefined, fallback: number, min: number, max: number): number {
    if (!key) return fallback;
    const raw = localStorage.getItem(key);
    if (!raw) return fallback;
    const n = Number(raw);
    return Number.isFinite(n) ? Math.min(max, Math.max(min, n)) : fallback;
}

export default function ResizablePanel({
    children,
    side,
    defaultWidth,
    minWidth = 280,
    maxWidth = 700,
    storageKey,
    className = '',
}: ResizablePanelProps) {
    const [width, setWidth] = useState(() => readStored(storageKey, defaultWidth, minWidth, maxWidth));
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

    useEffect(() => {
        if (storageKey) localStorage.setItem(storageKey, String(width));
    }, [width, storageKey]);

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
