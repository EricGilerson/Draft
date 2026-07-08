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

export type CommittedEnvVar = {
    value: string;
    scope: string;
};

export function committedEnvByKey(
    applied: store.EnvVar[],
    stagedChanges: deploy.StagedEnvVarChange[],
): Record<string, CommittedEnvVar> {
    const byKey: Record<string, CommittedEnvVar> = {};
    for (const v of applied) {
        byKey[v.key] = {
            value: v.value,
            scope: v.scope || 'runtime',
        };
    }
    for (const ch of stagedChanges) {
        if (ch.delete) {
            delete byKey[ch.key];
            continue;
        }
        byKey[ch.key] = {
            value: ch.value,
            scope: ch.scope || byKey[ch.key]?.scope || 'runtime',
        };
    }
    return byKey;
}

export function envDraftUpsertIsNoOp(
    committed: Record<string, CommittedEnvVar>,
    upsert: EnvDraftUpsert,
): boolean {
    const existing = committed[upsert.key];
    if (!existing) {
        return false;
    }
    const scope = upsert.scope || 'runtime';
    return existing.value === upsert.value && existing.scope === scope;
}

export function envDraftDeleteIsNoOp(
    committed: Record<string, CommittedEnvVar>,
    key: string,
): boolean {
    return !(key in committed);
}

export function normalizeEnvDraft(
    draft: EnvDraftState,
    committed: Record<string, CommittedEnvVar>,
): EnvDraftState {
    const upserts: Record<string, EnvDraftUpsert> = {};
    for (const upsert of Object.values(draft.upserts)) {
        if (!envDraftUpsertIsNoOp(committed, upsert)) {
            upserts[upsert.key] = upsert;
        }
    }
    const deleteKeys = draft.deleteKeys.filter((key) => !envDraftDeleteIsNoOp(committed, key));
    return {upserts, deleteKeys};
}

export function envDraftHasChanges(
    draft: EnvDraftState,
    committed: Record<string, CommittedEnvVar>,
): boolean {
    const normalized = normalizeEnvDraft(draft, committed);
    return Object.keys(normalized.upserts).length > 0 || normalized.deleteKeys.length > 0;
}
