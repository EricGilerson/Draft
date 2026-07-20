import {FolderOpen, FileSearch, Plus, Trash2, GitBranch, RefreshCw, ArrowUpRight} from 'lucide-react';
import {useCallback, useEffect, useMemo, useState, type ReactNode} from 'react';
import {
    GetServiceRoot, SetServiceRoot, SelectServiceRoot,
    SelectFile, ParseDockerfileExpose,
    IsGitRepo, ListGitBranches, SetGitDeploymentConfig, GetGitHookStatus,
    SetNodeSetting,
    GetNode, GetServiceTemplate, ListManagedVolumes, DeleteManagedVolume, GetNodeConfigStatus,
    PreviewDeleteService, DeleteNode,
    PreviewLinkToSharedRoot, LinkToSharedRoot, ListShareTargets,
} from '../../wailsjs/go/main/App';
import {dockerfile, deploy, main, store} from '../../wailsjs/go/models';
import {buildImageOptions, CUSTOM_IMAGE_VALUE} from '../utils/imageRef';
import {useServiceConfigEditor} from '../lib/serviceConfigEditor';
import {getSettingStagingState} from '../lib/settingStaging';
import {useLinkedServiceTarget} from '../lib/linkedService';
import SettingStagingNote from './SettingStagingNote';
import VolumeEditor, {VolumeEntry, parseVolumeEntries, serializeVolumeEntries} from './VolumeEditor';
import Dialog from './Dialog';
import {useAppDialog} from './AppDialogProvider';
import {Skeleton} from './Skeleton';
import './EnvironmentSwitcher.css';

type VolumeDisposition = 'orphan' | 'delete';
type ShareMode = 'same' | 'any';

function matchReasonLabel(reason?: string): string {
    switch (reason) {
        case 'label+template': return 'same name & template';
        case 'template': return 'same template';
        case 'label': return 'same name';
        case 'image+port': return 'same image & port';
        case 'image': return 'same image';
        default: return 'matched';
    }
}

type DeployTrigger = 'manual' | 'on_commit' | 'on_push';

type TemplateSchema = {
    serviceRoot?: 'optional' | 'hidden';
    dockerfile?: 'optional' | 'hidden';
    wizardSteps?: {id: string; title: string}[];
    settings?: Record<string, {default?: string; hidden?: boolean; label?: string; type?: string; options?: string[]}>;
    hideSections?: string[];
};

function parseSchema(raw: string): TemplateSchema {
    if (!raw) return {};
    try {
        return JSON.parse(raw) as TemplateSchema;
    } catch {
        return {};
    }
}

type DeletePreview = {
    label: string;
    isRunning: boolean;
    managedVolumeCount: number;
    dependents: deploy.ReferenceDependent[];
    activeAliases: string[];
};

type SettingsTabProps = {
    nodeId: string;
    projectId: number;
    projectPath: string;
    serviceLabel: string;
    onServicesChanged?: () => void;
    onServiceDeleted?: () => void;
    onOpenRootService?: (projectId: number, rootNodeId: string, rootEnvironmentId: number) => void;
};

type LabelEntry = {
    key: string;
    value: string;
};

export default function SettingsTab({nodeId, projectId, projectPath, serviceLabel, onServicesChanged, onServiceDeleted, onOpenRootService}: SettingsTabProps) {
    const {
        appliedSettings,
        stagedSettings,
        draftSettings,
        committedSettings,
        effectiveSettings,
        updateDraftSetting,
        hasStagedChanges,
        loading: configLoading,
        isSessionDirty,
        reload,
    } = useServiceConfigEditor();
    const {loading: linkLoading, isLinked, linkInfo, targetNodeId, refresh: refreshLink} = useLinkedServiceTarget(nodeId);
    const readOnly = isLinked;
    const [linkedHasStagedChanges, setLinkedHasStagedChanges] = useState(false);
    const {alert} = useAppDialog();

    const [shareDialogOpen, setShareDialogOpen] = useState(false);
    const [shareTargets, setShareTargets] = useState<deploy.ShareTargetEnvironment[]>([]);
    const [shareMode, setShareMode] = useState<ShareMode>('same');
    const [shareEnvId, setShareEnvId] = useState(0);
    const [shareRootId, setShareRootId] = useState('');
    const [shareVolumes, setShareVolumes] = useState<VolumeDisposition>('orphan');
    const [sharePreview, setSharePreview] = useState<deploy.LinkToSharedRootPreview | null>(null);
    const [shareLoading, setShareLoading] = useState(false);
    const [shareBusy, setShareBusy] = useState(false);
    const [shareError, setShareError] = useState('');

    const stagingNoteFor = useCallback((key: string) => (
        <SettingStagingNote {...getSettingStagingState(key, appliedSettings, stagedSettings, draftSettings)} />
    ), [appliedSettings, stagedSettings, draftSettings]);
    const [rootPath, setRootPath] = useState('');
    const [inputValue, setInputValue] = useState('');
    const [error, setError] = useState('');
    const [saving, setSaving] = useState(false);
    const [dockerfilePath, setDockerfilePath] = useState('');
    const [dockerfileInput, setDockerfileInput] = useState('');
    const [port, setPort] = useState('');
    const [portInput, setPortInput] = useState('');
    const [exposePorts, setExposePorts] = useState<dockerfile.ExposePort[]>([]);
    const [useDockerignore, setUseDockerignore] = useState(false);
    const [useGitignore, setUseGitignore] = useState(false);
    const [useBuildkitLocalContext, setUseBuildkitLocalContext] = useState(true);
    const [gitStream, setGitStream] = useState(true);

    const [settings, setSettings] = useState<Record<string, string>>({});
    const [volumes, setVolumes] = useState<VolumeEntry[]>([]);
    const [managedVolumes, setManagedVolumes] = useState<deploy.ManagedVolume[]>([]);
    const [labels, setLabels] = useState<LabelEntry[]>([]);

    const localManagedVolumeCount = useMemo(
        () => volumes.filter((v) => v.type === 'volume' && !!v.containerPath.trim()).length,
        [volumes],
    );

    const [template, setTemplate] = useState<store.ServiceTemplate | null>(null);
    const [imageInput, setImageInput] = useState('');
    // Explicit "custom" mode for the image picker. We can't derive this from
    // imageInput alone: when the current image happens to match a curated ref
    // (e.g. the template default), selecting Custom… would otherwise re-derive
    // the select value back to that curated ref and hide the free-text field.
    const [imageCustomMode, setImageCustomMode] = useState(false);

    const schema = useMemo<TemplateSchema>(() => parseSchema(template?.schema || ''), [template]);
    const hiddenSections = useMemo<Set<string>>(() => new Set(schema.hideSections || []), [schema]);
    // Image mode is determined by the template when available; fall back to the
    // settings (image set + no dockerfile) so blank nodes that someone pointed
    // at an image still get the image-mode UI.
    const isImageMode = template ? template.mode === 'image' : (!!settings.image && !settings.dockerfile);
    // Curated version options for image-mode templates with a tag list. When
    // present, the Image section renders a version dropdown plus a Custom…
    // free-text fallback; otherwise it stays a plain free-text input.
    const imageOptions = useMemo(() => {
        if (!template || template.mode !== 'image' || !template.image) return [];
        return buildImageOptions(template.image, template.imageTags);
    }, [template]);
    // The select shows Custom… when the user explicitly switched to custom mode
    // OR when the current image isn't one of the curated refs.
    const imageIsCurated = imageOptions.some((o) => o.ref === imageInput);
    const imageSelectValue = imageCustomMode || !imageIsCurated ? CUSTOM_IMAGE_VALUE : imageInput;
    const showImageCustomField = imageCustomMode || (!imageIsCurated && !!imageInput);
    const sectionHidden = (id: string) => hiddenSections.has(id);

    const [isGitRepo, setIsGitRepo] = useState(false);
    const [gitBranch, setGitBranch] = useState('');
    const [branches, setBranches] = useState<string[]>([]);
    const [branchesLoading, setBranchesLoading] = useState(false);
    const [branchError, setBranchError] = useState('');
    const [deployTrigger, setDeployTrigger] = useState<DeployTrigger>('manual');
    const [redeployOnPull, setRedeployOnPull] = useState(false);
    const [gitAutomationSaving, setGitAutomationSaving] = useState(false);
    const [hookStatus, setHookStatus] = useState<main.GitHookStatus | null>(null);

    const [showDeleteDialog, setShowDeleteDialog] = useState(false);
    const [deletePreview, setDeletePreview] = useState<DeletePreview | null>(null);
    const [deleteLoading, setDeleteLoading] = useState(false);
    const [deleteError, setDeleteError] = useState('');
    const [deleting, setDeleting] = useState(false);

    const saveSetting = useCallback((key: string, value: string) => {
        setSettings(prev => ({...prev, [key]: value}));
        updateDraftSetting(key, value);
    }, [updateDraftSetting]);

    const getSetting = (key: string) => settings[key] || '';

    const refreshBranches = useCallback(() => {
        setBranchesLoading(true);
        setBranchError('');
        ListGitBranches(targetNodeId, projectId)
            .then((list) => setBranches(list || []))
            .catch((e) => setBranchError(typeof e === 'string' ? e : e?.message || 'Failed to list branches'))
            .finally(() => setBranchesLoading(false));
    }, [targetNodeId, projectId]);

    const refreshHookStatus = useCallback(() => {
        GetGitHookStatus(targetNodeId, projectId).then(setHookStatus).catch(() => setHookStatus(null));
    }, [targetNodeId, projectId]);

    const refreshManagedVolumes = useCallback(() => {
        ListManagedVolumes(projectId, targetNodeId)
            .then((list) => setManagedVolumes(list ?? []))
            .catch(() => setManagedVolumes([]));
    }, [projectId, targetNodeId]);

    useEffect(() => {
        IsGitRepo(targetNodeId, projectId).then((ok) => {
            setIsGitRepo(ok);
            if (ok) {
                refreshBranches();
                refreshHookStatus();
            } else {
                setBranches([]);
                setHookStatus(null);
            }
        }).catch(() => setIsGitRepo(false));
    }, [targetNodeId, projectId, rootPath, refreshBranches, refreshHookStatus]);

    const commitGitBranch = useCallback((value: string) => {
        setGitBranch(value);
        setGitAutomationSaving(true);
        SetGitDeploymentConfig(nodeId, projectId, value, deployTrigger, value ? redeployOnPull : false).then(() => {
            refreshHookStatus();
            return reload();
        }).then(() => onServicesChanged?.()).catch((e) => {
            setError(typeof e === 'string' ? e : e?.message || 'Failed to update git deployment settings');
        }).finally(() => setGitAutomationSaving(false));
    }, [nodeId, projectId, deployTrigger, redeployOnPull, refreshHookStatus, reload, onServicesChanged]);

    const commitDeployTrigger = useCallback((value: DeployTrigger) => {
        setDeployTrigger(value);
        setGitAutomationSaving(true);
        SetGitDeploymentConfig(nodeId, projectId, gitBranch, value, redeployOnPull)
            .then(() => refreshHookStatus())
            .then(() => reload())
            .catch((e) => setError(typeof e === 'string' ? e : e?.message || 'Failed to update deploy trigger'))
            .finally(() => setGitAutomationSaving(false));
    }, [nodeId, projectId, gitBranch, redeployOnPull, refreshHookStatus, reload]);

    const commitRedeployOnPull = useCallback((value: boolean) => {
        setRedeployOnPull(value);
        setGitAutomationSaving(true);
        SetGitDeploymentConfig(nodeId, projectId, gitBranch, deployTrigger, value)
            .then(() => refreshHookStatus())
            .then(() => reload())
            .catch((e) => setError(typeof e === 'string' ? e : e?.message || 'Failed to update redeploy-on-pull'))
            .finally(() => setGitAutomationSaving(false));
    }, [nodeId, projectId, gitBranch, deployTrigger, refreshHookStatus, reload]);

    const applyGitStream = useCallback((next: boolean) => {
        setGitStream(next);
        SetNodeSetting(nodeId, 'git_stream', next ? 'true' : 'false')
            .then(() => reload())
            .catch(() => {});
    }, [nodeId, reload]);

    const applySettingsSnapshot = useCallback((s: Record<string, string>) => {
        setSettings(s);
        const df = s.dockerfile || '';
        setDockerfilePath(df);
        setDockerfileInput(df);
        const p = s.service_port || '';
        setPort(p);
        setPortInput(p);
        setImageInput(s.image || '');
        setImageCustomMode(false);
        setUseDockerignore(s.use_dockerignore === 'true');
        setUseGitignore(s.use_gitignore === 'true');
        setUseBuildkitLocalContext(s.use_buildkit_local_context !== 'false');
        if (s.volume_mounts) {
            setVolumes(parseVolumeEntries(s.volume_mounts));
        } else {
            setVolumes([]);
        }
        refreshManagedVolumes();
        if (s.custom_labels) {
            try {
                const obj = JSON.parse(s.custom_labels);
                setLabels(Object.entries(obj).map(([key, value]) => ({key, value: value as string})));
            } catch { setLabels([]); }
        } else {
            setLabels([]);
        }
        if (df) {
            ParseDockerfileExpose(df, projectId, s.service_root || '').then(setExposePorts).catch(() => setExposePorts([]));
        } else {
            setExposePorts([]);
        }
    }, [projectId, refreshManagedVolumes]);

    useEffect(() => {
        if (readOnly || configLoading || isSessionDirty) return;
        applySettingsSnapshot(committedSettings);
    }, [readOnly, committedSettings, configLoading, isSessionDirty, applySettingsSnapshot]);

    useEffect(() => {
        if (readOnly || configLoading) return;
        setGitBranch(appliedSettings.git_branch || '');
        setDeployTrigger((appliedSettings.deploy_trigger as DeployTrigger) || 'manual');
        setRedeployOnPull(appliedSettings.redeploy_on_pull === 'true');
        setGitStream(appliedSettings.git_stream !== 'false');
    }, [readOnly, appliedSettings, configLoading]);

    useEffect(() => {
        if (linkLoading || !readOnly) {
            setLinkedHasStagedChanges(false);
            return;
        }
        let cancelled = false;
        GetNodeConfigStatus(targetNodeId).then((status) => {
            if (cancelled) return;
            const applied = status?.appliedSettings || {};
            const staged = status?.stagedSettings || {};
            const effective = {...applied, ...staged};
            applySettingsSnapshot(effective);
            setGitBranch(effective.git_branch || '');
            setDeployTrigger((effective.deploy_trigger as DeployTrigger) || 'manual');
            setRedeployOnPull(effective.redeploy_on_pull === 'true');
            setGitStream(effective.git_stream !== 'false');
            setLinkedHasStagedChanges(!!status?.hasStagedChanges);
        }).catch(() => {
            if (!cancelled) setLinkedHasStagedChanges(false);
        });
        return () => {
            cancelled = true;
        };
    }, [linkLoading, readOnly, targetNodeId, applySettingsSnapshot]);

    useEffect(() => {
        GetServiceRoot(targetNodeId, projectId).then((path) => {
            setRootPath(path || '');
            setInputValue(path || '');
        });
        GetNode(targetNodeId)
            .then((node) => {
                if (node?.templateId) {
                    return GetServiceTemplate(node.templateId).then(setTemplate).catch(() => setTemplate(null));
                }
                setTemplate(null);
                return Promise.resolve();
            })
            .catch(() => setTemplate(null));
    }, [targetNodeId, projectId]);

    const matchedShareTargets = useMemo(
        () => shareTargets.filter((t) => !!t.matchedRoot?.nodeId),
        [shareTargets],
    );
    const selectedShareTarget = useMemo(
        () => shareTargets.find((t) => t.environmentId === shareEnvId) || null,
        [shareTargets, shareEnvId],
    );

    const openShareDialog = useCallback(async () => {
        setShareDialogOpen(true);
        setShareRootId('');
        setShareEnvId(0);
        setSharePreview(null);
        setShareError('');
        setShareVolumes('orphan');
        setShareLoading(true);
        try {
            const targets = await ListShareTargets(nodeId);
            const list = targets || [];
            setShareTargets(list);
            const matched = list.filter((t) => !!t.matchedRoot?.nodeId);
            const mode: ShareMode = matched.length > 0 ? 'same' : 'any';
            setShareMode(mode);
            if (mode === 'same' && matched[0]) {
                setShareEnvId(matched[0].environmentId);
                const rootId = matched[0].matchedRoot?.nodeId || '';
                setShareRootId(rootId);
                if (rootId) {
                    try {
                        setSharePreview(await PreviewLinkToSharedRoot(nodeId, rootId));
                    } catch (e: any) {
                        setShareError(typeof e === 'string' ? e : e?.message || 'Preview failed');
                    }
                }
            }
        } catch (e: any) {
            setShareTargets([]);
            setShareError(typeof e === 'string' ? e : e?.message || 'Failed to list share targets');
        } finally {
            setShareLoading(false);
        }
    }, [nodeId]);

    const selectShareRoot = useCallback(async (rootId: string) => {
        setShareRootId(rootId);
        setSharePreview(null);
        setShareError('');
        if (!rootId) return;
        try {
            setSharePreview(await PreviewLinkToSharedRoot(nodeId, rootId));
        } catch (e: any) {
            setShareError(typeof e === 'string' ? e : e?.message || 'Preview failed');
        }
    }, [nodeId]);

    const onShareModeChange = useCallback((mode: ShareMode) => {
        setShareMode(mode);
        setShareEnvId(0);
        setShareRootId('');
        setSharePreview(null);
        setShareError('');
        if (mode === 'same') {
            const first = shareTargets.find((t) => !!t.matchedRoot?.nodeId);
            if (first?.matchedRoot?.nodeId) {
                setShareEnvId(first.environmentId);
                void selectShareRoot(first.matchedRoot.nodeId);
            }
        }
    }, [shareTargets, selectShareRoot]);

    const onShareEnvChange = useCallback((envId: number) => {
        setShareEnvId(envId);
        setShareRootId('');
        setSharePreview(null);
        setShareError('');
        const target = shareTargets.find((t) => t.environmentId === envId);
        if (!target) return;
        if (shareMode === 'same' && target.matchedRoot?.nodeId) {
            void selectShareRoot(target.matchedRoot.nodeId);
        }
    }, [shareTargets, shareMode, selectShareRoot]);

    const confirmShare = useCallback(async () => {
        if (!shareRootId || shareBusy) return;
        setShareBusy(true);
        setShareError('');
        try {
            const disposition = localManagedVolumeCount > 0 ? shareVolumes : 'orphan';
            await LinkToSharedRoot(nodeId, shareRootId, disposition);
            setShareDialogOpen(false);
            await refreshLink();
            await reload();
            onServicesChanged?.();
            await alert({
                title: 'Now sharing',
                message: `This service now uses ${sharePreview?.rootLabel || 'the selected root'} from ${sharePreview?.rootEnvName || 'another environment'}.`,
            });
        } catch (e: any) {
            setShareError(typeof e === 'string' ? e : e?.message || 'Failed to share service');
        } finally {
            setShareBusy(false);
        }
    }, [shareRootId, shareBusy, localManagedVolumeCount, shareVolumes, nodeId, refreshLink, reload, onServicesChanged, alert, sharePreview]);

    const commitImage = useCallback((value?: string) => {
        const trimmed = (value ?? imageInput).trim();
        if (trimmed === (effectiveSettings.image || '')) return;
        setImageInput(trimmed);
        saveSetting('image', trimmed);
    }, [imageInput, effectiveSettings.image, saveSetting]);

    const saveRoot = useCallback(async (absolutePath: string) => {
        setError('');
        setSaving(true);
        try {
            await SetServiceRoot(nodeId, projectId, absolutePath);
            setRootPath(absolutePath);
            setInputValue(absolutePath);
            refreshBranches();
            refreshHookStatus();
            await reload();
            onServicesChanged?.();
        } catch (e: any) {
            const msg = typeof e === 'string' ? e : e?.message || 'Failed to set service root';
            setError(msg);
            setInputValue(rootPath);
        } finally {
            setSaving(false);
        }
    }, [nodeId, projectId, rootPath, refreshBranches, refreshHookStatus, reload, onServicesChanged]);

    const handleInputCommit = useCallback(() => {
        const trimmed = inputValue.trim();
        if (!trimmed) {
            setInputValue(rootPath);
            return;
        }
        if (trimmed === rootPath) return;

        const sep = projectPath.includes('\\') ? '\\' : '/';
        const isAbsolute = /^[A-Za-z]:[\\/]/.test(trimmed) || trimmed.startsWith('/');
        const absolutePath = isAbsolute ? trimmed : projectPath + sep + trimmed;

        saveRoot(absolutePath);
    }, [inputValue, rootPath, projectPath, saveRoot]);

    const handleBrowse = useCallback(async () => {
        setError('');
        try {
            const selected = await SelectServiceRoot(projectId);
            if (selected) {
                await saveRoot(selected);
            }
        } catch (e: any) {
            const msg = typeof e === 'string' ? e : e?.message || 'Failed to select folder';
            setError(msg);
        }
    }, [projectId, saveRoot]);

    const commitDockerfile = useCallback((value?: string) => {
        const trimmed = (value ?? dockerfileInput).trim();
        if (trimmed === dockerfilePath) return;
        setDockerfilePath(trimmed);
        setDockerfileInput(trimmed);
        saveSetting('dockerfile', trimmed);
        if (trimmed) {
            ParseDockerfileExpose(trimmed, projectId, settings.service_root || '').then(setExposePorts).catch(() => setExposePorts([]));
        } else {
            setExposePorts([]);
        }
    }, [projectId, dockerfileInput, dockerfilePath, saveSetting, settings.service_root]);

    const browseDockerfile = useCallback(async () => {
        const selected = await SelectFile('Select Dockerfile', projectPath);
        if (selected) {
            setDockerfileInput(selected);
            commitDockerfile(selected);
        }
    }, [projectPath, commitDockerfile]);

    const commitPort = useCallback((value?: string) => {
        const trimmed = (value ?? portInput).trim();
        if (trimmed === port) return;
        if (trimmed && (isNaN(Number(trimmed)) || Number(trimmed) < 1 || Number(trimmed) > 65535)) return;
        setPort(trimmed);
        setPortInput(trimmed);
        saveSetting('service_port', trimmed);
    }, [portInput, port, saveSetting]);

    const toggleDockerignore = useCallback(() => {
        const next = !useDockerignore;
        setUseDockerignore(next);
        saveSetting('use_dockerignore', next ? 'true' : 'false');
    }, [nodeId, useDockerignore, saveSetting]);

    const toggleGitignore = useCallback(() => {
        const next = !useGitignore;
        setUseGitignore(next);
        saveSetting('use_gitignore', next ? 'true' : 'false');
    }, [nodeId, useGitignore, saveSetting]);

    const toggleBuildkitLocalContext = useCallback(() => {
        const next = !useBuildkitLocalContext;
        setUseBuildkitLocalContext(next);
        saveSetting('use_buildkit_local_context', next ? 'true' : 'false');
    }, [nodeId, useBuildkitLocalContext, saveSetting]);

    const toggleGitStream = useCallback(() => {
        applyGitStream(!gitStream);
    }, [gitStream, applyGitStream]);

    const applyExposePort = useCallback((exposePort: number) => {
        const val = String(exposePort);
        setPortInput(val);
        setPort(val);
        saveSetting('service_port', val);
    }, [saveSetting]);

    const saveVolumes = useCallback((vols: VolumeEntry[]) => {
        setVolumes(vols);
        saveSetting('volume_mounts', serializeVolumeEntries(vols));
    }, [saveSetting]);

    const deleteDockerVolume = useCallback(async (name: string) => {
        if (!name) return;
        // force=false: Docker refuses if a container still uses it, which is the
        // safe default — the caller sees the error instead of yanking live data.
        try {
            await DeleteManagedVolume(name, false);
            refreshManagedVolumes();
        } catch {
            // Surface failures to the existing error line in the Source section.
            setError(`Failed to delete volume ${name}. It may still be in use — stop the service first.`);
        }
    }, [refreshManagedVolumes]);

    const managedByTarget = useMemo(() => {
        const m: Record<string, deploy.ManagedVolume> = {};
        for (const v of managedVolumes) {
            if (v.target) m[v.target] = v;
        }
        return m;
    }, [managedVolumes]);

    const saveLabels = useCallback((lbls: LabelEntry[]) => {
        setLabels(lbls);
        const obj: Record<string, string> = {};
        lbls.forEach(l => { if (l.key) obj[l.key] = l.value; });
        saveSetting('custom_labels', JSON.stringify(obj));
    }, [saveSetting]);

    const openDeleteDialog = useCallback(async () => {
        setDeleteError('');
        setDeleteLoading(true);
        setShowDeleteDialog(true);
        try {
            const preview = await PreviewDeleteService(nodeId);
            setDeletePreview({
                label: preview?.label ?? serviceLabel,
                isRunning: preview?.isRunning ?? false,
                managedVolumeCount: preview?.managedVolumeCount ?? 0,
                dependents: preview?.dependents ?? [],
                activeAliases: preview?.activeAliases ?? [],
            });
        } catch (e: unknown) {
            const msg = typeof e === 'string' ? e : (e as Error)?.message || 'Failed to load delete preview';
            setDeleteError(msg);
            setDeletePreview(null);
        } finally {
            setDeleteLoading(false);
        }
    }, [nodeId]);

    const confirmDelete = useCallback(async () => {
        setDeleteError('');
        setDeleting(true);
        try {
            await DeleteNode(nodeId);
            setShowDeleteDialog(false);
            onServiceDeleted?.();
            onServicesChanged?.();
        } catch (e: unknown) {
            const msg = typeof e === 'string' ? e : (e as Error)?.message || 'Failed to delete service';
            setDeleteError(msg);
        } finally {
            setDeleting(false);
        }
    }, [nodeId, onServiceDeleted, onServicesChanged]);

    const displayPath = rootPath
        ? (rootPath.toLowerCase().startsWith(projectPath.toLowerCase())
            ? '.' + rootPath.slice(projectPath.length)
            : rootPath)
        : '';

    const restartPolicy = getSetting('restart_policy') || 'no';

    // Volumes section, extracted so it can be placed high for image-mode nodes
    // (right after Image — datastores care about storage more than ports) and at
    // its usual lower spot for build-mode nodes.
    const volumesSection = !sectionHidden('volumes') && (
        <div className="settings-section">
            <h3 className="settings-section-title">Volumes</h3>
            <span className="settings-hint">
                Persistent storage for this service. Named volumes are Docker-managed — Draft mints a
                stable name from this service's identity so data survives redeploys. Bind mounts point
                at a host directory.
            </span>
            <VolumeEditor
                entries={volumes}
                onChange={saveVolumes}
                managedByTarget={managedByTarget}
                onDeleteVolume={deleteDockerVolume}
                projectId={projectId}
                onManagedVolumesChanged={refreshManagedVolumes}
            />
            {stagingNoteFor('volume_mounts')}
        </div>
    );
    const imageModeVolumes = isImageMode && volumesSection;
    const buildModeVolumes = !isImageMode && volumesSection;

    if (linkLoading) {
        return (
            <div className="settings-tab">
                <div className="settings-section">
                    <h3 className="settings-section-title">Settings</h3>
                    <span className="settings-hint">Loading shared root settings…</span>
                </div>
            </div>
        );
    }

    return (
        <div className="settings-tab">
            {(readOnly ? linkedHasStagedChanges : hasStagedChanges) && (
                <div className="settings-staged-banner">
                    {readOnly ? 'The root service has staged settings. This view includes them.' : 'Staged settings will apply on the next deploy.'}
                </div>
            )}
            {readOnly && (
                <div className="settings-section settings-locked-section">
                    <h3 className="settings-section-title">Sharing</h3>
                    <div className="settings-locked-callout">
                        <p className="settings-locked-title">This linked service is view-only here.</p>
                        <p className="settings-hint">
                            You are viewing the root service&apos;s settings from{' '}
                            <span className="settings-mono">{linkInfo?.rootEnvName || 'another environment'}</span>
                            {linkInfo?.rootLabel ? ` · ${linkInfo.rootLabel}` : ''}. Update the root service to change how {serviceLabel} runs here.
                            Use <strong>Promote</strong> or <strong>Unlink</strong> on Overview to stop sharing.
                        </p>
                        {!!linkInfo?.rootNodeId && !!linkInfo?.rootEnvironmentId && onOpenRootService && (
                            <button
                                className="btn btn-primary settings-locked-action"
                                onClick={() => onOpenRootService(projectId, linkInfo.rootNodeId || '', linkInfo.rootEnvironmentId || 0)}
                            >
                                <ArrowUpRight size={14} />
                                Go to root service
                            </button>
                        )}
                    </div>
                </div>
            )}
            {!readOnly && !linkLoading && (
                <div className="settings-section">
                    <h3 className="settings-section-title">Sharing</h3>
                    <span className="settings-hint">
                        Keep this service independent, or share a root service from another environment
                        (same idea as Share when duplicating an environment).
                    </span>
                    <div className="settings-sharing-row">
                        <div>
                            <strong className="settings-sharing-mode">Independent</strong>
                            <p className="settings-hint" style={{margin: '4px 0 0'}}>
                                This environment runs its own container
                                {localManagedVolumeCount > 0 ? ' and volumes' : ''}.
                            </p>
                        </div>
                        <button
                            type="button"
                            className="btn btn-ghost"
                            onClick={() => void openShareDialog()}
                            disabled={saving || shareBusy}
                        >
                            Share with…
                        </button>
                    </div>
                </div>
            )}
            <fieldset className="settings-fieldset" disabled={readOnly}>
            {/* ── Source ── */}
            {!sectionHidden('source') && (
            <div className="settings-section">
                <h3 className="settings-section-title">Source</h3>
                <p className="settings-immediate-hint">
                    Root directory applies immediately
                    {isGitRepo ? '; git branch, deploy triggers, and stream mode do too' : ''}
                    {' '}so hooks, source resolution, and file pickers stay in sync.
                </p>
                {isGitRepo && (
                    <div className="form-field">
                        <label className="form-label">
                            <GitBranch size={13} style={{verticalAlign: '-2px', marginRight: 4}} />
                            Git Branch
                        </label>
                        <span className="settings-hint">
                            Deploy from a specific committed branch instead of the files currently on disk.
                            The branch is exported into a temporary workspace at build time — your working
                            tree and any uncommitted changes are never touched. Leave as “Working tree” to
                            deploy exactly what is on disk (the default).
                        </span>
                        <div className="input-with-action">
                            <select
                                className="input settings-select"
                                value={gitBranch}
                                onChange={(e) => commitGitBranch(e.target.value)}
                            >
                                <option value="">Working tree (files on disk)</option>
                                {gitBranch && !branches.includes(gitBranch) && (
                                    <option value={gitBranch}>{gitBranch} (not found)</option>
                                )}
                                {branches.map((b) => (
                                    <option key={b} value={b}>{b}</option>
                                ))}
                            </select>
                            <button
                                className="btn btn-ghost input-action-btn"
                                onClick={refreshBranches}
                                disabled={branchesLoading}
                                title="Refresh branch list"
                            >
                                <RefreshCw size={14} className={branchesLoading ? 'spin' : ''} />
                            </button>
                        </div>
                        {branchError && <p className="form-error">{branchError}</p>}
                        {gitBranch && !branchError && (
                            <span className="settings-resolved">Deploying from branch “{gitBranch}”</span>
                        )}
                    </div>
                )}
                {isGitRepo && gitBranch && (
                    <div className="form-field">
                        <label className="form-label">Deploy Trigger</label>
                        <span className="settings-hint">
                            When to redeploy “{gitBranch}”. <strong>Manual</strong> deploys only when you click
                            Deploy. <strong>On commit</strong> redeploys whenever a commit lands on this branch.
                            <strong> On push</strong> redeploys only when this branch is pushed to its remote.
                            Automatic triggers install a local git hook — nothing is committed to your repository.
                        </span>
                        <div className="trigger-seg" role="group" aria-label="Deploy trigger">
                            {([
                                ['manual', 'Manual'],
                                ['on_commit', 'On commit'],
                                ['on_push', 'On push'],
                            ] as [DeployTrigger, string][]).map(([value, label]) => (
                                <button
                                    key={value}
                                    type="button"
                                    className={`trigger-seg-btn ${deployTrigger === value ? 'trigger-seg-btn--active' : ''}`}
                                    onClick={() => commitDeployTrigger(value)}
                                    disabled={gitAutomationSaving}
                                >
                                    {label}
                                </button>
                            ))}
                        </div>
                        {deployTrigger !== 'manual' && hookStatus &&
                            ((deployTrigger === 'on_commit' && hookStatus.commitForeign) ||
                             (deployTrigger === 'on_push' && hookStatus.pushForeign)) && (
                            <span className="settings-hint" style={{marginTop: 6}}>
                                An existing {deployTrigger === 'on_push' ? 'pre-push' : 'post-commit'} hook was found.
                                Draft chains to it, so your existing hook keeps running.
                            </span>
                        )}
                        <div style={{marginTop: 12}}>
                            <ToggleRow
                                label="Redeploy on pull"
                                desc="Also redeploy whenever a `git pull` (merge or rebase) or merge updates this branch. Installs local post-merge and post-rewrite git hooks — nothing is committed to your repo."
                                checked={redeployOnPull}
                                onToggle={() => commitRedeployOnPull(!redeployOnPull)}
                                inactive={gitAutomationSaving}
                            />
                        </div>
                        {redeployOnPull && hookStatus?.pullForeign && (
                            <span className="settings-hint" style={{marginTop: 6}}>
                                An existing post-merge or post-rewrite hook was found. Draft chains to it, so your existing hook keeps running.
                            </span>
                        )}
                    </div>
                )}
                <div className="form-field">
                    <label className="form-label">Root Directory</label>
                    <span className="settings-hint">
                        Path to the service source code, relative to the project root.
                        Must be inside <span className="settings-mono">{projectPath}</span>
                    </span>
                    <div className="input-with-action">
                        <input
                            className="input"
                            value={inputValue}
                            onChange={(e) => { setInputValue(e.target.value); setError(''); }}
                            onBlur={handleInputCommit}
                            onKeyDown={(e) => {
                                if (e.key === 'Enter') handleInputCommit();
                                if (e.key === 'Escape') setInputValue(rootPath);
                            }}
                            placeholder={projectPath}
                            disabled={saving}
                        />
                        <button className="btn btn-ghost input-action-btn" onClick={handleBrowse} disabled={saving} title="Browse for folder">
                            <FolderOpen size={14} />
                        </button>
                    </div>
                    {error && <p className="form-error">{error}</p>}
                    {displayPath && !error && <span className="settings-resolved">{displayPath}</span>}
                </div>
            </div>
            )}

            {/* ── Image (image-mode nodes) ── */}
            {isImageMode && (
                <div className="settings-section">
                    <h3 className="settings-section-title">Image</h3>
                    <div className="form-field">
                        <label className="form-label">Container Image</label>
                        <span className="settings-hint">
                            The registry image to pull and run. Pick a curated version or choose Custom to type any ref.
                        </span>
                        {imageOptions.length > 0 ? (
                            <>
                                <select
                                    className="input select-styled"
                                    value={imageSelectValue}
                                    onChange={(e) => {
                                        const v = e.target.value;
                                        if (v === CUSTOM_IMAGE_VALUE) {
                                            setImageCustomMode(true);
                                            // Seed the free-text field with the current ref so
                                            // the user can tweak it rather than starting blank.
                                            setImageInput(imageInput || settings.image || '');
                                        } else {
                                            setImageCustomMode(false);
                                            setImageInput(v);
                                            commitImage(v);
                                        }
                                    }}
                                >
                                    {imageOptions.map((opt) => (
                                        <option key={opt.ref} value={opt.ref}>{opt.label}</option>
                                    ))}
                                    <option value={CUSTOM_IMAGE_VALUE}>Custom…</option>
                                </select>
                                {showImageCustomField && (
                                    <input
                                        className="input"
                                        value={imageInput}
                                        onChange={(e) => setImageInput(e.target.value)}
                                        onBlur={() => commitImage()}
                                        onKeyDown={(e) => {
                                            if (e.key === 'Enter') commitImage();
                                            if (e.key === 'Escape') setImageInput(settings.image || '');
                                        }}
                                        placeholder="e.g. postgres:15-alpine"
                                    />
                                )}
                            </>
                        ) : (
                            <input
                                className="input"
                                value={imageInput}
                                onChange={(e) => setImageInput(e.target.value)}
                                onBlur={() => commitImage()}
                                onKeyDown={(e) => {
                                    if (e.key === 'Enter') commitImage();
                                    if (e.key === 'Escape') setImageInput(settings.image || '');
                                }}
                                placeholder="e.g. postgres:16-alpine"
                            />
                        )}
                    </div>
                    <div className="form-field">
                        <label className="form-label">Pull Policy</label>
                        <span className="settings-hint">
                            When to fetch the image. <strong>Missing</strong> uses the local image if present,
                            otherwise pulls — best for locally-built images. <strong>Always</strong> pulls on
                            every deploy. <strong>Never</strong> requires the image to already be local.
                        </span>
                        <select
                            className="input select-styled"
                            value={getSetting('pull_policy') || 'missing'}
                            onChange={(e) => saveSetting('pull_policy', e.target.value)}
                        >
                            <option value="missing">Missing (use local if present)</option>
                            <option value="always">Always (pull on every deploy)</option>
                            <option value="never">Never (local only)</option>
                        </select>
                        {stagingNoteFor('pull_policy')}
                    </div>
                    {stagingNoteFor('image')}
                </div>
            )}

            {/* ── Volumes (image-mode: high, right after Image) ── */}
            {imageModeVolumes}

            {/* ── Docker ── */}
            {!isImageMode && !sectionHidden('dockerfile') && (
            <div className="settings-section">
                <h3 className="settings-section-title">Docker</h3>
                <div className="form-field">
                    <label className="form-label">Dockerfile</label>
                    <span className="settings-hint">Path to the Dockerfile used to build this service.</span>
                    <div className="input-with-action">
                        <input
                            className="input"
                            value={dockerfileInput}
                            onChange={(e) => setDockerfileInput(e.target.value)}
                            onBlur={() => commitDockerfile()}
                            onKeyDown={(e) => {
                                if (e.key === 'Enter') commitDockerfile();
                                if (e.key === 'Escape') setDockerfileInput(dockerfilePath);
                            }}
                            placeholder="Dockerfile"
                        />
                        <button className="btn btn-ghost input-action-btn" onClick={browseDockerfile} title="Browse for Dockerfile">
                            <FileSearch size={14} />
                        </button>
                    </div>
                    {stagingNoteFor('dockerfile')}
                </div>
            </div>
            )}

            {/* ── Build Context ── */}
            {!isImageMode && !sectionHidden('buildContext') && (
            <div className="settings-section">
                <h3 className="settings-section-title">Build Context</h3>
                {gitBranch && (
                    <ToggleRow
                        label="Stream branch to Docker"
                        desc="Pipe the branch's committed files straight to Docker for a faster build. .dockerignore, .gitignore and BuildKit local context don't apply while streaming — commit only what you need to keep the context small. Turn off to build from a full checkout that respects your ignore files (slower). If the branch has git submodules, Draft includes them: stream splices from the local modules cache when those commits are already present, otherwise it checks out an ephemeral worktree and runs submodule update."
                        checked={gitStream}
                        onToggle={toggleGitStream}
                    />
                )}
                <ToggleRow settingKey="use_dockerignore" renderStagingNote={stagingNoteFor} label=".dockerignore" desc="Exclude files matched by .dockerignore patterns found in the service root." checked={useDockerignore} onToggle={toggleDockerignore} inactive={gitBranch !== '' && gitStream} inactiveNote="Not applied while streaming from a git branch." />
                <ToggleRow settingKey="use_gitignore" renderStagingNote={stagingNoteFor} label=".gitignore" desc="Exclude files matched by .gitignore patterns found anywhere in the service root." checked={useGitignore} onToggle={toggleGitignore} inactive={gitBranch !== '' && gitStream} inactiveNote="Not applied while streaming from a git branch." />
                <ToggleRow settingKey="use_buildkit_local_context" renderStagingNote={stagingNoteFor} label="BuildKit local context" desc="Faster on repeated deploys when only a small part of the service changes. Turn it off if you want Draft's legacy tar upload path for maximum compatibility." checked={useBuildkitLocalContext} onToggle={toggleBuildkitLocalContext} inactive={gitBranch !== '' && gitStream} inactiveNote="Not applied while streaming from a git branch." />
            </div>
            )}

            {/* ── Build Configuration ── */}
            {!isImageMode && !sectionHidden('buildConfiguration') && (
            <div className="settings-section">
                <h3 className="settings-section-title">Build Configuration</h3>
                <SettingInput renderStagingNote={stagingNoteFor} label="Target Stage" hint="For multi-stage builds, specify which stage to build (--target)." settingKey="build_target" value={getSetting('build_target')} onSave={saveSetting} placeholder="e.g. production" />
                <SettingInput renderStagingNote={stagingNoteFor} label="Platform" hint="Target platform for the build (e.g. linux/amd64, linux/arm64)." settingKey="build_platform" value={getSetting('build_platform')} onSave={saveSetting} placeholder="e.g. linux/amd64" />
                <ToggleRow settingKey="build_no_cache" renderStagingNote={stagingNoteFor} label="No Cache" desc="Force a full rebuild without using any cached layers." checked={getSetting('build_no_cache') === 'true'} onToggle={() => saveSetting('build_no_cache', getSetting('build_no_cache') === 'true' ? '' : 'true')} />
            </div>
            )}

            {/* ── Deployment Retention (build mode only) ── */}
            {!isImageMode && (
            <div className="settings-section">
                <h3 className="settings-section-title">Deployment Retention</h3>
                <div className="form-field">
                    <label className="form-label">Keep images</label>
                    <span className="settings-hint">
                        How many historical build images to keep on disk for rollback. Image-mode services are always re-pullable and ignore this.
                    </span>
                    <select
                        className="input settings-select"
                        value={getSetting('keep_images') || 'last'}
                        onChange={(e) => saveSetting('keep_images', e.target.value)}
                    >
                        <option value="last">Last (keep N-1 — one rollback, ~2× disk)</option>
                        <option value="none">None (minimal disk, no rollback)</option>
                        <option value="all">All (full rollback history, most disk)</option>
                    </select>
                    {stagingNoteFor('keep_images')}
                </div>
            </div>
            )}

            {/* ── Networking ── */}
            <div className="settings-section">
                <h3 className="settings-section-title">Networking</h3>
                <div className="form-field">
                    <label className="form-label">Port</label>
                    <span className="settings-hint">The port your service listens on inside the container. Required for deployment.</span>
                    <input
                        className="input"
                        type="number"
                        min={1}
                        max={65535}
                        value={portInput}
                        onChange={(e) => setPortInput(e.target.value)}
                        onBlur={() => commitPort()}
                        onKeyDown={(e) => {
                            if (e.key === 'Enter') commitPort();
                            if (e.key === 'Escape') setPortInput(port);
                        }}
                        placeholder="e.g. 3000"
                    />
                    {stagingNoteFor('service_port')}
                    {exposePorts.length > 0 && (
                        <div className="settings-expose">
                            <span className="settings-expose-label">Dockerfile EXPOSE:</span>
                            <div className="settings-expose-ports">
                                {exposePorts.map((ep) => {
                                    const label = ep.protocol === 'udp' ? `${ep.port}/udp` : String(ep.port);
                                    const isActive = port === String(ep.port);
                                    return (
                                        <button
                                            key={`${ep.port}/${ep.protocol}`}
                                            className={`settings-expose-port ${isActive ? 'settings-expose-port--active' : ''}`}
                                            onClick={() => applyExposePort(ep.port)}
                                            title={isActive ? 'Currently set' : `Use port ${ep.port}`}
                                        >
                                            {label}
                                        </button>
                                    );
                                })}
                            </div>
                            <span className="settings-expose-note">EXPOSE is documentation only — click a port to use it, or enter your own above.</span>
                        </div>
                    )}
                    {port && exposePorts.length > 0 && !exposePorts.some(ep => String(ep.port) === port) && (
                        <span className="settings-expose-warning">
                            Port {port} differs from Dockerfile EXPOSE ({exposePorts.map(ep => ep.port).join(', ')}). This is fine if intentional.
                        </span>
                    )}
                </div>
                <div className="form-field">
                    <label className="form-label">Route protocol</label>
                    <span className="settings-hint">
                        HTTP services are fronted by Draft&apos;s proxy on an ephemeral host port. TCP services (e.g. databases) bind a stable host port directly so clients can reach <code>localhost:&lt;port&gt;</code>.
                    </span>
                    <select
                        className="input settings-select"
                        value={getSetting('route_protocol') || 'http'}
                        onChange={(e) => saveSetting('route_protocol', e.target.value)}
                    >
                        <option value="http">HTTP (proxied)</option>
                        <option value="tcp">TCP (stable host port)</option>
                    </select>
                    {stagingNoteFor('route_protocol')}
                </div>
                {(getSetting('route_protocol') || 'http') === 'tcp' && (
                    <div className="form-field">
                        <label className="form-label">Host port</label>
                        <span className="settings-hint">Preferred host port to bind (e.g. 5432). Leave 0/empty for auto-assign. Draft reuses the same port across redeploys when it&apos;s free.</span>
                        <input
                            className="input"
                            type="number"
                            min={0}
                            max={65535}
                            value={getSetting('host_port') || ''}
                            onChange={(e) => saveSetting('host_port', e.target.value)}
                            placeholder="0 (auto)"
                        />
                        {stagingNoteFor('host_port')}
                    </div>
                )}
            </div>

            {/* ── Runtime Command ── */}
            {!sectionHidden('runtimeCommand') && (
            <div className="settings-section">
                <h3 className="settings-section-title">Runtime Command</h3>
                <SettingInput renderStagingNote={stagingNoteFor} label="Command" hint="Override the Dockerfile CMD. Supports shell syntax (e.g. node server.js --port 3000)." settingKey="cmd_override" value={getSetting('cmd_override')} onSave={saveSetting} placeholder='e.g. node server.js' />
                <SettingInput renderStagingNote={stagingNoteFor} label="Entrypoint" hint="Override the Dockerfile ENTRYPOINT." settingKey="entrypoint_override" value={getSetting('entrypoint_override')} onSave={saveSetting} placeholder='e.g. /usr/bin/tini --' />
                <SettingInput renderStagingNote={stagingNoteFor} label="Working Directory" hint="Override the container working directory (WORKDIR)." settingKey="working_dir" value={getSetting('working_dir')} onSave={saveSetting} placeholder="e.g. /app" />
                <SettingInput renderStagingNote={stagingNoteFor} label="User" hint="Run the container as this user/UID (e.g. node, 1000, 1000:1000)." settingKey="run_user" value={getSetting('run_user')} onSave={saveSetting} placeholder="e.g. node" />
            </div>
            )}

            {/* ── Restart Policy ── */}
            {!sectionHidden('restart') && (
            <div className="settings-section">
                <h3 className="settings-section-title">Restart Policy</h3>
                <div className="form-field">
                    <label className="form-label">Policy</label>
                    <span className="settings-hint">How Docker should restart the container if it exits.</span>
                    <select
                        className="input settings-select"
                        value={restartPolicy}
                        onChange={(e) => saveSetting('restart_policy', e.target.value)}
                    >
                        <option value="no">No (default)</option>
                        <option value="always">Always</option>
                        <option value="on-failure">On Failure</option>
                        <option value="unless-stopped">Unless Stopped</option>
                    </select>
                </div>
                {restartPolicy === 'on-failure' && (
                    <SettingInput renderStagingNote={stagingNoteFor} label="Max Retries" hint="Maximum number of restart attempts before giving up." settingKey="restart_max_retries" value={getSetting('restart_max_retries')} onSave={saveSetting} placeholder="e.g. 5" type="number" />
                )}
            </div>
            )}

            {/* ── Health Check ── */}
            {!sectionHidden('healthcheck') && (
            <div className="settings-section">
                <h3 className="settings-section-title">Health Check</h3>
                <ToggleRow label="Disable Health Check" desc="Ignore any HEALTHCHECK instruction in the Dockerfile." checked={getSetting('healthcheck_disable') === 'true'} onToggle={() => saveSetting('healthcheck_disable', getSetting('healthcheck_disable') === 'true' ? '' : 'true')} />
                {getSetting('healthcheck_disable') !== 'true' && (<>
                    <SettingInput renderStagingNote={stagingNoteFor} label="Command" hint="Health check command (e.g. curl -f http://localhost:3000/health)." settingKey="healthcheck_cmd" value={getSetting('healthcheck_cmd')} onSave={saveSetting} placeholder="e.g. curl -f http://localhost:3000/health" />
                    <SettingInput renderStagingNote={stagingNoteFor} label="Interval" hint="Time between checks (Go duration, e.g. 30s, 1m)." settingKey="healthcheck_interval" value={getSetting('healthcheck_interval')} onSave={saveSetting} placeholder="e.g. 30s" />
                    <SettingInput renderStagingNote={stagingNoteFor} label="Timeout" hint="Max time for a single check." settingKey="healthcheck_timeout" value={getSetting('healthcheck_timeout')} onSave={saveSetting} placeholder="e.g. 10s" />
                    <SettingInput renderStagingNote={stagingNoteFor} label="Start Period" hint="Grace period before the first health check." settingKey="healthcheck_start_period" value={getSetting('healthcheck_start_period')} onSave={saveSetting} placeholder="e.g. 5s" />
                    <SettingInput renderStagingNote={stagingNoteFor} label="Retries" hint="Number of consecutive failures before marking as unhealthy." settingKey="healthcheck_retries" value={getSetting('healthcheck_retries')} onSave={saveSetting} placeholder="e.g. 3" type="number" />
                </>)}
            </div>
            )}

            {/* ── Resource Limits ── */}
            {!sectionHidden('resources') && (
            <div className="settings-section">
                <h3 className="settings-section-title">Resource Limits</h3>
                <SettingInput renderStagingNote={stagingNoteFor} label="CPU Limit" hint="Maximum CPU cores (e.g. 1.5 = 1.5 cores, 0.5 = half a core)." settingKey="cpu_limit" value={getSetting('cpu_limit')} onSave={saveSetting} placeholder="e.g. 1.5" />
                <SettingInput renderStagingNote={stagingNoteFor} label="Memory Limit" hint="Maximum memory (e.g. 512m, 1g, 256mb)." settingKey="memory_limit" value={getSetting('memory_limit')} onSave={saveSetting} placeholder="e.g. 512m" />
                <SettingInput renderStagingNote={stagingNoteFor} label="Memory Reservation" hint="Soft memory limit — Docker will try to keep usage below this." settingKey="memory_reservation" value={getSetting('memory_reservation')} onSave={saveSetting} placeholder="e.g. 256m" />
                <SettingInput renderStagingNote={stagingNoteFor} label="PID Limit" hint="Maximum number of processes in the container." settingKey="pids_limit" value={getSetting('pids_limit')} onSave={saveSetting} placeholder="e.g. 100" type="number" />
            </div>
            )}

            {/* ── Volumes (build-mode position) ── */}
            {buildModeVolumes}

            {/* ── Lifecycle Hooks ── */}
            {!sectionHidden('lifecycle') && (
            <div className="settings-section">
                <h3 className="settings-section-title">Lifecycle Hooks</h3>
                <SettingInput renderStagingNote={stagingNoteFor} label="Pre-Build" hint="Shell command to run on your machine before building the image. If you're streaming from a git branch (the “Stream branch to Docker” option), any files this command generates on disk won't be included — the build context comes straight from git. Turn that option off to build from a full checkout that picks them up." settingKey="pre_build_cmd" value={getSetting('pre_build_cmd')} onSave={saveSetting} placeholder="e.g. npm run generate" />
                <SettingInput renderStagingNote={stagingNoteFor} label="Post-Build" hint="Shell command to run on your machine after a successful build." settingKey="post_build_cmd" value={getSetting('post_build_cmd')} onSave={saveSetting} placeholder="e.g. echo Build complete" />
                <SettingInput renderStagingNote={stagingNoteFor} label="Pre-Deploy" hint="Shell command to run on your machine before starting the container." settingKey="pre_deploy_cmd" value={getSetting('pre_deploy_cmd')} onSave={saveSetting} placeholder="e.g. ./scripts/migrate.sh" />
                <SettingInput renderStagingNote={stagingNoteFor} label="Post-Deploy" hint="Shell command to run on your machine after the container is running." settingKey="post_deploy_cmd" value={getSetting('post_deploy_cmd')} onSave={saveSetting} placeholder="e.g. curl http://localhost:3000/warmup" />
                <div className="form-field">
                    <label className="form-label">Stop Signal</label>
                    <span className="settings-hint">Signal sent to the container when stopping (default: SIGTERM).</span>
                    <select
                        className="input settings-select"
                        value={getSetting('stop_signal') || ''}
                        onChange={(e) => saveSetting('stop_signal', e.target.value)}
                    >
                        <option value="">Default (SIGTERM)</option>
                        <option value="SIGTERM">SIGTERM</option>
                        <option value="SIGINT">SIGINT</option>
                        <option value="SIGQUIT">SIGQUIT</option>
                        <option value="SIGKILL">SIGKILL</option>
                    </select>
                </div>
                <SettingInput renderStagingNote={stagingNoteFor} label="Stop Grace Period" hint="Seconds to wait after stop signal before force-killing." settingKey="stop_grace_period" value={getSetting('stop_grace_period')} onSave={saveSetting} placeholder="e.g. 10" type="number" />
            </div>
            )}

            {/* ── Security ── */}
            {!sectionHidden('security') && (
            <div className="settings-section">
                <h3 className="settings-section-title">Security</h3>
                <ToggleRow label="Privileged" desc="Run the container with full host privileges. Use with caution." checked={getSetting('privileged') === 'true'} onToggle={() => saveSetting('privileged', getSetting('privileged') === 'true' ? '' : 'true')} />
                <ToggleRow label="Init Process" desc="Run an init process (tini) as PID 1 to handle signal forwarding and zombie reaping." checked={getSetting('init_process') === 'true'} onToggle={() => saveSetting('init_process', getSetting('init_process') === 'true' ? '' : 'true')} />
                <ToggleRow label="Read-Only Root Filesystem" desc="Mount the container root filesystem as read-only." checked={getSetting('readonly_rootfs') === 'true'} onToggle={() => saveSetting('readonly_rootfs', getSetting('readonly_rootfs') === 'true' ? '' : 'true')} />
                <SettingInput renderStagingNote={stagingNoteFor} label="Add Capabilities" hint="Comma-separated Linux capabilities to add (e.g. SYS_PTRACE, NET_ADMIN)." settingKey="cap_add" value={getSetting('cap_add')} onSave={saveSetting} placeholder="e.g. SYS_PTRACE, NET_ADMIN" />
                <SettingInput renderStagingNote={stagingNoteFor} label="Drop Capabilities" hint="Comma-separated Linux capabilities to drop." settingKey="cap_drop" value={getSetting('cap_drop')} onSave={saveSetting} placeholder="e.g. NET_RAW, MKNOD" />
            </div>
            )}

            {/* ── Custom Labels ── */}
            {!sectionHidden('labels') && (
            <div className="settings-section">
                <h3 className="settings-section-title">Custom Labels</h3>
                <span className="settings-hint">Key-value labels applied to the container. Draft labels are added automatically.</span>
                {labels.map((lbl, i) => (
                    <div key={i} className="settings-kv-row">
                        <input
                            className="input settings-kv-input"
                            value={lbl.key}
                            onChange={(e) => {
                                const updated = [...labels];
                                updated[i] = {...updated[i], key: e.target.value};
                                setLabels(updated);
                            }}
                            onBlur={() => saveLabels(labels)}
                            placeholder="Label key"
                        />
                        <input
                            className="input settings-kv-input"
                            value={lbl.value}
                            onChange={(e) => {
                                const updated = [...labels];
                                updated[i] = {...updated[i], value: e.target.value};
                                setLabels(updated);
                            }}
                            onBlur={() => saveLabels(labels)}
                            placeholder="Label value"
                        />
                        <button className="btn btn-ghost settings-kv-remove" onClick={() => saveLabels(labels.filter((_, j) => j !== i))} title="Remove">
                            <Trash2 size={12} />
                        </button>
                    </div>
                ))}
                <button className="btn btn-ghost settings-add-btn" onClick={() => setLabels([...labels, {key: '', value: ''}])}>
                    <Plus size={12} /> Add Label
                </button>
            </div>
            )}

            </fieldset>
            {!readOnly && (
            <div className="settings-section settings-section--danger">
                <h3 className="settings-section-title">Delete Service</h3>
                <p className="settings-hint">
                    Permanently remove <strong>{serviceLabel}</strong> from this project. Draft-managed Docker volumes
                    are kept so data can be recovered or cleaned up later; bind mounts on disk are not deleted.
                </p>
                <button className="btn btn-danger settings-delete-btn" onClick={openDeleteDialog}>
                    <Trash2 size={14} /> Delete service…
                </button>
            </div>
            )}

            {!readOnly && showDeleteDialog && (
                <Dialog
                    title={`Delete “${serviceLabel}”?`}
                    onClose={() => !deleting && setShowDeleteDialog(false)}
                    footer={
                        <div className="settings-delete-dialog-footer">
                            <button className="btn btn-ghost" onClick={() => setShowDeleteDialog(false)} disabled={deleting}>
                                Cancel
                            </button>
                            <button
                                className="btn btn-danger"
                                onClick={confirmDelete}
                                disabled={deleting || deleteLoading || (deletePreview?.activeAliases?.length ?? 0) > 0}
                            >
                                {deleting ? 'Deleting…' : 'Delete service'}
                            </button>
                        </div>
                    }
                >
                    {deleteLoading && (
                        <div className="skel-stack" style={{gap: 10}} aria-busy="true" aria-label="Loading delete preview">
                            <Skeleton width="90%" height={12} />
                            <Skeleton width="75%" height={12} />
                            <Skeleton width="60%" height={12} />
                        </div>
                    )}
                    {!deleteLoading && deletePreview && (
                        <div className="settings-delete-dialog">
                            <p className="settings-delete-lead">
                                This removes the service from the canvas, stops any running container, and deletes its
                                settings, variables, and deployment history. This cannot be undone.
                            </p>
                            {deletePreview.isRunning && (
                                <p className="settings-delete-warning">
                                    A container for this service is currently running and will be stopped.
                                </p>
                            )}
                            {deletePreview.managedVolumeCount > 0 && (
                                <p className="settings-delete-note">
                                    {deletePreview.managedVolumeCount} Draft-managed Docker volume
                                    {deletePreview.managedVolumeCount === 1 ? '' : 's'} will be kept (orphaned) so you can
                                    delete them separately if needed.
                                </p>
                            )}
                            {(deletePreview.activeAliases ?? []).length > 0 && (
                                <div className="settings-delete-dependents">
                                    <p className="settings-delete-warning">
                                        This is a shared root. Unlink or promote these aliases first:
                                    </p>
                                    <ul className="settings-delete-dependent-list">
                                        {(deletePreview.activeAliases ?? []).map((label) => (
                                            <li key={label}>
                                                <span className="settings-delete-dependent-service">{label}</span>
                                            </li>
                                        ))}
                                    </ul>
                                </div>
                            )}
                            {(deletePreview.dependents ?? []).length > 0 && (
                                <div className="settings-delete-dependents">
                                    <p className="settings-delete-warning">
                                        Other services still reference this one. Their variable values will <strong>not</strong> be
                                        changed, but deploy and preview will break until you update them:
                                    </p>
                                    <ul className="settings-delete-dependent-list">
                                        {(deletePreview.dependents ?? []).map((dep) => (
                                            <li key={`${dep.sourceNodeId}:${dep.varKey}:${dep.token}`}>
                                                <span className="settings-delete-dependent-service">
                                                    {dep.sourceLabel}.{dep.varKey}
                                                </span>
                                                <span className="settings-delete-dependent-token">{dep.token}</span>
                                            </li>
                                        ))}
                                    </ul>
                                </div>
                            )}
                        </div>
                    )}
                    {deleteError && <p className="form-error">{deleteError}</p>}
                </Dialog>
            )}

            {shareDialogOpen && (
                <Dialog
                    title="Share with another environment"
                    onClose={() => !shareBusy && setShareDialogOpen(false)}
                    footer={
                        <>
                            <button className="btn btn-ghost" onClick={() => setShareDialogOpen(false)} disabled={shareBusy}>
                                Cancel
                            </button>
                            <button
                                className="btn btn-primary"
                                onClick={() => void confirmShare()}
                                disabled={shareBusy || !shareRootId || shareLoading}
                            >
                                {shareBusy ? 'Sharing…' : 'Share'}
                            </button>
                        </>
                    }
                >
                    <div className="dialog-copy">
                        <p className="dialog-message">
                            Stop the local container for <strong>{serviceLabel}</strong> and attach a root from
                            another environment. Prefer <strong>Same service</strong> when a counterpart exists;
                            use <strong>Any service</strong> to pick a different root.
                        </p>
                        {shareLoading ? (
                            <p className="settings-hint">Loading services…</p>
                        ) : shareTargets.length === 0 ? (
                            <p className="settings-hint">
                                No other environments have a root service to share yet. Duplicate an environment
                                with independent services first, or create the service in another environment.
                            </p>
                        ) : (
                            <>
                                <div className="form-field">
                                    <label className="form-label">Share mode</label>
                                    <div className="environment-data-segment" role="radiogroup" aria-label="Share mode">
                                        <label className={`environment-data-segment-option${shareMode === 'same' ? ' is-active' : ''}`}>
                                            <input
                                                type="radio"
                                                name="share-mode"
                                                checked={shareMode === 'same'}
                                                disabled={shareBusy || matchedShareTargets.length === 0}
                                                onChange={() => onShareModeChange('same')}
                                            />
                                            Same service
                                        </label>
                                        <label className={`environment-data-segment-option${shareMode === 'any' ? ' is-active' : ''}`}>
                                            <input
                                                type="radio"
                                                name="share-mode"
                                                checked={shareMode === 'any'}
                                                disabled={shareBusy}
                                                onChange={() => onShareModeChange('any')}
                                            />
                                            Any service
                                        </label>
                                    </div>
                                    {shareMode === 'same' && matchedShareTargets.length === 0 && (
                                        <span className="settings-hint">
                                            No clear counterpart found in other environments. Switch to Any service
                                            to pick manually.
                                        </span>
                                    )}
                                </div>

                                {shareMode === 'same' ? (
                                    <div className="form-field">
                                        <label className="form-label" htmlFor="share-env-same">Environment</label>
                                        <select
                                            id="share-env-same"
                                            className="input settings-select"
                                            value={shareEnvId || ''}
                                            disabled={shareBusy || matchedShareTargets.length === 0}
                                            onChange={(e) => onShareEnvChange(Number(e.target.value) || 0)}
                                        >
                                            <option value="">Select environment…</option>
                                            {matchedShareTargets.map((t) => (
                                                <option key={t.environmentId} value={t.environmentId}>
                                                    {t.envName} · {t.matchedRoot?.label}
                                                    {t.matchedRoot?.matchReason
                                                        ? ` (${matchReasonLabel(t.matchedRoot.matchReason)})`
                                                        : ''}
                                                </option>
                                            ))}
                                        </select>
                                        {selectedShareTarget?.matchedRoot && (
                                            <span className="settings-hint">
                                                Will share <strong>{selectedShareTarget.matchedRoot.label}</strong> in{' '}
                                                {selectedShareTarget.envName}.
                                            </span>
                                        )}
                                    </div>
                                ) : (
                                    <>
                                        <div className="form-field">
                                            <label className="form-label" htmlFor="share-env-any">Environment</label>
                                            <select
                                                id="share-env-any"
                                                className="input settings-select"
                                                value={shareEnvId || ''}
                                                disabled={shareBusy}
                                                onChange={(e) => onShareEnvChange(Number(e.target.value) || 0)}
                                            >
                                                <option value="">Select environment…</option>
                                                {shareTargets.map((t) => (
                                                    <option key={t.environmentId} value={t.environmentId}>
                                                        {t.envName} ({t.roots?.length || 0} services)
                                                    </option>
                                                ))}
                                            </select>
                                        </div>
                                        <div className="form-field">
                                            <label className="form-label" htmlFor="share-root-any">Service</label>
                                            <select
                                                id="share-root-any"
                                                className="input settings-select"
                                                value={shareRootId}
                                                disabled={shareBusy || !shareEnvId}
                                                onChange={(e) => void selectShareRoot(e.target.value)}
                                            >
                                                <option value="">Select service…</option>
                                                {(selectedShareTarget?.roots || []).map((r) => (
                                                    <option key={r.nodeId} value={r.nodeId}>
                                                        {r.label}
                                                        {r.matchReason ? ` (${matchReasonLabel(r.matchReason)})` : ''}
                                                    </option>
                                                ))}
                                            </select>
                                        </div>
                                    </>
                                )}

                                {sharePreview?.warning && (
                                    <p className="settings-delete-warning">{sharePreview.warning}</p>
                                )}
                                {sharePreview?.isRunning && (
                                    <p className="settings-hint">
                                        The local container for this service is running and will be stopped.
                                    </p>
                                )}
                                {localManagedVolumeCount > 0 && (
                                    <div className="form-field">
                                        <label className="form-label">Local volumes</label>
                                        <span className="settings-hint">
                                            This service has {localManagedVolumeCount} managed volume
                                            {localManagedVolumeCount === 1 ? '' : 's'}. Linked aliases do not keep
                                            local mounts.
                                        </span>
                                        <select
                                            className="input settings-select"
                                            value={shareVolumes}
                                            disabled={shareBusy}
                                            onChange={(e) => setShareVolumes(e.target.value as VolumeDisposition)}
                                        >
                                            <option value="orphan">Keep as orphans (recommended)</option>
                                            <option value="delete">Delete volumes</option>
                                        </select>
                                    </div>
                                )}
                            </>
                        )}
                        {shareError && <p className="form-error">{shareError}</p>}
                    </div>
                </Dialog>
            )}
        </div>
    );
}

function ToggleRow({label, desc, checked, onToggle, inactive, inactiveNote, settingKey, renderStagingNote}: {
    label: string;
    desc: string;
    checked: boolean;
    onToggle: () => void;
    inactive?: boolean;
    inactiveNote?: string;
    settingKey?: string;
    renderStagingNote?: (key: string) => ReactNode;
}) {
    return (
        <div className={`settings-toggle-row${inactive ? ' settings-toggle-row--inactive' : ''}`}>
            <div className="settings-toggle-label">
                <span className="settings-toggle-name">{label}</span>
                <span className="settings-toggle-desc">
                    {desc}
                    {inactive && inactiveNote && <em className="settings-toggle-inactive-note"> {inactiveNote}</em>}
                </span>
                {settingKey && renderStagingNote?.(settingKey)}
            </div>
            <button
                className={`toggle-switch${checked ? ' toggle-switch--on' : ''}`}
                onClick={onToggle}
                role="switch"
                aria-checked={checked}
                disabled={inactive}
            />
        </div>
    );
}

function SettingInput({label, hint, settingKey, value, onSave, placeholder, type, renderStagingNote}: {
    label: string;
    hint: string;
    settingKey: string;
    value: string;
    onSave: (key: string, value: string) => void;
    placeholder?: string;
    type?: string;
    renderStagingNote?: (key: string) => ReactNode;
}) {
    const [local, setLocal] = useState(value);

    useEffect(() => { setLocal(value); }, [value]);

    const commit = () => {
        const trimmed = local.trim();
        if (trimmed !== value) {
            onSave(settingKey, trimmed);
        }
    };

    return (
        <div className="form-field">
            <label className="form-label">{label}</label>
            <span className="settings-hint">{hint}</span>
            <input
                className="input"
                type={type || 'text'}
                value={local}
                onChange={(e) => setLocal(e.target.value)}
                onBlur={commit}
                onKeyDown={(e) => {
                    if (e.key === 'Enter') commit();
                    if (e.key === 'Escape') setLocal(value);
                }}
                placeholder={placeholder}
            />
            {renderStagingNote?.(settingKey)}
        </div>
    );
}
