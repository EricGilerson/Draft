import type {CSSProperties} from 'react';
import {SERVICE_COLORS, STATUS_COLORS, ServicePreview} from '../lib/dashboardData';
import './ServicePill.css';

type ServicePillProps = {
    service: ServicePreview;
};

export default function ServicePill({service}: ServicePillProps) {
    const color = SERVICE_COLORS[service.type];

    return (
        <div
            className={`service-pill service-pill-${service.type}`}
            style={{
                '--service-color': color,
                '--service-status': STATUS_COLORS[service.status],
            } as CSSProperties}
        >
            <span className="service-pill-status" aria-hidden="true"/>
            <span className="service-pill-name">{service.name}</span>
            {service.port && (
                <span className="service-pill-port" aria-label={`Port ${service.port}`}>
                    <span className="service-pill-port-prefix">:</span>{service.port}
                </span>
            )}
        </div>
    );
}
