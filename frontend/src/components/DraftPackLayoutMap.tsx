import {useMemo} from 'react';
import {draftpack} from '../../wailsjs/go/models';
import './DraftPackLayoutMap.css';

type Props = {
    layout: draftpack.LayoutPreview;
    layoutMode: 'auto' | 'preserve' | 'grid';
    onLayoutModeChange: (mode: 'auto' | 'preserve' | 'grid') => void;
    /** Hide mode picker when parent already has one. */
    showModePicker?: boolean;
};

/**
 * Mini-map of destination canvas: existing services (muted) + incoming pack
 * services (accent). Overlapping incoming nodes are drawn translucent with a
 * hatch so you can see something is underneath — not hidden.
 */
export default function DraftPackLayoutMap({
    layout,
    layoutMode,
    onLayoutModeChange,
    showModePicker = true,
}: Props) {
    const map = useMemo(() => {
        const minX = layout.minX ?? 0;
        const minY = layout.minY ?? 0;
        const maxX = Math.max(layout.maxX ?? 400, minX + 200);
        const maxY = Math.max(layout.maxY ?? 300, minY + 150);
        const worldW = Math.max(maxX - minX, 1);
        const worldH = Math.max(maxY - minY, 1);
        const viewW = 520;
        const viewH = Math.min(280, Math.max(160, (viewW * worldH) / worldW));
        const scale = Math.min(viewW / worldW, viewH / worldH);
        const padX = (viewW - worldW * scale) / 2;
        const padY = (viewH - worldH * scale) / 2;
        const nw = (layout.nodeWidth || 220) * scale;
        const nh = (layout.nodeHeight || 100) * scale;

        const project = (x: number, y: number) => ({
            left: padX + (x - minX) * scale,
            top: padY + (y - minY) * scale,
        });

        return {viewW, viewH, nw, nh, project, scale};
    }, [layout]);

    const existing = layout.existing ?? [];
    const incoming = layout.incoming ?? [];
    const overlapCount = layout.overlapCount ?? 0;
    const wouldStack = !!layout.wouldOverlapWithoutShift;
    const shifted = !!layout.shifted;

    let statusText = 'Placement looks clear.';
    let statusKind: 'ok' | 'warn' | 'info' = 'ok';
    if (overlapCount > 0) {
        statusText = `${overlapCount} service${overlapCount === 1 ? '' : 's'} will sit on top of existing nodes — switch to Auto or Free grid to separate them.`;
        statusKind = 'warn';
    } else if (shifted && wouldStack) {
        statusText = 'Auto placement moved the pack clear of existing services (relative layout kept).';
        statusKind = 'info';
    } else if (layoutMode === 'grid') {
        statusText = 'Services will be arranged in a free grid next to existing nodes.';
        statusKind = 'info';
    } else if (existing.length === 0) {
        statusText = 'Empty canvas — pack layout will be applied as-is.';
        statusKind = 'ok';
    }

    return (
        <div className={`draftpack-layout-map${overlapCount > 0 ? ' has-overlap' : ''}`}>
            <div className="draftpack-layout-map-header">
                <h4>Canvas placement</h4>
                {showModePicker && (
                    <select
                        className="input draftpack-layout-mode-select"
                        value={layoutMode}
                        onChange={(e) => onLayoutModeChange(e.target.value as 'auto' | 'preserve' | 'grid')}
                    >
                        <option value="auto">Auto (avoid overlap)</option>
                        <option value="preserve">Keep pack coordinates</option>
                        <option value="grid">Free grid</option>
                    </select>
                )}
            </div>

            <p className={`draftpack-layout-status is-${statusKind}`}>{statusText}</p>

            <div className="draftpack-layout-legend">
                <span className="draftpack-layout-swatch is-existing"/> Existing
                <span className="draftpack-layout-swatch is-incoming"/> Importing
                <span className="draftpack-layout-swatch is-overlap"/> Overlap
            </div>

            <div
                className="draftpack-layout-viewport"
                style={{width: map.viewW, height: map.viewH}}
                role="img"
                aria-label="Canvas placement preview"
            >
                {/* Existing nodes first (underneath) */}
                {existing.map((n) => {
                    const p = map.project(n.x, n.y);
                    return (
                        <div
                            key={`ex-${n.key}`}
                            className="draftpack-layout-node is-existing"
                            style={{left: p.left, top: p.top, width: map.nw, height: map.nh}}
                            title={n.label}
                        >
                            <span className="draftpack-layout-node-label">{n.label}</span>
                        </div>
                    );
                })}
                {/* Incoming on top — overlapping ones stay translucent so existing shows through */}
                {incoming.map((n) => {
                    const p = map.project(n.x, n.y);
                    const title = n.overlaps
                        ? `${n.label} overlaps: ${(n.overlapsLabels ?? []).join(', ') || 'existing service'}`
                        : n.label;
                    return (
                        <div
                            key={`in-${n.key}`}
                            className={`draftpack-layout-node is-incoming${n.overlaps ? ' is-overlap' : ''}`}
                            style={{left: p.left, top: p.top, width: map.nw, height: map.nh}}
                            title={title}
                        >
                            <span className="draftpack-layout-node-label">{n.label}</span>
                            {n.overlaps && <span className="draftpack-layout-node-badge">on top</span>}
                        </div>
                    );
                })}
                {existing.length === 0 && incoming.length === 0 && (
                    <div className="draftpack-layout-empty">No services to place</div>
                )}
            </div>

            {overlapCount > 0 && (
                <ul className="draftpack-layout-overlap-list">
                    {incoming.filter((n) => n.overlaps).map((n) => (
                        <li key={n.key}>
                            <strong>{n.label}</strong>
                            {' '}covers {(n.overlapsLabels ?? []).join(', ') || 'an existing service'}
                        </li>
                    ))}
                </ul>
            )}
        </div>
    );
}
