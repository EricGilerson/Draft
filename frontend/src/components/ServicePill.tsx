import {Cpu, Database, Globe, Zap} from 'lucide-react';
import {SERVICE_COLORS, STATUS_COLORS, ServicePreview, ServiceType} from '../lib/dashboardData';
import './ServicePill.css';

const ICONS: Record<ServiceType, typeof Globe> = {
    web: Globe,
    database: Database,
    cache: Zap,
    worker: Cpu,
};

type ServicePillProps = {
    service: ServicePreview;
};

export default function ServicePill({service}: ServicePillProps) {
    const Icon = ICONS[service.type];
    const color = SERVICE_COLORS[service.type];

    return (
        <div
            className="service-pill"
            style={{
                borderColor: `${color}28`,
                background: `${color}10`,
            }}
        >
            <Icon size={11} className="service-pill-icon" style={{color}}/>
            <span className="service-pill-name" style={{color: `${color}dd`}}>{service.name}</span>
            {service.port && <span className="service-pill-port">:{service.port}</span>}
            <span
                className="service-pill-status"
                style={{background: STATUS_COLORS[service.status]}}
                aria-hidden="true"
            />
        </div>
    );
}

