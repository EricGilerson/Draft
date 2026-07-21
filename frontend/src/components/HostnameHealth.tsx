import {useEffect, useState} from 'react';
import {GetLocalDomainStatus} from '../../wailsjs/go/main/App';
import {networking} from '../../wailsjs/go/models';
import './HostnameHealth.css';

type HealthTone = 'ok' | 'warn' | 'error' | 'checking';

function summarize(status: networking.LocalDomainStatus | null): {tone: HealthTone; label: string; detail: string} {
    if (!status) {
        return {tone: 'checking', label: 'Hostnames…', detail: 'Checking local domain status'};
    }
    if (status.hostsError) {
        return {
            tone: 'error',
            label: 'Hosts issue',
            detail: status.hostsError,
        };
    }
    if (status.dnsError) {
        return {
            tone: 'warn',
            label: 'DNS issue',
            detail: status.dnsError,
        };
    }
    if (status.draftEnabled && !status.dnsVerified) {
        return {
            tone: 'warn',
            label: 'DNS unverified',
            detail: status.dnsAddr
                ? `Local *.draft resolver at ${status.dnsAddr} is not verified yet`
                : 'Local *.draft DNS is enabled but not verified',
        };
    }
    const mode = status.mode === 'localhost-port' ? 'localhost' : 'public hostnames';
    const port = status.proxyPort > 0 ? ` · :${status.proxyPort}` : '';
    return {
        tone: 'ok',
        label: 'Hostnames OK',
        detail: `${mode}${port}${status.draftEnabled ? ' · *.draft' : ''}`,
    };
}

/** Compact topbar chip for hosts / local DNS / proxy health. */
export default function HostnameHealth() {
    const [status, setStatus] = useState<networking.LocalDomainStatus | null>(null);

    useEffect(() => {
        let cancelled = false;
        const load = () => {
            GetLocalDomainStatus()
                .then((s) => {
                    if (!cancelled) setStatus(s);
                })
                .catch(() => {
                    if (!cancelled) setStatus(null);
                });
        };
        load();
        const id = window.setInterval(load, 60_000);
        const onFocus = () => load();
        const onVisibility = () => {
            if (document.visibilityState === 'visible') load();
        };
        window.addEventListener('focus', onFocus);
        document.addEventListener('visibilitychange', onVisibility);
        return () => {
            cancelled = true;
            window.clearInterval(id);
            window.removeEventListener('focus', onFocus);
            document.removeEventListener('visibilitychange', onVisibility);
        };
    }, []);

    const {tone, label, detail} = summarize(status);

    return (
        <div className={`hostname-health hostname-health--${tone}`} title={detail} role="status">
            <span className="hostname-health-dot" aria-hidden />
            <span className="hostname-health-label">{label}</span>
        </div>
    );
}
