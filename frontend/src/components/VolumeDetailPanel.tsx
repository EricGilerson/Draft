import {ArrowUpRight, FolderOpen, HardDrive, X} from 'lucide-react';
import {useCallback, useEffect, useMemo, useState} from 'react';
import {
    DeleteManagedVolume,
    GetNodeSettings,
    ListManagedVolumes,
    SetNodeSetting,
} from '../../wailsjs/go/main/App';
import {deploy} from '../../wailsjs/go/models';
import VolumeEditor, {parseVolumeEntries, serializeVolumeEntries, type VolumeEntry} from './VolumeEditor';
import './NodeDetailPanel.css';

type VolumeDetailPanelProps = {
    parentNodeId: string;
    parentLabel: string;
    volumeIndex: number;
    projectId: number;
    onClose: () => void;
    onOpenService?: () => void;
    onVolumesChanged?: () => void;
};

export default function VolumeDetailPanel({
    parentNodeId,
    parentLabel,
    volumeIndex,
    projectId,
    onClose,
    onOpenService,
    onVolumesChanged,
}: VolumeDetailPanelProps) {
    const [volumes, setVolumes] = useState<VolumeEntry[]>([]);
    const [managedVolumes, setManagedVolumes] = useState<deploy.ManagedVolume[]>([]);

    const refreshManagedVolumes = useCallback(() => {
        ListManagedVolumes(projectId, parentNodeId)
            .then((list) => setManagedVolumes(list ?? []))
            .catch(() => setManagedVolumes([]));
    }, [projectId, parentNodeId]);

    const loadVolumes = useCallback(() => {
        GetNodeSettings(parentNodeId)
            .then((s) => setVolumes(parseVolumeEntries(s?.volume_mounts)))
            .catch(() => setVolumes([]));
        refreshManagedVolumes();
    }, [parentNodeId, refreshManagedVolumes]);

    useEffect(() => {
        loadVolumes();
    }, [loadVolumes]);

    const managedByTarget = useMemo(() => {
        const m: Record<string, deploy.ManagedVolume> = {};
        for (const v of managedVolumes) {
            if (v.target) m[v.target] = v;
        }
        return m;
    }, [managedVolumes]);

    const entry = volumes[volumeIndex];
    const isNamed = entry?.type === 'volume';

    const saveVolumes = useCallback((next: VolumeEntry[]) => {
        SetNodeSetting(parentNodeId, 'volume_mounts', serializeVolumeEntries(next)).then(() => {
            setVolumes(next);
            refreshManagedVolumes();
            onVolumesChanged?.();
            if (volumeIndex >= next.length) {
                onClose();
            }
        });
    }, [parentNodeId, volumeIndex, refreshManagedVolumes, onVolumesChanged, onClose]);

    const deleteDockerVolume = useCallback(async (name: string) => {
        if (!window.confirm(`Delete Docker volume "${name}"? This permanently removes stored data.`)) {
            return;
        }
        try {
            await DeleteManagedVolume(name, false);
            refreshManagedVolumes();
        } catch (e) {
            window.alert(String(e));
        }
    }, [refreshManagedVolumes]);

    if (!entry) {
        return (
            <div className="node-detail-panel">
                <div className="node-detail-header">
                    <h2 className="node-detail-title">Volume not found</h2>
                    <button className="dialog-close" onClick={onClose} aria-label="Close">
                        <X size={16}/>
                    </button>
                </div>
                <div className="node-detail-body">
                    <span className="settings-hint">This volume was removed or changed. Close and select again.</span>
                </div>
            </div>
        );
    }

    return (
        <div className="node-detail-panel">
            <div className="node-detail-header">
                <div className="node-detail-title-row">
                    <div className="node-detail-icon volume-detail-icon">
                        {isNamed ? <HardDrive size={14}/> : <FolderOpen size={14}/>}
                    </div>
                    <div className="volume-detail-titles">
                        <h2 className="node-detail-title volume-detail-title" title={entry.containerPath}>
                            {entry.containerPath.split('/').filter(Boolean).pop() || entry.containerPath || 'Volume'}
                        </h2>
                        <button type="button" className="volume-detail-parent" onClick={onOpenService}>
                            Mounted on {parentLabel}
                            <ArrowUpRight size={12}/>
                        </button>
                    </div>
                </div>
                <button className="dialog-close" onClick={onClose} aria-label="Close">
                    <X size={16}/>
                </button>
            </div>

            <div className="node-detail-body">
                <div className="settings-section">
                    <h3 className="settings-section-title">Volume settings</h3>
                    <span className="settings-hint">
                        Persistent storage for <strong>{parentLabel}</strong>. Named volumes are Docker-managed and
                        survive redeploys; bind mounts map a host directory into the container.
                    </span>
                    <VolumeEditor
                        entries={[entry]}
                        onChange={(rows) => {
                            const next = [...volumes];
                            if (rows.length === 0) {
                                next.splice(volumeIndex, 1);
                            } else {
                                next[volumeIndex] = rows[0];
                            }
                            saveVolumes(next);
                        }}
                        managedByTarget={managedByTarget}
                        onDeleteVolume={deleteDockerVolume}
                        allowAdd={false}
                    />
                </div>
            </div>
        </div>
    );
}
