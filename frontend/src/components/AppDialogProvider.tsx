import {createContext, type ReactNode, useCallback, useContext, useMemo, useRef, useState} from 'react';
import Dialog from './Dialog';

type ConfirmOptions = {
    title?: string;
    message: string;
    detail?: string;
    confirmLabel?: string;
    cancelLabel?: string;
    danger?: boolean;
};

type AlertOptions = {
    title?: string;
    message: string;
    detail?: string;
    closeLabel?: string;
};

type DialogContextValue = {
    confirm: (options: ConfirmOptions) => Promise<boolean>;
    alert: (options: AlertOptions) => Promise<void>;
};

type DialogRequest =
    | ({kind: 'confirm'; resolve: (value: boolean) => void} & ConfirmOptions)
    | ({kind: 'alert'; resolve: () => void} & AlertOptions);

const DialogContext = createContext<DialogContextValue | null>(null);

export function AppDialogProvider({children}: {children: ReactNode}) {
    const queueRef = useRef<DialogRequest[]>([]);
    const activeRef = useRef<DialogRequest | null>(null);
    const [active, setActive] = useState<DialogRequest | null>(null);

    const showNext = useCallback(() => {
        const next = queueRef.current.shift() ?? null;
        activeRef.current = next;
        setActive(next);
    }, []);

    const enqueue = useCallback((request: DialogRequest) => {
        if (activeRef.current) {
            queueRef.current.push(request);
            return;
        }
        activeRef.current = request;
        setActive(request);
    }, []);

    const confirm = useCallback((options: ConfirmOptions) => (
        new Promise<boolean>((resolve) => {
            enqueue({...options, kind: 'confirm', resolve});
        })
    ), [enqueue]);

    const alert = useCallback((options: AlertOptions) => (
        new Promise<void>((resolve) => {
            enqueue({...options, kind: 'alert', resolve});
        })
    ), [enqueue]);

    const closeConfirm = useCallback((result: boolean) => {
        const current = activeRef.current;
        if (!current || current.kind !== 'confirm') return;
        current.resolve(result);
        showNext();
    }, [showNext]);

    const closeAlert = useCallback(() => {
        const current = activeRef.current;
        if (!current || current.kind !== 'alert') return;
        current.resolve();
        showNext();
    }, [showNext]);

    const value = useMemo(() => ({confirm, alert}), [confirm, alert]);

    return (
        <DialogContext.Provider value={value}>
            {children}
            {active && (
                <Dialog
                    title={active.title || (active.kind === 'confirm' ? 'Confirm' : 'Notice')}
                    onClose={() => active.kind === 'confirm' ? closeConfirm(false) : closeAlert()}
                    footer={
                        active.kind === 'confirm' ? (
                            <>
                                <button className="btn btn-ghost" onClick={() => closeConfirm(false)}>
                                    {active.cancelLabel || 'Cancel'}
                                </button>
                                <button className={active.danger ? 'btn btn-danger' : 'btn btn-primary'} onClick={() => closeConfirm(true)}>
                                    {active.confirmLabel || 'Confirm'}
                                </button>
                            </>
                        ) : (
                            <button className="btn btn-primary" onClick={closeAlert}>
                                {active.closeLabel || 'Close'}
                            </button>
                        )
                    }
                >
                    <div className="dialog-copy">
                        <p className="dialog-message">{active.message}</p>
                        {active.detail && <p className="dialog-detail">{active.detail}</p>}
                    </div>
                </Dialog>
            )}
        </DialogContext.Provider>
    );
}

export function useAppDialog(): DialogContextValue {
    const value = useContext(DialogContext);
    if (!value) {
        throw new Error('useAppDialog must be used within AppDialogProvider');
    }
    return value;
}
