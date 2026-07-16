import type {CSSProperties, ReactNode} from 'react';
import './Skeleton.css';

type SkeletonProps = {
    width?: string | number;
    height?: string | number;
    className?: string;
    style?: CSSProperties;
    /** `circle` for avatars/dots, `pill` for chips/buttons */
    variant?: 'rect' | 'circle' | 'pill';
};

/** Single shimmer bar — the building block for all loading layouts. */
export function Skeleton({
    width,
    height = 12,
    className = '',
    style,
    variant = 'rect',
}: SkeletonProps) {
    const variantClass =
        variant === 'circle' ? ' skel--circle' : variant === 'pill' ? ' skel--pill' : '';
    return (
        <span
            className={'skel' + variantClass + (className ? ` ${className}` : '')}
            style={{width, height, ...style}}
            aria-hidden="true"
        />
    );
}

/** Non-interactive wrapper that marks a region as busy. */
export function SkeletonBlock({
    className = '',
    label = 'Loading',
    children,
}: {
    className?: string;
    label?: string;
    children: ReactNode;
}) {
    return (
        <div
            className={'skel-block' + (className ? ` ${className}` : '')}
            aria-busy="true"
            aria-label={label}
        >
            {children}
        </div>
    );
}

export function SkeletonStack({
    className = '',
    gap,
    children,
}: {
    className?: string;
    gap?: number | string;
    children: ReactNode;
}) {
    return (
        <div className={'skel-stack' + (className ? ` ${className}` : '')} style={gap != null ? {gap} : undefined}>
            {children}
        </div>
    );
}

/** Stat / metric tile grid (Overview, Routes, Volumes, Docker). */
export function SkeletonStats({count = 4, columns}: {count?: number; columns?: 3 | 4}) {
    const cols = columns ?? (count <= 3 ? 3 : 4);
    return (
        <div className={`skel-stats skel-stats--${cols}`}>
            {Array.from({length: count}, (_, i) => (
                <div key={i} className="skel-stat">
                    <Skeleton width={72} height={10} />
                    <Skeleton width={i % 2 === 0 ? 56 : 72} height={28} />
                    <Skeleton width="55%" height={10} />
                </div>
            ))}
        </div>
    );
}

/** Expandable / stacked list cards (Projects, Secrets, Sandboxes). */
export function SkeletonListCards({
    count = 4,
    withActions = false,
    className = '',
}: {
    count?: number;
    withActions?: boolean;
    className?: string;
}) {
    return (
        <SkeletonStack className={className} gap={8}>
            {Array.from({length: count}, (_, i) => (
                <div
                    key={i}
                    className={'skel-list-card' + (withActions ? ' skel-list-card--actions' : '')}
                >
                    <Skeleton width={8} height={8} />
                    <div className="skel-col">
                        <Skeleton width={i % 2 === 0 ? '42%' : '55%'} height={14} />
                        <Skeleton width={i % 3 === 0 ? '28%' : '38%'} height={10} />
                    </div>
                    {withActions && (
                        <div className="skel-row" style={{flex: 'none'}}>
                            <Skeleton width={64} height={28} variant="pill" />
                            <Skeleton width={28} height={28} />
                        </div>
                    )}
                </div>
            ))}
        </SkeletonStack>
    );
}

/** Data-table body placeholder (Routes, Volumes, Docker). */
export function SkeletonTable({
    rows = 8,
    columns = ['1fr', '80px', '1fr', '100px', '72px'],
}: {
    rows?: number;
    columns?: string[];
}) {
    const template = columns.join(' ');
    return (
        <div className="skel-table">
            {Array.from({length: rows}, (_, i) => (
                <div key={i} className="skel-table-row" style={{gridTemplateColumns: template}}>
                    {columns.map((_, c) => (
                        <Skeleton
                            key={c}
                            width={c === 0 ? '70%' : c === columns.length - 1 ? 36 : '85%'}
                            height={c === columns.length - 1 ? 28 : 12}
                        />
                    ))}
                </div>
            ))}
        </div>
    );
}

/** Template / service picker grid. */
export function SkeletonGridCards({count = 8, minWidth = 190}: {count?: number; minWidth?: number}) {
    return (
        <div className="skel-grid" style={{gridTemplateColumns: `repeat(auto-fill, minmax(${minWidth}px, 1fr))`}}>
            {Array.from({length: count}, (_, i) => (
                <div key={i} className="skel-grid-card">
                    <Skeleton width={26} height={26} />
                    <div className="skel-col">
                        <Skeleton width="70%" height={12} />
                        <Skeleton width="45%" height={10} />
                    </div>
                </div>
            ))}
        </div>
    );
}

/** Settings section with label + control stubs. */
export function SkeletonSettings({sections = 2, rowsPerSection = 3}: {sections?: number; rowsPerSection?: number}) {
    return (
        <SkeletonStack gap={18}>
            {Array.from({length: sections}, (_, s) => (
                <div key={s}>
                    <Skeleton width={100} height={10} style={{marginBottom: 10}} />
                    <div className="skel-settings-section">
                        {Array.from({length: rowsPerSection}, (_, r) => (
                            <div key={r} className="skel-settings-row">
                                <div className="skel-col" style={{maxWidth: 360}}>
                                    <Skeleton width="55%" height={13} />
                                    <Skeleton width="90%" height={10} />
                                </div>
                                <Skeleton width={44} height={24} variant="pill" />
                            </div>
                        ))}
                    </div>
                </div>
            ))}
        </SkeletonStack>
    );
}

/** Overview activity / project focus rows. */
export function SkeletonFeedRows({count = 4}: {count?: number}) {
    return (
        <SkeletonStack gap={0}>
            {Array.from({length: count}, (_, i) => (
                <div
                    key={i}
                    className="skel-row"
                    style={{padding: '12px 0', borderTop: i === 0 ? undefined : '1px solid var(--border)'}}
                >
                    <Skeleton width={8} height={8} variant="circle" />
                    <Skeleton width={52} height={10} />
                    <Skeleton width={88} height={10} />
                    <Skeleton className="skel-grow" height={10} />
                </div>
            ))}
        </SkeletonStack>
    );
}
