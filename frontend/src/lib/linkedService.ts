import {useCallback, useEffect, useMemo, useState} from 'react';
import {GetLinkedServiceInfo} from '../../wailsjs/go/main/App';
import {deploy} from '../../wailsjs/go/models';

type LinkedServiceTarget = {
    loading: boolean;
    isLinked: boolean;
    linkInfo: deploy.LinkedServiceInfo | null;
    targetNodeId: string;
    refresh: () => Promise<void>;
};

export function useLinkedServiceTarget(nodeId: string): LinkedServiceTarget {
    const [loading, setLoading] = useState(true);
    const [linkInfo, setLinkInfo] = useState<deploy.LinkedServiceInfo | null>(null);

    const refresh = useCallback(async () => {
        setLoading(true);
        try {
            const info = await GetLinkedServiceInfo(nodeId);
            setLinkInfo(info ?? null);
        } catch {
            setLinkInfo(null);
        } finally {
            setLoading(false);
        }
    }, [nodeId]);

    useEffect(() => {
        let cancelled = false;
        setLoading(true);
        setLinkInfo(null);
        GetLinkedServiceInfo(nodeId)
            .then((info) => {
                if (!cancelled) setLinkInfo(info ?? null);
            })
            .catch(() => {
                if (!cancelled) setLinkInfo(null);
            })
            .finally(() => {
                if (!cancelled) setLoading(false);
            });
        return () => {
            cancelled = true;
        };
    }, [nodeId]);

    return useMemo(() => {
        const isLinked = !!linkInfo?.isLinked && !!linkInfo?.rootNodeId;
        const targetNodeId = isLinked ? (linkInfo?.rootNodeId || nodeId) : nodeId;
        return {
            loading,
            isLinked,
            linkInfo,
            targetNodeId,
            refresh,
        };
    }, [loading, linkInfo, nodeId, refresh]);
}
