import {StartLogStream, StopLogStream} from '../../wailsjs/go/main/App';

const STOP_DELAY_MS = 250;

type StreamEntry = {
    refs: number;
    active: boolean;
    startPromise: Promise<void> | null;
    stopTimer: number | null;
};

const streams = new Map<string, StreamEntry>();

function getEntry(nodeId: string): StreamEntry {
    let entry = streams.get(nodeId);
    if (!entry) {
        entry = {
            refs: 0,
            active: false,
            startPromise: null,
            stopTimer: null,
        };
        streams.set(nodeId, entry);
    }
    return entry;
}

function clearStopTimer(entry: StreamEntry) {
    if (entry.stopTimer !== null) {
        window.clearTimeout(entry.stopTimer);
        entry.stopTimer = null;
    }
}

function deleteIfIdle(nodeId: string, entry: StreamEntry) {
    if (entry.refs === 0 && !entry.active && entry.startPromise === null && entry.stopTimer === null) {
        streams.delete(nodeId);
    }
}

function scheduleStop(nodeId: string, entry: StreamEntry) {
    if (entry.refs > 0 || entry.stopTimer !== null) {
        return;
    }
    entry.stopTimer = window.setTimeout(() => {
        entry.stopTimer = null;
        if (entry.refs > 0) {
            return;
        }
        const shouldStop = entry.active || entry.startPromise !== null;
        entry.active = false;
        if (!shouldStop) {
            deleteIfIdle(nodeId, entry);
            return;
        }
        void StopLogStream(nodeId).catch(() => {}).finally(() => {
            deleteIfIdle(nodeId, entry);
        });
    }, STOP_DELAY_MS);
}

export function acquireLogStream(nodeId: string): Promise<void> {
    const entry = getEntry(nodeId);
    entry.refs += 1;
    clearStopTimer(entry);

    if (entry.active) {
        return Promise.resolve();
    }
    if (entry.startPromise) {
        return entry.startPromise;
    }

    entry.startPromise = StartLogStream(nodeId)
        .then(() => {
            entry.active = true;
        })
        .catch((err) => {
            entry.active = false;
            throw err;
        })
        .finally(() => {
            entry.startPromise = null;
            if (entry.refs === 0) {
                scheduleStop(nodeId, entry);
            }
        });

    return entry.startPromise;
}

export function releaseLogStream(nodeId: string) {
    const entry = streams.get(nodeId);
    if (!entry) {
        return;
    }
    entry.refs = Math.max(0, entry.refs - 1);
    if (entry.refs === 0) {
        scheduleStop(nodeId, entry);
    }
}
