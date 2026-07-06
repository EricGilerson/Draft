export const IMMEDIATE_SETTING_KEYS = new Set([
    'git_branch',
    'deploy_trigger',
    'redeploy_on_pull',
    'git_stream',
    'service_root',
    'env_file',
]);

export function isImmediateSetting(key: string): boolean {
    return IMMEDIATE_SETTING_KEYS.has(key);
}

export type SettingStagingState = {
    key: string;
    applied: string;
    staged?: string;
    draft?: string;
    isStaged: boolean;
    isDraft: boolean;
};

export function getSettingStagingState(
    key: string,
    appliedSettings: Record<string, string>,
    stagedSettings: Record<string, string>,
    draftSettings: Record<string, string>,
): SettingStagingState {
    const applied = appliedSettings[key] ?? '';
    const staged = key in stagedSettings ? stagedSettings[key] : undefined;
    const draft = key in draftSettings ? draftSettings[key] : undefined;
    const isStaged = staged !== undefined && staged !== applied;
    const isDraft = draft !== undefined && draft !== (staged ?? applied);
    return {key, applied, staged, draft, isStaged, isDraft};
}

export function formatSettingValue(key: string, value: string): string {
    if (!value) return '(empty)';
    switch (key) {
        case 'deploy_trigger':
            if (value === 'on_commit') return 'On commit';
            if (value === 'on_push') return 'On push';
            return 'Manual';
        case 'redeploy_on_pull':
        case 'use_dockerignore':
        case 'use_gitignore':
        case 'use_buildkit_local_context':
        case 'git_stream':
        case 'build_no_cache':
        case 'healthcheck_disable':
        case 'privileged':
        case 'init_process':
        case 'readonly_rootfs':
            return value === 'true' ? 'On' : 'Off';
        case 'volume_mounts': {
            try {
                const arr = JSON.parse(value);
                if (Array.isArray(arr)) {
                    return arr.length === 1 ? '1 mount' : `${arr.length} mounts`;
                }
            } catch {
                // fall through
            }
            return 'Custom mounts';
        }
        case 'custom_labels': {
            try {
                const obj = JSON.parse(value);
                if (obj && typeof obj === 'object') {
                    const n = Object.keys(obj).length;
                    return n === 1 ? '1 label' : `${n} labels`;
                }
            } catch {
                // fall through
            }
            return value;
        }
        default:
            return value;
    }
}
