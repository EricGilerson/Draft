import {FolderOpen, FileSearch, Plus, Trash2, GitBranch, RefreshCw} from 'lucide-react';
import {useCallback, useEffect, useState} from 'react';
import {
    GetServiceRoot, SetServiceRoot, SelectServiceRoot,
    GetNodeSettings, SetNodeSetting, SelectFile, ParseDockerfileExpose,
    IsGitRepo, ListGitBranches,
} from '../../wailsjs/go/main/App';
import {dockerfile} from '../../wailsjs/go/models';

type SettingsTabProps = {
    nodeId: string;
    projectId: number;
    projectPath: string;
    onServicesChanged?: () => void;
};

type VolumeEntry = {
    hostPath: string;
    containerPath: string;
    readOnly: boolean;
};

type LabelEntry = {
    key: string;
    value: string;
};

export default function SettingsTab({nodeId, projectId, projectPath, onServicesChanged}: SettingsTabProps) {
    const [rootPath, setRootPath] = useState('');
    const [inputValue, setInputValue] = useState('');
    const [error, setError] = useState('');
    const [saving, setSaving] = useState(false);
    const [dockerfilePath, setDockerfilePath] = useState('');
    const [dockerfileInput, setDockerfileInput] = useState('');
    const [port, setPort] = useState('');
    const [portInput, setPortInput] = useState('');
    const [exposePorts, setExposePorts] = useState<dockerfile.ExposePort[]>([]);
    const [useDockerignore, setUseDockerignore] = useState(false);
    const [useGitignore, setUseGitignore] = useState(false);
    const [useBuildkitLocalContext, setUseBuildkitLocalContext] = useState(true);
    const [gitStream, setGitStream] = useState(true);

    const [settings, setSettings] = useState<Record<string, string>>({});
    const [volumes, setVolumes] = useState<VolumeEntry[]>([]);
    const [labels, setLabels] = useState<LabelEntry[]>([]);

    const [isGitRepo, setIsGitRepo] = useState(false);
    const [gitBranch, setGitBranch] = useState('');
    const [branches, setBranches] = useState<string[]>([]);
    const [branchesLoading, setBranchesLoading] = useState(false);
    const [branchError, setBranchError] = useState('');

    const saveSetting = useCallback((key: string, value: string) => {
        setSettings(prev => ({...prev, [key]: value}));
        SetNodeSetting(nodeId, key, value);
    }, [nodeId]);

    const getSetting = (key: string) => settings[key] || '';

    const refreshBranches = useCallback(() => {
        setBranchesLoading(true);
        setBranchError('');
        ListGitBranches(projectId)
            .then((list) => setBranches(list || []))
            .catch((e) => setBranchError(typeof e === 'string' ? e : e?.message || 'Failed to list branches'))
            .finally(() => setBranchesLoading(false));
    }, [projectId]);

    useEffect(() => {
        IsGitRepo(projectId).then((ok) => {
            setIsGitRepo(ok);
            if (ok) refreshBranches();
        }).catch(() => setIsGitRepo(false));
    }, [projectId, refreshBranches]);

    const commitGitBranch = useCallback((value: string) => {
        setGitBranch(value);
        setSettings(prev => ({...prev, git_branch: value}));
        SetNodeSetting(nodeId, 'git_branch', value).then(() => onServicesChanged?.());
    }, [nodeId, onServicesChanged]);

    useEffect(() => {
        GetServiceRoot(nodeId, projectId).then((path) => {
            setRootPath(path || '');
            setInputValue(path || '');
        });
        GetNodeSettings(nodeId).then((s) => {
            if (!s) s = {};
            setSettings(s);
            setGitBranch(s.git_branch || '');
            const df = s.dockerfile || '';
            setDockerfilePath(df);
            setDockerfileInput(df);
            const p = s.service_port || '';
            setPort(p);
            setPortInput(p);
            setUseDockerignore(s.use_dockerignore === 'true');
            setUseGitignore(s.use_gitignore === 'true');
            setUseBuildkitLocalContext(s.use_buildkit_local_context !== 'false');
            setGitStream(s.git_stream !== 'false');
            if (s.volume_mounts) {
                try { setVolumes(JSON.parse(s.volume_mounts)); } catch { setVolumes([]); }
            }
            if (s.custom_labels) {
                try {
                    const obj = JSON.parse(s.custom_labels);
                    setLabels(Object.entries(obj).map(([key, value]) => ({key, value: value as string})));
                } catch { setLabels([]); }
            }
            if (df) {
                ParseDockerfileExpose(df, projectId).then(setExposePorts).catch(() => setExposePorts([]));
            }
        });
    }, [nodeId, projectId]);

    const saveRoot = useCallback(async (absolutePath: string) => {
        setError('');
        setSaving(true);
        try {
            await SetServiceRoot(nodeId, projectId, absolutePath);
            setRootPath(absolutePath);
            setInputValue(absolutePath);
            onServicesChanged?.();
        } catch (e: any) {
            const msg = typeof e === 'string' ? e : e?.message || 'Failed to set service root';
            setError(msg);
            setInputValue(rootPath);
        } finally {
            setSaving(false);
        }
    }, [nodeId, projectId, rootPath, onServicesChanged]);

    const handleInputCommit = useCallback(() => {
        const trimmed = inputValue.trim();
        if (!trimmed) {
            setInputValue(rootPath);
            return;
        }
        if (trimmed === rootPath) return;

        const sep = projectPath.includes('\\') ? '\\' : '/';
        const isAbsolute = /^[A-Za-z]:[\\/]/.test(trimmed) || trimmed.startsWith('/');
        const absolutePath = isAbsolute ? trimmed : projectPath + sep + trimmed;

        saveRoot(absolutePath);
    }, [inputValue, rootPath, projectPath, saveRoot]);

    const handleBrowse = useCallback(async () => {
        setError('');
        try {
            const selected = await SelectServiceRoot(projectId);
            if (selected) {
                await saveRoot(selected);
            }
        } catch (e: any) {
            const msg = typeof e === 'string' ? e : e?.message || 'Failed to select folder';
            setError(msg);
        }
    }, [projectId, saveRoot]);

    const commitDockerfile = useCallback((value?: string) => {
        const trimmed = (value ?? dockerfileInput).trim();
        if (trimmed === dockerfilePath) return;
        SetNodeSetting(nodeId, 'dockerfile', trimmed).then(() => {
            setDockerfilePath(trimmed);
            setDockerfileInput(trimmed);
            setSettings(prev => ({...prev, dockerfile: trimmed}));
            if (trimmed) {
                ParseDockerfileExpose(trimmed, projectId).then(setExposePorts).catch(() => setExposePorts([]));
            } else {
                setExposePorts([]);
            }
            onServicesChanged?.();
        });
    }, [nodeId, projectId, dockerfileInput, dockerfilePath, onServicesChanged]);

    const browseDockerfile = useCallback(async () => {
        const selected = await SelectFile('Select Dockerfile', projectPath);
        if (selected) {
            setDockerfileInput(selected);
            commitDockerfile(selected);
        }
    }, [projectPath, commitDockerfile]);

    const commitPort = useCallback((value?: string) => {
        const trimmed = (value ?? portInput).trim();
        if (trimmed === port) return;
        if (trimmed && (isNaN(Number(trimmed)) || Number(trimmed) < 1 || Number(trimmed) > 65535)) return;
        SetNodeSetting(nodeId, 'service_port', trimmed).then(() => {
            setPort(trimmed);
            setPortInput(trimmed);
            setSettings(prev => ({...prev, service_port: trimmed}));
            onServicesChanged?.();
        });
    }, [nodeId, portInput, port, onServicesChanged]);

    const toggleDockerignore = useCallback(() => {
        const next = !useDockerignore;
        setUseDockerignore(next);
        saveSetting('use_dockerignore', next ? 'true' : 'false');
    }, [nodeId, useDockerignore, saveSetting]);

    const toggleGitignore = useCallback(() => {
        const next = !useGitignore;
        setUseGitignore(next);
        saveSetting('use_gitignore', next ? 'true' : 'false');
    }, [nodeId, useGitignore, saveSetting]);

    const toggleBuildkitLocalContext = useCallback(() => {
        const next = !useBuildkitLocalContext;
        setUseBuildkitLocalContext(next);
        saveSetting('use_buildkit_local_context', next ? 'true' : 'false');
    }, [nodeId, useBuildkitLocalContext, saveSetting]);

    const toggleGitStream = useCallback(() => {
        const next = !gitStream;
        setGitStream(next);
        saveSetting('git_stream', next ? 'true' : 'false');
    }, [gitStream, saveSetting]);

    const applyExposePort = useCallback((exposePort: number) => {
        const val = String(exposePort);
        setPortInput(val);
        SetNodeSetting(nodeId, 'service_port', val).then(() => {
            setPort(val);
            setSettings(prev => ({...prev, service_port: val}));
            onServicesChanged?.();
        });
    }, [nodeId, onServicesChanged]);

    const saveVolumes = useCallback((vols: VolumeEntry[]) => {
        setVolumes(vols);
        saveSetting('volume_mounts', JSON.stringify(vols));
    }, [saveSetting]);

    const saveLabels = useCallback((lbls: LabelEntry[]) => {
        setLabels(lbls);
        const obj: Record<string, string> = {};
        lbls.forEach(l => { if (l.key) obj[l.key] = l.value; });
        saveSetting('custom_labels', JSON.stringify(obj));
    }, [saveSetting]);

    const displayPath = rootPath
        ? (rootPath.toLowerCase().startsWith(projectPath.toLowerCase())
            ? '.' + rootPath.slice(projectPath.length)
            : rootPath)
        : '';

    const restartPolicy = getSetting('restart_policy') || 'no';

    return (
        <div className="settings-tab">
            {/* ── Source ── */}
            <div className="settings-section">
                <h3 className="settings-section-title">Source</h3>
                {isGitRepo && (
                    <div className="form-field">
                        <label className="form-label">
                            <GitBranch size={13} style={{verticalAlign: '-2px', marginRight: 4}} />
                            Git Branch
                        </label>
                        <span className="settings-hint">
                            Deploy from a specific committed branch instead of the files currently on disk.
                            The branch is exported into a temporary workspace at build time — your working
                            tree and any uncommitted changes are never touched. Leave as “Working tree” to
                            deploy exactly what is on disk (the default).
                        </span>
                        <div className="input-with-action">
                            <select
                                className="input settings-select"
                                value={gitBranch}
                                onChange={(e) => commitGitBranch(e.target.value)}
                            >
                                <option value="">Working tree (files on disk)</option>
                                {gitBranch && !branches.includes(gitBranch) && (
                                    <option value={gitBranch}>{gitBranch} (not found)</option>
                                )}
                                {branches.map((b) => (
                                    <option key={b} value={b}>{b}</option>
                                ))}
                            </select>
                            <button
                                className="btn btn-ghost input-action-btn"
                                onClick={refreshBranches}
                                disabled={branchesLoading}
                                title="Refresh branch list"
                            >
                                <RefreshCw size={14} className={branchesLoading ? 'spin' : ''} />
                            </button>
                        </div>
                        {branchError && <p className="form-error">{branchError}</p>}
                        {gitBranch && !branchError && (
                            <span className="settings-resolved">Deploying from branch “{gitBranch}”</span>
                        )}
                    </div>
                )}
                <div className="form-field">
                    <label className="form-label">Root Directory</label>
                    <span className="settings-hint">
                        Path to the service source code, relative to the project root.
                        Must be inside <span className="settings-mono">{projectPath}</span>
                    </span>
                    <div className="input-with-action">
                        <input
                            className="input"
                            value={inputValue}
                            onChange={(e) => { setInputValue(e.target.value); setError(''); }}
                            onBlur={handleInputCommit}
                            onKeyDown={(e) => {
                                if (e.key === 'Enter') handleInputCommit();
                                if (e.key === 'Escape') setInputValue(rootPath);
                            }}
                            placeholder={projectPath}
                            disabled={saving}
                        />
                        <button className="btn btn-ghost input-action-btn" onClick={handleBrowse} disabled={saving} title="Browse for folder">
                            <FolderOpen size={14} />
                        </button>
                    </div>
                    {error && <p className="form-error">{error}</p>}
                    {displayPath && !error && <span className="settings-resolved">{displayPath}</span>}
                </div>
            </div>

            {/* ── Docker ── */}
            <div className="settings-section">
                <h3 className="settings-section-title">Docker</h3>
                <div className="form-field">
                    <label className="form-label">Dockerfile</label>
                    <span className="settings-hint">Path to the Dockerfile used to build this service.</span>
                    <div className="input-with-action">
                        <input
                            className="input"
                            value={dockerfileInput}
                            onChange={(e) => setDockerfileInput(e.target.value)}
                            onBlur={() => commitDockerfile()}
                            onKeyDown={(e) => {
                                if (e.key === 'Enter') commitDockerfile();
                                if (e.key === 'Escape') setDockerfileInput(dockerfilePath);
                            }}
                            placeholder="Dockerfile"
                        />
                        <button className="btn btn-ghost input-action-btn" onClick={browseDockerfile} title="Browse for Dockerfile">
                            <FileSearch size={14} />
                        </button>
                    </div>
                </div>
            </div>

            {/* ── Build Context ── */}
            <div className="settings-section">
                <h3 className="settings-section-title">Build Context</h3>
                {gitBranch && (
                    <ToggleRow
                        label="Stream branch to Docker"
                        desc="Pipe the branch's committed files straight to Docker for a faster build. .dockerignore, .gitignore and BuildKit local context don't apply while streaming — commit only what you need to keep the context small. Turn off to build from a full checkout that respects your ignore files (slower)."
                        checked={gitStream}
                        onToggle={toggleGitStream}
                    />
                )}
                <ToggleRow label=".dockerignore" desc="Exclude files matched by .dockerignore patterns found in the service root." checked={useDockerignore} onToggle={toggleDockerignore} inactive={gitBranch !== '' && gitStream} inactiveNote="Not applied while streaming from a git branch." />
                <ToggleRow label=".gitignore" desc="Exclude files matched by .gitignore patterns found anywhere in the service root." checked={useGitignore} onToggle={toggleGitignore} inactive={gitBranch !== '' && gitStream} inactiveNote="Not applied while streaming from a git branch." />
                <ToggleRow label="BuildKit local context" desc="Faster on repeated deploys when only a small part of the service changes. Turn it off if you want Draft's legacy tar upload path for maximum compatibility." checked={useBuildkitLocalContext} onToggle={toggleBuildkitLocalContext} inactive={gitBranch !== '' && gitStream} inactiveNote="Not applied while streaming from a git branch." />
            </div>

            {/* ── Build Configuration ── */}
            <div className="settings-section">
                <h3 className="settings-section-title">Build Configuration</h3>
                <SettingInput label="Target Stage" hint="For multi-stage builds, specify which stage to build (--target)." settingKey="build_target" value={getSetting('build_target')} onSave={saveSetting} placeholder="e.g. production" />
                <SettingInput label="Platform" hint="Target platform for the build (e.g. linux/amd64, linux/arm64)." settingKey="build_platform" value={getSetting('build_platform')} onSave={saveSetting} placeholder="e.g. linux/amd64" />
                <ToggleRow label="No Cache" desc="Force a full rebuild without using any cached layers." checked={getSetting('build_no_cache') === 'true'} onToggle={() => saveSetting('build_no_cache', getSetting('build_no_cache') === 'true' ? '' : 'true')} />
            </div>

            {/* ── Networking ── */}
            <div className="settings-section">
                <h3 className="settings-section-title">Networking</h3>
                <div className="form-field">
                    <label className="form-label">Port</label>
                    <span className="settings-hint">The port your service listens on inside the container. Required for deployment.</span>
                    <input
                        className="input"
                        type="number"
                        min={1}
                        max={65535}
                        value={portInput}
                        onChange={(e) => setPortInput(e.target.value)}
                        onBlur={() => commitPort()}
                        onKeyDown={(e) => {
                            if (e.key === 'Enter') commitPort();
                            if (e.key === 'Escape') setPortInput(port);
                        }}
                        placeholder="e.g. 3000"
                    />
                    {exposePorts.length > 0 && (
                        <div className="settings-expose">
                            <span className="settings-expose-label">Dockerfile EXPOSE:</span>
                            <div className="settings-expose-ports">
                                {exposePorts.map((ep) => {
                                    const label = ep.protocol === 'udp' ? `${ep.port}/udp` : String(ep.port);
                                    const isActive = port === String(ep.port);
                                    return (
                                        <button
                                            key={`${ep.port}/${ep.protocol}`}
                                            className={`settings-expose-port ${isActive ? 'settings-expose-port--active' : ''}`}
                                            onClick={() => applyExposePort(ep.port)}
                                            title={isActive ? 'Currently set' : `Use port ${ep.port}`}
                                        >
                                            {label}
                                        </button>
                                    );
                                })}
                            </div>
                            <span className="settings-expose-note">EXPOSE is documentation only — click a port to use it, or enter your own above.</span>
                        </div>
                    )}
                    {port && exposePorts.length > 0 && !exposePorts.some(ep => String(ep.port) === port) && (
                        <span className="settings-expose-warning">
                            Port {port} differs from Dockerfile EXPOSE ({exposePorts.map(ep => ep.port).join(', ')}). This is fine if intentional.
                        </span>
                    )}
                </div>
            </div>

            {/* ── Runtime Command ── */}
            <div className="settings-section">
                <h3 className="settings-section-title">Runtime Command</h3>
                <SettingInput label="Command" hint="Override the Dockerfile CMD. Supports shell syntax (e.g. node server.js --port 3000)." settingKey="cmd_override" value={getSetting('cmd_override')} onSave={saveSetting} placeholder='e.g. node server.js' />
                <SettingInput label="Entrypoint" hint="Override the Dockerfile ENTRYPOINT." settingKey="entrypoint_override" value={getSetting('entrypoint_override')} onSave={saveSetting} placeholder='e.g. /usr/bin/tini --' />
                <SettingInput label="Working Directory" hint="Override the container working directory (WORKDIR)." settingKey="working_dir" value={getSetting('working_dir')} onSave={saveSetting} placeholder="e.g. /app" />
                <SettingInput label="User" hint="Run the container as this user/UID (e.g. node, 1000, 1000:1000)." settingKey="run_user" value={getSetting('run_user')} onSave={saveSetting} placeholder="e.g. node" />
            </div>

            {/* ── Restart Policy ── */}
            <div className="settings-section">
                <h3 className="settings-section-title">Restart Policy</h3>
                <div className="form-field">
                    <label className="form-label">Policy</label>
                    <span className="settings-hint">How Docker should restart the container if it exits.</span>
                    <select
                        className="input settings-select"
                        value={restartPolicy}
                        onChange={(e) => saveSetting('restart_policy', e.target.value)}
                    >
                        <option value="no">No (default)</option>
                        <option value="always">Always</option>
                        <option value="on-failure">On Failure</option>
                        <option value="unless-stopped">Unless Stopped</option>
                    </select>
                </div>
                {restartPolicy === 'on-failure' && (
                    <SettingInput label="Max Retries" hint="Maximum number of restart attempts before giving up." settingKey="restart_max_retries" value={getSetting('restart_max_retries')} onSave={saveSetting} placeholder="e.g. 5" type="number" />
                )}
            </div>

            {/* ── Health Check ── */}
            <div className="settings-section">
                <h3 className="settings-section-title">Health Check</h3>
                <ToggleRow label="Disable Health Check" desc="Ignore any HEALTHCHECK instruction in the Dockerfile." checked={getSetting('healthcheck_disable') === 'true'} onToggle={() => saveSetting('healthcheck_disable', getSetting('healthcheck_disable') === 'true' ? '' : 'true')} />
                {getSetting('healthcheck_disable') !== 'true' && (<>
                    <SettingInput label="Command" hint="Health check command (e.g. curl -f http://localhost:3000/health)." settingKey="healthcheck_cmd" value={getSetting('healthcheck_cmd')} onSave={saveSetting} placeholder="e.g. curl -f http://localhost:3000/health" />
                    <SettingInput label="Interval" hint="Time between checks (Go duration, e.g. 30s, 1m)." settingKey="healthcheck_interval" value={getSetting('healthcheck_interval')} onSave={saveSetting} placeholder="e.g. 30s" />
                    <SettingInput label="Timeout" hint="Max time for a single check." settingKey="healthcheck_timeout" value={getSetting('healthcheck_timeout')} onSave={saveSetting} placeholder="e.g. 10s" />
                    <SettingInput label="Start Period" hint="Grace period before the first health check." settingKey="healthcheck_start_period" value={getSetting('healthcheck_start_period')} onSave={saveSetting} placeholder="e.g. 5s" />
                    <SettingInput label="Retries" hint="Number of consecutive failures before marking as unhealthy." settingKey="healthcheck_retries" value={getSetting('healthcheck_retries')} onSave={saveSetting} placeholder="e.g. 3" type="number" />
                </>)}
            </div>

            {/* ── Resource Limits ── */}
            <div className="settings-section">
                <h3 className="settings-section-title">Resource Limits</h3>
                <SettingInput label="CPU Limit" hint="Maximum CPU cores (e.g. 1.5 = 1.5 cores, 0.5 = half a core)." settingKey="cpu_limit" value={getSetting('cpu_limit')} onSave={saveSetting} placeholder="e.g. 1.5" />
                <SettingInput label="Memory Limit" hint="Maximum memory (e.g. 512m, 1g, 256mb)." settingKey="memory_limit" value={getSetting('memory_limit')} onSave={saveSetting} placeholder="e.g. 512m" />
                <SettingInput label="Memory Reservation" hint="Soft memory limit — Docker will try to keep usage below this." settingKey="memory_reservation" value={getSetting('memory_reservation')} onSave={saveSetting} placeholder="e.g. 256m" />
                <SettingInput label="PID Limit" hint="Maximum number of processes in the container." settingKey="pids_limit" value={getSetting('pids_limit')} onSave={saveSetting} placeholder="e.g. 100" type="number" />
            </div>

            {/* ── Volumes ── */}
            <div className="settings-section">
                <h3 className="settings-section-title">Volumes</h3>
                <span className="settings-hint">Bind mount host directories into the container.</span>
                {volumes.map((vol, i) => (
                    <div key={i} className="settings-kv-row">
                        <input
                            className="input settings-kv-input"
                            value={vol.hostPath}
                            onChange={(e) => {
                                const updated = [...volumes];
                                updated[i] = {...updated[i], hostPath: e.target.value};
                                setVolumes(updated);
                            }}
                            onBlur={() => saveVolumes(volumes)}
                            placeholder="Host path"
                        />
                        <input
                            className="input settings-kv-input"
                            value={vol.containerPath}
                            onChange={(e) => {
                                const updated = [...volumes];
                                updated[i] = {...updated[i], containerPath: e.target.value};
                                setVolumes(updated);
                            }}
                            onBlur={() => saveVolumes(volumes)}
                            placeholder="Container path"
                        />
                        <label className="settings-kv-check" title="Read-only">
                            <input
                                type="checkbox"
                                checked={vol.readOnly}
                                onChange={(e) => {
                                    const updated = [...volumes];
                                    updated[i] = {...updated[i], readOnly: e.target.checked};
                                    saveVolumes(updated);
                                }}
                            />
                            <span className="settings-kv-check-label">RO</span>
                        </label>
                        <button className="btn btn-ghost settings-kv-remove" onClick={() => saveVolumes(volumes.filter((_, j) => j !== i))} title="Remove">
                            <Trash2 size={12} />
                        </button>
                    </div>
                ))}
                <button className="btn btn-ghost settings-add-btn" onClick={() => { setVolumes([...volumes, {hostPath: '', containerPath: '', readOnly: false}]); }}>
                    <Plus size={12} /> Add Volume
                </button>
            </div>

            {/* ── Lifecycle Hooks ── */}
            <div className="settings-section">
                <h3 className="settings-section-title">Lifecycle Hooks</h3>
                <SettingInput label="Pre-Build" hint="Shell command to run on your machine before building the image. If you're streaming from a git branch (the “Stream branch to Docker” option), any files this command generates on disk won't be included — the build context comes straight from git. Turn that option off to build from a full checkout that picks them up." settingKey="pre_build_cmd" value={getSetting('pre_build_cmd')} onSave={saveSetting} placeholder="e.g. npm run generate" />
                <SettingInput label="Post-Build" hint="Shell command to run on your machine after a successful build." settingKey="post_build_cmd" value={getSetting('post_build_cmd')} onSave={saveSetting} placeholder="e.g. echo Build complete" />
                <SettingInput label="Pre-Deploy" hint="Shell command to run on your machine before starting the container." settingKey="pre_deploy_cmd" value={getSetting('pre_deploy_cmd')} onSave={saveSetting} placeholder="e.g. ./scripts/migrate.sh" />
                <SettingInput label="Post-Deploy" hint="Shell command to run on your machine after the container is running." settingKey="post_deploy_cmd" value={getSetting('post_deploy_cmd')} onSave={saveSetting} placeholder="e.g. curl http://localhost:3000/warmup" />
                <div className="form-field">
                    <label className="form-label">Stop Signal</label>
                    <span className="settings-hint">Signal sent to the container when stopping (default: SIGTERM).</span>
                    <select
                        className="input settings-select"
                        value={getSetting('stop_signal') || ''}
                        onChange={(e) => saveSetting('stop_signal', e.target.value)}
                    >
                        <option value="">Default (SIGTERM)</option>
                        <option value="SIGTERM">SIGTERM</option>
                        <option value="SIGINT">SIGINT</option>
                        <option value="SIGQUIT">SIGQUIT</option>
                        <option value="SIGKILL">SIGKILL</option>
                    </select>
                </div>
                <SettingInput label="Stop Grace Period" hint="Seconds to wait after stop signal before force-killing." settingKey="stop_grace_period" value={getSetting('stop_grace_period')} onSave={saveSetting} placeholder="e.g. 10" type="number" />
            </div>

            {/* ── Security ── */}
            <div className="settings-section">
                <h3 className="settings-section-title">Security</h3>
                <ToggleRow label="Privileged" desc="Run the container with full host privileges. Use with caution." checked={getSetting('privileged') === 'true'} onToggle={() => saveSetting('privileged', getSetting('privileged') === 'true' ? '' : 'true')} />
                <ToggleRow label="Init Process" desc="Run an init process (tini) as PID 1 to handle signal forwarding and zombie reaping." checked={getSetting('init_process') === 'true'} onToggle={() => saveSetting('init_process', getSetting('init_process') === 'true' ? '' : 'true')} />
                <ToggleRow label="Read-Only Root Filesystem" desc="Mount the container root filesystem as read-only." checked={getSetting('readonly_rootfs') === 'true'} onToggle={() => saveSetting('readonly_rootfs', getSetting('readonly_rootfs') === 'true' ? '' : 'true')} />
                <SettingInput label="Add Capabilities" hint="Comma-separated Linux capabilities to add (e.g. SYS_PTRACE, NET_ADMIN)." settingKey="cap_add" value={getSetting('cap_add')} onSave={saveSetting} placeholder="e.g. SYS_PTRACE, NET_ADMIN" />
                <SettingInput label="Drop Capabilities" hint="Comma-separated Linux capabilities to drop." settingKey="cap_drop" value={getSetting('cap_drop')} onSave={saveSetting} placeholder="e.g. NET_RAW, MKNOD" />
            </div>

            {/* ── Custom Labels ── */}
            <div className="settings-section">
                <h3 className="settings-section-title">Custom Labels</h3>
                <span className="settings-hint">Key-value labels applied to the container. Draft labels are added automatically.</span>
                {labels.map((lbl, i) => (
                    <div key={i} className="settings-kv-row">
                        <input
                            className="input settings-kv-input"
                            value={lbl.key}
                            onChange={(e) => {
                                const updated = [...labels];
                                updated[i] = {...updated[i], key: e.target.value};
                                setLabels(updated);
                            }}
                            onBlur={() => saveLabels(labels)}
                            placeholder="Label key"
                        />
                        <input
                            className="input settings-kv-input"
                            value={lbl.value}
                            onChange={(e) => {
                                const updated = [...labels];
                                updated[i] = {...updated[i], value: e.target.value};
                                setLabels(updated);
                            }}
                            onBlur={() => saveLabels(labels)}
                            placeholder="Label value"
                        />
                        <button className="btn btn-ghost settings-kv-remove" onClick={() => saveLabels(labels.filter((_, j) => j !== i))} title="Remove">
                            <Trash2 size={12} />
                        </button>
                    </div>
                ))}
                <button className="btn btn-ghost settings-add-btn" onClick={() => setLabels([...labels, {key: '', value: ''}])}>
                    <Plus size={12} /> Add Label
                </button>
            </div>
        </div>
    );
}

function ToggleRow({label, desc, checked, onToggle, inactive, inactiveNote}: {label: string; desc: string; checked: boolean; onToggle: () => void; inactive?: boolean; inactiveNote?: string}) {
    return (
        <div className={`settings-toggle-row${inactive ? ' settings-toggle-row--inactive' : ''}`}>
            <div className="settings-toggle-label">
                <span className="settings-toggle-name">{label}</span>
                <span className="settings-toggle-desc">
                    {desc}
                    {inactive && inactiveNote && <em className="settings-toggle-inactive-note"> {inactiveNote}</em>}
                </span>
            </div>
            <button
                className={`toggle-switch${checked ? ' toggle-switch--on' : ''}`}
                onClick={onToggle}
                role="switch"
                aria-checked={checked}
            />
        </div>
    );
}

function SettingInput({label, hint, settingKey, value, onSave, placeholder, type}: {
    label: string;
    hint: string;
    settingKey: string;
    value: string;
    onSave: (key: string, value: string) => void;
    placeholder?: string;
    type?: string;
}) {
    const [local, setLocal] = useState(value);

    useEffect(() => { setLocal(value); }, [value]);

    const commit = () => {
        const trimmed = local.trim();
        if (trimmed !== value) {
            onSave(settingKey, trimmed);
        }
    };

    return (
        <div className="form-field">
            <label className="form-label">{label}</label>
            <span className="settings-hint">{hint}</span>
            <input
                className="input"
                type={type || 'text'}
                value={local}
                onChange={(e) => setLocal(e.target.value)}
                onBlur={commit}
                onKeyDown={(e) => {
                    if (e.key === 'Enter') commit();
                    if (e.key === 'Escape') setLocal(value);
                }}
                placeholder={placeholder}
            />
        </div>
    );
}
