import {formatSettingValue, type SettingStagingState} from '../lib/settingStaging';
import './SettingStagingNote.css';

export default function SettingStagingNote({key, applied, staged, draft, isStaged, isDraft}: SettingStagingState) {
    if (!isStaged && !isDraft) {
        return null;
    }

    return (
        <div className="setting-staging-notes">
            {isDraft && (
                <p className="setting-staging-note setting-staging-note--draft">
                    Unsaved edit
                    {draft !== undefined ? `: ${formatSettingValue(key, draft)}` : ''}
                    {' '}— stage changes to persist
                </p>
            )}
            {isStaged && (
                <p className="setting-staging-note">
                    <span className="setting-staging-note-label">Live</span>
                    {' '}{formatSettingValue(key, applied)}
                    <span className="setting-staging-note-arrow"> → </span>
                    <span className="setting-staging-note-label">On next deploy</span>
                    {' '}{formatSettingValue(key, staged ?? '')}
                </p>
            )}
        </div>
    );
}
