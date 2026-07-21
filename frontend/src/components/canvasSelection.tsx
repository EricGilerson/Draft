import {createContext, useContext} from 'react';

export type SelectedVolume = {
    parentNodeId: string;
    index: number;
    parentLabel?: string;
};

type CanvasSelectionContextValue = {
    selectVolume: (volume: SelectedVolume) => void;
    selectedVolume: SelectedVolume | null;
};

export const CanvasSelectionContext = createContext<CanvasSelectionContextValue | null>(null);

export function useCanvasSelection() {
    return useContext(CanvasSelectionContext);
}

export function parseVolumeNodeId(id: string): {parentNodeId: string; index: number} | null {
    if (!id.startsWith('vol:')) return null;
    const rest = id.slice(4);
    const lastColon = rest.lastIndexOf(':');
    if (lastColon < 0) return null;
    const index = Number(rest.slice(lastColon + 1));
    if (!Number.isFinite(index)) return null;
    return {parentNodeId: rest.slice(0, lastColon), index};
}
