import {useEffect, useRef, useState} from 'react';
import {EventsOn} from '../../wailsjs/runtime/runtime';
import './ActivityTicker.css';

type DockerEvent = {
    type: string;
    action: string;
    actor: string;
    name: string;
    image: string;
};

type TickerEntry = {
    id: number;
    text: string;
    fading: boolean;
};

let nextId = 0;

function summarize(ev: DockerEvent): string | null {
    const label = ev.name || ev.actor?.slice(0, 12) || '';
    const img = ev.image || '';

    switch (ev.type) {
        case 'container':
            switch (ev.action) {
                case 'create':  return `Container created ${label}${img ? ` (${img})` : ''}`;
                case 'start':   return `Container started ${label}`;
                case 'stop':    return `Container stopped ${label}`;
                case 'die':     return `Container exited ${label}`;
                case 'destroy': return `Container removed ${label}`;
                case 'kill':    return `Container killed ${label}`;
                case 'pause':   return `Container paused ${label}`;
                case 'unpause': return `Container unpaused ${label}`;
                case 'restart': return `Container restarted ${label}`;
                default:        return `Container ${ev.action} ${label}`;
            }
        case 'image':
            switch (ev.action) {
                case 'pull':    return `Image pulled ${label || img}`;
                case 'build':   return `Image built ${label || img}`;
                case 'delete':  return `Image removed ${label || img}`;
                case 'tag':     return `Image tagged ${label || img}`;
                case 'untag':   return `Image untagged ${label || img}`;
                default:        return `Image ${ev.action} ${label || img}`;
            }
        case 'network':
            return `Network ${ev.action} ${label}`;
        case 'volume':
            return `Volume ${ev.action} ${label}`;
        default:
            return null;
    }
}

const MAX_VISIBLE = 3;
const FADE_DELAY = 4000;

export default function ActivityTicker() {
    const [entries, setEntries] = useState<TickerEntry[]>([]);
    const timersRef = useRef<Map<number, ReturnType<typeof setTimeout>>>(new Map());

    useEffect(() => {
        const unsub = EventsOn('docker:activity', (ev: DockerEvent) => {
            const text = summarize(ev);
            if (!text) return;

            const id = nextId++;
            setEntries(prev => {
                const next = [{id, text, fading: false}, ...prev];
                return next.slice(0, MAX_VISIBLE + 2);
            });

            const timer = setTimeout(() => {
                setEntries(prev => prev.map(e => e.id === id ? {...e, fading: true} : e));
                const removeTimer = setTimeout(() => {
                    setEntries(prev => prev.filter(e => e.id !== id));
                    timersRef.current.delete(id);
                }, 600);
                timersRef.current.set(id, removeTimer);
            }, FADE_DELAY);
            timersRef.current.set(id, timer);
        });

        return () => {
            unsub();
            timersRef.current.forEach(t => clearTimeout(t));
            timersRef.current.clear();
        };
    }, []);

    if (entries.length === 0) return null;

    return (
        <div className="activity-ticker">
            {entries.slice(0, MAX_VISIBLE).map(entry => (
                <span
                    key={entry.id}
                    className={`ticker-entry${entry.fading ? ' ticker-entry--fade' : ''}`}
                >
                    {entry.text}
                </span>
            ))}
        </div>
    );
}
