import {useState} from 'react';
import {FolderOpen} from 'lucide-react';
import {CreateProject, SelectFolder} from '../../wailsjs/go/main/App';
import {store} from '../../wailsjs/go/models';
import Dialog from './Dialog';

type CreateProjectDialogProps = {
    onClose: () => void;
    onCreated: (project: store.Project) => void;
};

export default function CreateProjectDialog({onClose, onCreated}: CreateProjectDialogProps) {
    const [name, setName] = useState('');
    const [path, setPath] = useState('');
    const [description, setDescription] = useState('');
    const [error, setError] = useState('');
    const [submitting, setSubmitting] = useState(false);

    const canSubmit = name.trim() !== '' && path.trim() !== '' && !submitting;

    const submit = () => {
        if (!canSubmit) return;
        setSubmitting(true);
        setError('');
        CreateProject(name, path, description)
            .then(onCreated)
            .catch((e) => {
                setError(String(e));
                setSubmitting(false);
            });
    };

    const onKeyDown = (e: React.KeyboardEvent) => {
        if (e.key === 'Enter') submit();
    };

    return (
        <Dialog
            title="Create Project"
            onClose={onClose}
            footer={
                <>
                    <button className="btn btn-ghost" onClick={onClose}>Cancel</button>
                    <button className="btn btn-primary" disabled={!canSubmit} onClick={submit}>
                        {submitting ? 'Creating…' : 'Create'}
                    </button>
                </>
            }
        >
            <div onKeyDown={onKeyDown}>
                <div className="form-field">
                    <label className="form-label">Name</label>
                    <input
                        className="input"
                        value={name}
                        onChange={(e) => setName(e.target.value)}
                        placeholder="my-app"
                        autoFocus
                    />
                </div>
                <div className="form-field">
                    <label className="form-label">Local path</label>
                    <div className="input-with-action">
                        <input
                            className="input"
                            value={path}
                            onChange={(e) => setPath(e.target.value)}
                            placeholder="C:\Users\me\code\my-app"
                        />
                        <button
                            type="button"
                            className="btn btn-ghost input-action-btn"
                            onClick={() => SelectFolder().then((p) => { if (p) setPath(p); })}
                        >
                            <FolderOpen size={15}/>
                        </button>
                    </div>
                </div>
                <div className="form-field">
                    <label className="form-label">
                        Description <span className="form-optional">optional</span>
                    </label>
                    <input
                        className="input"
                        value={description}
                        onChange={(e) => setDescription(e.target.value)}
                        placeholder="What is this project?"
                    />
                </div>
                {error && <p className="form-error">{error}</p>}
            </div>
        </Dialog>
    );
}
