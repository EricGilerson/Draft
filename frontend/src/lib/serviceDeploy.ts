/** Whether a service can start a real deploy from these (effective) settings. */
export type ServiceDeployability = {
    canDeploy: boolean;
    /** Empty when canDeploy; otherwise a short reason for disabled Deploy. */
    reason: string;
};

/**
 * Build-mode needs Dockerfile + port; image-mode needs image + port.
 * Linked aliases skip this — they only re-attach the root.
 */
export function serviceDeployability(
    settings: Record<string, string> | null | undefined,
    opts?: {isLinked?: boolean},
): ServiceDeployability {
    if (opts?.isLinked) {
        return {canDeploy: true, reason: ''};
    }
    const port = (settings?.service_port || '').trim();
    const dockerfile = (settings?.dockerfile || '').trim();
    const image = (settings?.image || '').trim();
    if (port && (dockerfile || image)) {
        return {canDeploy: true, reason: ''};
    }
    if (!port && !dockerfile && !image) {
        return {canDeploy: false, reason: 'Set an image or Dockerfile and a port in Settings first'};
    }
    if (!port) {
        return {canDeploy: false, reason: 'Set a container port in Settings first'};
    }
    return {canDeploy: false, reason: 'Set an image or Dockerfile in Settings first'};
}
