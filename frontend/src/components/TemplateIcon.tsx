import type React from 'react';
import {Box, Mail, MemoryStick} from 'lucide-react';
import {
    SiNextdotjs, SiNodedotjs, SiFastapi, SiFlask, SiVite, SiReact,
    SiPostgresql, SiRedis, SiMysql, SiMongodb, SiMariadb, SiSqlite,
    SiDocker, SiPython, SiTypescript, SiRust, SiGo, SiNginx,
    SiDjango, SiExpress, SiGunicorn, SiPnpm, SiYarn, SiBun, SiHtml5,
    SiRabbitmq, SiElixir, SiMinio, SiMeilisearch, SiClickhouse, SiAdminer,
    SiRubyonrails, SiDotnet, SiSpringboot, SiDeno,
} from '@icons-pack/react-simple-icons';

// Shared shape for both simple-icons brand components and the lucide
// fallbacks used where no brand icon exists (e.g. Mailpit, Memcached).
type IconComponent = React.ComponentType<{size?: number | string; color?: string; className?: string}>;

// A curated subset of simple-icons brand slugs (plus a few lucide fallbacks)
// exposed in the template editor's icon picker. Each entry maps the stored
// slug (the value persisted on a ServiceTemplate) to the React component and
// a human label. Unknown slugs stored on a template fall back to a generic
// Box icon at render time.
const ICON_MAP: Record<string, {label: string; Comp: IconComponent}> = {
    nextdotjs: {label: 'Next.js', Comp: SiNextdotjs},
    nodedotjs: {label: 'Node.js', Comp: SiNodedotjs},
    fastapi: {label: 'FastAPI', Comp: SiFastapi},
    flask: {label: 'Flask', Comp: SiFlask},
    vite: {label: 'Vite', Comp: SiVite},
    react: {label: 'React', Comp: SiReact},
    postgresql: {label: 'PostgreSQL', Comp: SiPostgresql},
    redis: {label: 'Redis', Comp: SiRedis},
    mysql: {label: 'MySQL', Comp: SiMysql},
    mongodb: {label: 'MongoDB', Comp: SiMongodb},
    mariadb: {label: 'MariaDB', Comp: SiMariadb},
    sqlite: {label: 'SQLite', Comp: SiSqlite},
    rabbitmq: {label: 'RabbitMQ', Comp: SiRabbitmq},
    nginx: {label: 'nginx', Comp: SiNginx},
    docker: {label: 'Docker', Comp: SiDocker},
    python: {label: 'Python', Comp: SiPython},
    django: {label: 'Django', Comp: SiDjango},
    gunicorn: {label: 'Gunicorn', Comp: SiGunicorn},
    express: {label: 'Express', Comp: SiExpress},
    typescript: {label: 'TypeScript', Comp: SiTypescript},
    rust: {label: 'Rust', Comp: SiRust},
    go: {label: 'Go', Comp: SiGo},
    elixir: {label: 'Elixir', Comp: SiElixir},
    pnpm: {label: 'pnpm', Comp: SiPnpm},
    yarn: {label: 'Yarn', Comp: SiYarn},
    bun: {label: 'Bun', Comp: SiBun},
    html5: {label: 'HTML5', Comp: SiHtml5},
    minio: {label: 'MinIO', Comp: SiMinio},
    meilisearch: {label: 'Meilisearch', Comp: SiMeilisearch},
    clickhouse: {label: 'ClickHouse', Comp: SiClickhouse},
    adminer: {label: 'Adminer', Comp: SiAdminer},
    // No simple-icons brand mark exists for these; lucide generics stand in.
    mailpit: {label: 'Mailpit', Comp: Mail},
    memcached: {label: 'Memcached', Comp: MemoryStick},
    rubyonrails: {label: 'Ruby on Rails', Comp: SiRubyonrails},
    dotnet: {label: '.NET', Comp: SiDotnet},
    springboot: {label: 'Spring Boot', Comp: SiSpringboot},
    deno: {label: 'Deno', Comp: SiDeno},
};

export const TEMPLATE_ICON_OPTIONS = Object.entries(ICON_MAP).map(([slug, {label}]) => ({
    slug,
    label,
}));

type TemplateIconProps = {
    slug: string;
    color?: string;
    size?: number;
    className?: string;
};

export default function TemplateIcon({slug, color, size = 24, className}: TemplateIconProps) {
    const entry = ICON_MAP[slug];
    if (!entry) {
        return <Box size={size} className={className} color={color || 'currentColor'} />;
    }
    const Comp = entry.Comp;
    return <Comp size={size} color={color || 'currentColor'} className={className} />;
}
