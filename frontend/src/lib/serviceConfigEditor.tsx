import {
    createContext,
    useCallback,
    useContext,
    useEffect,
    useMemo,
    useState,
    type ReactNode,
} from 'react';
import {
    DeployService,
    DiscardStagedChanges,
    GetNodeConfigStatus,
    PreviewStagedChanges,
    StageEnvVarChanges,
    StageNodeSettings,
} from '../../wailsjs/go/main/App';
import {deploy, store} from '../../wailsjs/go/models';

export type EnvDraftUpsert = {
    key: string;
    value: string;
    scope: string;
};

export type EnvDraftState = {
    upserts: Record<string, EnvDraftUpsert>;
    deleteKeys: string[];
};

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
    setEnvDraftUpsert: (upsert: EnvDraftUpsert) => void;
    setEnvDraftDelete: (key: string, remove: boolean) => void;
    clearEnvDraft: () => void;
    reload: () => Promise<void>;
    previewStage: () => Promise<deploy.StagedChangePreview>;
    stageChanges: () => Promise<void>;
    discardStaged: () => Promise<void>;
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
    const [envDraft, setEnvDraft] = useState<EnvDraftState>(emptyEnvDraft);

    const reload = useCallback(async () => {
        setLoading(true);
        try {
            const status = await GetNodeConfigStatus(nodeId);
            setAppliedSettings(status?.appliedSettings || {});
            setStagedSettings(status?.stagedSettings || {});
            setStagedEnvChanges(status?.stagedEnvChanges || []);
            setHasStagedChanges(!!status?.hasStagedChanges);
            setActiveDeploymentStatus(status?.activeDeploymentStatus || '');
        } finally {
            setLoading(false);
        }
    }, [nodeId]);

    useEffect(() => {
        setDraftSettings({});
        setEnvDraft(emptyEnvDraft());
        void reload();
    }, [nodeId, reload]);

    const committedSettings = useMemo(
        () => mergeSettings(appliedSettings, stagedSettings),
        [appliedSettings, stagedSettings],
    );

    const effectiveSettings = useMemo(
        () => mergeSettings(committedSettings, draftSettings),
        [committedSettings, draftSettings],
    );

    const isSessionDirty = useMemo(
        () => Object.keys(draftSettings).length > 0
            || Object.keys(envDraft.upserts).length > 0
            || envDraft.deleteKeys.length > 0,
        [draftSettings, envDraft],
    );

    const updateDraftSetting = useCallback((key: string, value: string) => {
        setDraftSettings((prev) => {
            const committed = mergeSettings(appliedSettings, stagedSettings);
            if ((committed[key] || '') === value) {
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

    const setEnvDraftUpsert = useCallback((upsert: EnvDraftUpsert) => {
        setEnvDraft((prev) => {
            const nextDeletes = prev.deleteKeys.filter((k) => k !== upsert.key);
            return {
                upserts: {...prev.upserts, [upsert.key]: upsert},
                deleteKeys: nextDeletes,
            };
        });
    }, []);

    const setEnvDraftDelete = useCallback((key: string, remove: boolean) => {
        setEnvDraft((prev) => {
            if (!remove) {
                const nextDeletes = prev.deleteKeys.filter((k) => k !== key);
                const nextUpserts = {...prev.upserts};
                delete nextUpserts[key];
                return {upserts: nextUpserts, deleteKeys: nextDeletes};
            }
            const nextUpserts = {...prev.upserts};
            delete nextUpserts[key];
            const nextDeletes = prev.deleteKeys.includes(key)
                ? prev.deleteKeys
                : [...prev.deleteKeys, key];
            return {upserts: nextUpserts, deleteKeys: nextDeletes};
        });
    }, []);

    const clearEnvDraft = useCallback(() => {
        setEnvDraft(emptyEnvDraft());
    }, []);

    const previewStage = useCallback(async () => {
        const proposed = mergeSettings(stagedSettings, draftSettings);
        return PreviewStagedChanges(nodeId, proposed);
    }, [nodeId, stagedSettings, draftSettings]);

    const stageChanges = useCallback(async () => {
        setStaging(true);
        try {
            if (Object.keys(draftSettings).length > 0) {
                await StageNodeSettings(nodeId, projectId, draftSettings);
            }
            const upserts = Object.values(envDraft.upserts).map((u) =>
                store.EnvVarStageUpsert.createFrom({
                    key: u.key,
                    value: u.value,
                    scope: u.scope || 'runtime',
                }),
            );
            if (upserts.length > 0 || envDraft.deleteKeys.length > 0) {
                await StageEnvVarChanges(nodeId, upserts, envDraft.deleteKeys);
            }
            setDraftSettings({});
            setEnvDraft(emptyEnvDraft());
            await reload();
        } finally {
            setStaging(false);
        }
    }, [nodeId, projectId, draftSettings, envDraft, reload]);

    const discardStaged = useCallback(async () => {
        setStaging(true);
        try {
            await DiscardStagedChanges(nodeId);
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
        setEnvDraftUpsert,
        setEnvDraftDelete,
        clearEnvDraft,
        reload,
        previewStage,
        stageChanges,
        discardStaged,
        stageAndDeploy,
    }), [
        nodeId,
        projectId,
        loading,
        staging,
        appliedSettings,
        stagedSettings,
        stagedEnvChanges,
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
        setEnvDraftUpsert,
        setEnvDraftDelete,
        clearEnvDraft,
        reload,
        previewStage,
        stageChanges,
        discardStaged,
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
