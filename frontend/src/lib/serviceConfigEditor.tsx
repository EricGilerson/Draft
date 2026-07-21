import {
    createContext,
    useCallback,
    useContext,
    useEffect,
    useMemo,
    useRef,
    useState,
    type ReactNode,
} from 'react';
import {
    DeployService,
    DiscardStagedChanges,
    DiscardStagedChangesPartial,
    GetEnvVars,
    GetNodeConfigStatus,
    PreviewStagedChanges,
    StageEnvVarChanges,
    StageNodeSettings,
} from '../../wailsjs/go/main/App';
import {EventsOn} from '../../wailsjs/runtime/runtime';
import {deploy, store} from '../../wailsjs/go/models';
import {
    committedEnvByKey,
    envDraftHasChanges,
    normalizeEnvDraft,
    type EnvDraftState,
    type EnvDraftUpsert,
} from './envStaging';
import {isImmediateSetting, settingsValuesEqual} from './settingStaging';

export type {EnvDraftUpsert, EnvDraftState};

function mergeSettings(...maps: Array<Record<string, string>>): Record<string, string> {
    return Object.assign({}, ...maps);
}

type ServiceConfigEditorContextValue = {
    nodeId: string;
    projectId: number;
    loading: boolean;
    staging: boolean;
    appliedSettings: Record<string, string>;
    stagedSettings: Record<string, string>;
    stagedEnvChanges: deploy.StagedEnvVarChange[];
    appliedEnvVars: store.EnvVar[];
    hasStagedChanges: boolean;
    activeDeploymentStatus: string;
    draftSettings: Record<string, string>;
    envDraft: EnvDraftState;
    committedSettings: Record<string, string>;
    effectiveSettings: Record<string, string>;
    isSessionDirty: boolean;
    updateDraftSetting: (key: string, value: string) => void;
    updateDraftSettings: (patch: Record<string, string>) => void;
    discardSessionDraft: () => void;
    /** Drop specific unsaved setting keys and/or env draft keys. */
    discardSessionDraftPartial: (settingKeys: string[], envKeys: string[]) => void;
    setEnvDraftUpsert: (upsert: EnvDraftUpsert) => void;
    setEnvDraftUpserts: (upserts: EnvDraftUpsert[]) => void;
    setEnvDraftDelete: (key: string, remove: boolean) => void;
    /** Drop session-draft upserts/deletes for the given keys (e.g. after staging them). */
    omitEnvDraftKeys: (keys: string[]) => void;
    clearEnvDraft: () => void;
    reload: () => Promise<void>;
    previewStage: () => Promise<deploy.StagedChangePreview>;
    stageChanges: () => Promise<void>;
    discardStaged: () => Promise<void>;
    /** Discard only the named staged setting/env keys (backend). */
    discardStagedPartial: (settingKeys: string[], envKeys: string[]) => Promise<void>;
    stageAndDeploy: () => Promise<void>;
};

const ServiceConfigEditorContext = createContext<ServiceConfigEditorContextValue | null>(null);

const emptyEnvDraft = (): EnvDraftState => ({upserts: {}, deleteKeys: []});

export function ServiceConfigEditorProvider({
    nodeId,
    projectId,
    children,
}: {
    nodeId: string;
    projectId: number;
    children: ReactNode;
}) {
    const [loading, setLoading] = useState(true);
    const [staging, setStaging] = useState(false);
    const [appliedSettings, setAppliedSettings] = useState<Record<string, string>>({});
    const [stagedSettings, setStagedSettings] = useState<Record<string, string>>({});
    const [stagedEnvChanges, setStagedEnvChanges] = useState<deploy.StagedEnvVarChange[]>([]);
    const [hasStagedChanges, setHasStagedChanges] = useState(false);
    const [activeDeploymentStatus, setActiveDeploymentStatus] = useState('');
    const [draftSettings, setDraftSettings] = useState<Record<string, string>>({});
    const [appliedEnvVars, setAppliedEnvVars] = useState<store.EnvVar[]>([]);
    const [envDraft, setEnvDraft] = useState<EnvDraftState>(emptyEnvDraft);
    const envDraftRef = useRef(envDraft);
    const committedEnvRef = useRef(committedEnvByKey([], []));
    const currentNodeIdRef = useRef(nodeId);

    envDraftRef.current = envDraft;
    currentNodeIdRef.current = nodeId;

    const reload = useCallback(async () => {
        const requestedNodeId = nodeId;
        if (currentNodeIdRef.current === requestedNodeId) {
            setLoading(true);
        }
        try {
            const [status, envVars] = await Promise.all([
                GetNodeConfigStatus(requestedNodeId),
                GetEnvVars(requestedNodeId),
            ]);
            // A reload started for the previously selected service may finish
            // after the panel is showing another one. Never let that response
            // replace the current service's editor state.
            if (currentNodeIdRef.current !== requestedNodeId) {
                return;
            }
            setAppliedSettings(status?.appliedSettings || {});
            setStagedSettings(status?.stagedSettings || {});
            setStagedEnvChanges(status?.stagedEnvChanges || []);
            setHasStagedChanges(!!status?.hasStagedChanges);
            setActiveDeploymentStatus(status?.activeDeploymentStatus || '');
            setAppliedEnvVars(envVars || []);
        } finally {
            if (currentNodeIdRef.current === requestedNodeId) {
                setLoading(false);
            }
        }
    }, [nodeId]);

    useEffect(() => {
        setDraftSettings({});
        setEnvDraft(emptyEnvDraft());
        void reload();
    }, [nodeId, reload]);

    // Successful deploy promotes staged settings/env into applied rows; refresh
    // once the deploy finishes so banners and per-field staging notes clear.
    useEffect(() => {
        const unsubscribe = EventsOn('deploy:status', (payload: any) => {
            if (payload?.nodeId !== nodeId) {
                return;
            }
            const status: string = payload?.event?.status || '';
            if (status === 'running' || status === 'failed' || status === 'stopped') {
                void reload();
            }
        });
        return unsubscribe;
    }, [nodeId, reload]);

    const committedSettings = useMemo(
        () => mergeSettings(appliedSettings, stagedSettings),
        [appliedSettings, stagedSettings],
    );

    const effectiveSettings = useMemo(
        () => mergeSettings(committedSettings, draftSettings),
        [committedSettings, draftSettings],
    );

    const stageableDraftSettings = useMemo(
        () => Object.fromEntries(
            Object.entries(draftSettings).filter(([key]) => !isImmediateSetting(key)),
        ),
        [draftSettings],
    );

    const committedEnv = useMemo(
        () => committedEnvByKey(appliedEnvVars, stagedEnvChanges),
        [appliedEnvVars, stagedEnvChanges],
    );

    committedEnvRef.current = committedEnv;

    const isSessionDirty = useMemo(
        () => Object.keys(stageableDraftSettings).length > 0
            || envDraftHasChanges(envDraft, committedEnv),
        [stageableDraftSettings, envDraft, committedEnv],
    );

    const updateDraftSetting = useCallback((key: string, value: string) => {
        if (isImmediateSetting(key)) {
            return;
        }
        setDraftSettings((prev) => {
            const committed = mergeSettings(appliedSettings, stagedSettings);
            if (settingsValuesEqual(committed[key] || '', value)) {
                if (!(key in prev)) return prev;
                const next = {...prev};
                delete next[key];
                return next;
            }
            return {...prev, [key]: value};
        });
    }, [appliedSettings, stagedSettings]);

    const updateDraftSettings = useCallback((patch: Record<string, string>) => {
        for (const [key, value] of Object.entries(patch)) {
            updateDraftSetting(key, value);
        }
    }, [updateDraftSetting]);

    const discardSessionDraft = useCallback(() => {
        setDraftSettings({});
        setEnvDraft(emptyEnvDraft());
    }, []);

    const discardSessionDraftPartial = useCallback((settingKeys: string[], envKeys: string[]) => {
        if (settingKeys.length > 0) {
            const drop = new Set(settingKeys);
            setDraftSettings((prev) => {
                const next = {...prev};
                for (const k of drop) delete next[k];
                return next;
            });
        }
        if (envKeys.length > 0) {
            const drop = new Set(envKeys);
            setEnvDraft((prev) => ({
                upserts: Object.fromEntries(
                    Object.entries(prev.upserts).filter(([k]) => !drop.has(k)),
                ),
                deleteKeys: prev.deleteKeys.filter((k) => !drop.has(k)),
            }));
        }
    }, []);

    const setEnvDraftUpsert = useCallback((upsert: EnvDraftUpsert) => {
        setEnvDraft((prev) => ({
            upserts: {...prev.upserts, [upsert.key]: upsert},
            deleteKeys: prev.deleteKeys.filter((k) => k !== upsert.key),
        }));
    }, []);

    const setEnvDraftUpserts = useCallback((upserts: EnvDraftUpsert[]) => {
        if (upserts.length === 0) return;
        setEnvDraft((prev) => {
            const nextUpserts = {...prev.upserts};
            const drop = new Set(upserts.map((u) => u.key));
            for (const u of upserts) {
                nextUpserts[u.key] = u;
            }
            return {
                upserts: nextUpserts,
                deleteKeys: prev.deleteKeys.filter((k) => !drop.has(k)),
            };
        });
    }, []);

    const setEnvDraftDelete = useCallback((key: string, remove: boolean) => {
        setEnvDraft((prev) => {
            if (!remove) {
                return {
                    upserts: Object.fromEntries(
                        Object.entries(prev.upserts).filter(([k]) => k !== key),
                    ),
                    deleteKeys: prev.deleteKeys.filter((k) => k !== key),
                };
            }
            const nextUpserts = {...prev.upserts};
            delete nextUpserts[key];
            const nextDeletes = prev.deleteKeys.includes(key)
                ? prev.deleteKeys
                : [...prev.deleteKeys, key];
            return {upserts: nextUpserts, deleteKeys: nextDeletes};
        });
    }, []);

    const omitEnvDraftKeys = useCallback((keys: string[]) => {
        if (keys.length === 0) return;
        const drop = new Set(keys);
        setEnvDraft((prev) => ({
            upserts: Object.fromEntries(
                Object.entries(prev.upserts).filter(([k]) => !drop.has(k)),
            ),
            deleteKeys: prev.deleteKeys.filter((k) => !drop.has(k)),
        }));
    }, []);

    const clearEnvDraft = useCallback(() => {
        setEnvDraft(emptyEnvDraft());
    }, []);

    const previewStage = useCallback(async () => {
        if (Object.keys(stageableDraftSettings).length === 0) {
            return deploy.StagedChangePreview.createFrom({warnings: [], errors: []});
        }
        return PreviewStagedChanges(nodeId, stageableDraftSettings);
    }, [nodeId, stageableDraftSettings]);

    const stageChanges = useCallback(async () => {
        setStaging(true);
        try {
            if (Object.keys(stageableDraftSettings).length > 0) {
                await StageNodeSettings(nodeId, projectId, stageableDraftSettings);
            }
            const normalizedEnvDraft = normalizeEnvDraft(envDraftRef.current, committedEnvRef.current);
            const upserts = Object.values(normalizedEnvDraft.upserts).map((u) =>
                store.EnvVarStageUpsert.createFrom({
                    key: u.key,
                    value: u.value,
                    scope: u.scope || 'runtime',
                }),
            );
            if (upserts.length > 0 || normalizedEnvDraft.deleteKeys.length > 0) {
                await StageEnvVarChanges(nodeId, upserts, normalizedEnvDraft.deleteKeys);
            }
            setDraftSettings({});
            await reload();
            setEnvDraft(emptyEnvDraft());
        } finally {
            setStaging(false);
        }
    }, [nodeId, projectId, stageableDraftSettings, reload]);

    const discardStaged = useCallback(async () => {
        setStaging(true);
        try {
            await DiscardStagedChanges(nodeId);
            await reload();
        } finally {
            setStaging(false);
        }
    }, [nodeId, reload]);

    const discardStagedPartial = useCallback(async (settingKeys: string[], envKeys: string[]) => {
        if (settingKeys.length === 0 && envKeys.length === 0) return;
        setStaging(true);
        try {
            await DiscardStagedChangesPartial(nodeId, settingKeys, envKeys);
            await reload();
        } finally {
            setStaging(false);
        }
    }, [nodeId, reload]);

    const stageAndDeploy = useCallback(async () => {
        await stageChanges();
        await DeployService(nodeId);
    }, [stageChanges, nodeId]);

    const value = useMemo<ServiceConfigEditorContextValue>(() => ({
        nodeId,
        projectId,
        loading,
        staging,
        appliedSettings,
        stagedSettings,
        stagedEnvChanges,
        appliedEnvVars,
        hasStagedChanges,
        activeDeploymentStatus,
        draftSettings,
        envDraft,
        committedSettings,
        effectiveSettings,
        isSessionDirty,
        updateDraftSetting,
        updateDraftSettings,
        discardSessionDraft,
        discardSessionDraftPartial,
        setEnvDraftUpsert,
        setEnvDraftUpserts,
        setEnvDraftDelete,
        omitEnvDraftKeys,
        clearEnvDraft,
        reload,
        previewStage,
        stageChanges,
        discardStaged,
        discardStagedPartial,
        stageAndDeploy,
    }), [
        nodeId,
        projectId,
        loading,
        staging,
        appliedSettings,
        stagedSettings,
        stagedEnvChanges,
        appliedEnvVars,
        hasStagedChanges,
        activeDeploymentStatus,
        draftSettings,
        envDraft,
        committedSettings,
        effectiveSettings,
        isSessionDirty,
        updateDraftSetting,
        updateDraftSettings,
        discardSessionDraft,
        discardSessionDraftPartial,
        setEnvDraftUpsert,
        setEnvDraftUpserts,
        setEnvDraftDelete,
        omitEnvDraftKeys,
        clearEnvDraft,
        reload,
        previewStage,
        stageChanges,
        discardStaged,
        discardStagedPartial,
        stageAndDeploy,
    ]);

    return (
        <ServiceConfigEditorContext.Provider value={value}>
            {children}
        </ServiceConfigEditorContext.Provider>
    );
}

export function useServiceConfigEditor(): ServiceConfigEditorContextValue {
    const ctx = useContext(ServiceConfigEditorContext);
    if (!ctx) {
        throw new Error('useServiceConfigEditor must be used within ServiceConfigEditorProvider');
    }
    return ctx;
}

export function useOptionalServiceConfigEditor(): ServiceConfigEditorContextValue | null {
    return useContext(ServiceConfigEditorContext);
}
