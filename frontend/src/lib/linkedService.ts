import {useEffect, useMemo, useState} from 'react';
import {GetLinkedServiceInfo} from '../../wailsjs/go/main/App';
import {deploy} from '../../wailsjs/go/models';

type LinkedServiceTarget = {
    loading: boolean;
    isLinked: boolean;
    linkInfo: deploy.LinkedServiceInfo | null;
    targetNodeId: string;
};

export function useLinkedServiceTarget(nodeId: string): LinkedServiceTarget {
    const [loading, setLoading] = useState(true);
    const [linkInfo, setLinkInfo] = useState<deploy.LinkedServiceInfo | null>(null);

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
        };
    }, [loading, linkInfo, nodeId]);
}
