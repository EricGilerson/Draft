import type {networking, store} from '../../wailsjs/go/models';

/** Rewrite *.draft.local → *.{suffix} for display. */
export function hostnameWithSuffix(hostname: string, suffix: string): string {
    if (!hostname || !suffix) return '';
    return hostname.replace(/\.draft\.local\.?$/, `.${suffix}`);
}

/**
 * Preferred UI public endpoint from cached LocalDomainStatus.
 * Mirrors networking.BestServiceURL: localhost mode → 127.0.0.1,
 * else public suffix (.draft when verified, else resolv.sh), else loopback.
 */
export function bestPublicEndpoint(
    hostname: string | undefined,
    hostPort: number,
    localDomain: networking.LocalDomainStatus | null,
    protocol: string = 'http',
): string {
    const isTCP = protocol === 'tcp';
    const loopback = hostPort > 0
        ? (isTCP ? `127.0.0.1:${hostPort}` : `http://127.0.0.1:${hostPort}`)
        : '';

    if (!localDomain || localDomain.mode === 'localhost-port' || !localDomain.proxyPort) {
        return loopback;
    }

    const suffix = localDomain.publicSuffix || localDomain.loopbackSuffix || '';
    const publicHostname = hostnameWithSuffix(hostname || '', suffix);
    if (!publicHostname) return loopback;

    if (isTCP) {
        return hostPort > 0 ? `${publicHostname}:${hostPort}` : '';
    }
    if (localDomain.proxyPort === 80) {
        return `http://${publicHostname}`;
    }
    return `http://${publicHostname}:${localDomain.proxyPort}`;
}

export function bestPublicDeploymentURL(
    deployment: store.Deployment | null,
    localDomain: networking.LocalDomainStatus | null,
    protocol: string = 'http',
): string {
    if (!deployment) return '';
    return bestPublicEndpoint(deployment.hostname, deployment.hostPort || 0, localDomain, protocol);
}
