export namespace build {
	
	export class CacheRecord {
	    ID: string;
	    Parent?: string;
	    Parents?: string[];
	    Type: string;
	    Description: string;
	    InUse: boolean;
	    Shared: boolean;
	    Size: number;
	    // Go type: time
	    CreatedAt: any;
	    // Go type: time
	    LastUsedAt?: any;
	    UsageCount: number;
	
	    static createFrom(source: any = {}) {
	        return new CacheRecord(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ID = source["ID"];
	        this.Parent = source["Parent"];
	        this.Parents = source["Parents"];
	        this.Type = source["Type"];
	        this.Description = source["Description"];
	        this.InUse = source["InUse"];
	        this.Shared = source["Shared"];
	        this.Size = source["Size"];
	        this.CreatedAt = this.convertValues(source["CreatedAt"], null);
	        this.LastUsedAt = this.convertValues(source["LastUsedAt"], null);
	        this.UsageCount = source["UsageCount"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace cloudconfig {
	
	export class Note {
	    kind: string;
	    code: string;
	    field?: string;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new Note(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.code = source["code"];
	        this.field = source["field"];
	        this.message = source["message"];
	    }
	}
	export class Report {
	    notes: Note[];
	
	    static createFrom(source: any = {}) {
	        return new Report(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.notes = this.convertValues(source["notes"], Note);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace container {
	
	export class MountPoint {
	    Source: string;
	    Destination: string;
	    Mode: string;
	    RW: boolean;
	    Propagation: string;
	
	    static createFrom(source: any = {}) {
	        return new MountPoint(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Source = source["Source"];
	        this.Destination = source["Destination"];
	        this.Mode = source["Mode"];
	        this.RW = source["RW"];
	        this.Propagation = source["Propagation"];
	    }
	}
	export class NetworkSettingsSummary {
	    Networks: Record<string, network.EndpointSettings>;
	
	    static createFrom(source: any = {}) {
	        return new NetworkSettingsSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Networks = this.convertValues(source["Networks"], network.EndpointSettings, true);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Port {
	    IP?: string;
	    PrivatePort: number;
	    PublicPort?: number;
	    Type: string;
	
	    static createFrom(source: any = {}) {
	        return new Port(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.IP = source["IP"];
	        this.PrivatePort = source["PrivatePort"];
	        this.PublicPort = source["PublicPort"];
	        this.Type = source["Type"];
	    }
	}
	export class Summary {
	    Id: string;
	    Names: string[];
	    Image: string;
	    ImageID: string;
	    ImageManifestDescriptor?: v1.Descriptor;
	    Command: string;
	    Created: number;
	    Ports: Port[];
	    Labels: Record<string, string>;
	    State: string;
	    Status: string;
	    // Go type: struct { NetworkMode string "json:\",omitempty\""; Annotations map[string]string "json:\",omitempty\"" }
	    HostConfig: any;
	    NetworkSettings?: NetworkSettingsSummary;
	    Mounts: MountPoint[];
	
	    static createFrom(source: any = {}) {
	        return new Summary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Id = source["Id"];
	        this.Names = source["Names"];
	        this.Image = source["Image"];
	        this.ImageID = source["ImageID"];
	        this.ImageManifestDescriptor = this.convertValues(source["ImageManifestDescriptor"], v1.Descriptor);
	        this.Command = source["Command"];
	        this.Created = source["Created"];
	        this.Ports = this.convertValues(source["Ports"], Port);
	        this.Labels = source["Labels"];
	        this.State = source["State"];
	        this.Status = source["Status"];
	        this.HostConfig = this.convertValues(source["HostConfig"], Object);
	        this.NetworkSettings = this.convertValues(source["NetworkSettings"], NetworkSettingsSummary);
	        this.Mounts = this.convertValues(source["Mounts"], MountPoint);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace deploy {
	
	export class CloneVolumePreview {
	    sourceVolume: string;
	    targetVolume: string;
	    sourceSize: number;
	    targetSize: number;
	    sourceRunning: boolean;
	    targetRunning: boolean;
	    willOrphanVolume: boolean;
	    containerPath: string;
	    warning: string;
	
	    static createFrom(source: any = {}) {
	        return new CloneVolumePreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sourceVolume = source["sourceVolume"];
	        this.targetVolume = source["targetVolume"];
	        this.sourceSize = source["sourceSize"];
	        this.targetSize = source["targetSize"];
	        this.sourceRunning = source["sourceRunning"];
	        this.targetRunning = source["targetRunning"];
	        this.willOrphanVolume = source["willOrphanVolume"];
	        this.containerPath = source["containerPath"];
	        this.warning = source["warning"];
	    }
	}
	export class CloneVolumeResult {
	    newVolumeName: string;
	    orphanedVolumeName?: string;
	
	    static createFrom(source: any = {}) {
	        return new CloneVolumeResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.newVolumeName = source["newVolumeName"];
	        this.orphanedVolumeName = source["orphanedVolumeName"];
	    }
	}
	export class Connection {
	    sourceNodeId: string;
	    sourceKey: string;
	    targetNodeId: string;
	    targetAttr: string;
	
	    static createFrom(source: any = {}) {
	        return new Connection(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sourceNodeId = source["sourceNodeId"];
	        this.sourceKey = source["sourceKey"];
	        this.targetNodeId = source["targetNodeId"];
	        this.targetAttr = source["targetAttr"];
	    }
	}
	export class ContainerSummary {
	    id: string;
	    names: string[];
	    image: string;
	    state: string;
	    status: string;
	    ports: container.Port[];
	    created: number;
	    sizeRw: number;
	    sizeRootFs: number;
	    labels: Record<string, string>;
	    managed: boolean;
	    projectId?: number;
	    projectName?: string;
	    nodeId?: string;
	    nodeLabel?: string;
	
	    static createFrom(source: any = {}) {
	        return new ContainerSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.names = source["names"];
	        this.image = source["image"];
	        this.state = source["state"];
	        this.status = source["status"];
	        this.ports = this.convertValues(source["ports"], container.Port);
	        this.created = source["created"];
	        this.sizeRw = source["sizeRw"];
	        this.sizeRootFs = source["sizeRootFs"];
	        this.labels = source["labels"];
	        this.managed = source["managed"];
	        this.projectId = source["projectId"];
	        this.projectName = source["projectName"];
	        this.nodeId = source["nodeId"];
	        this.nodeLabel = source["nodeLabel"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CreateNodeFromTemplateRequest {
	    id: string;
	    label: string;
	    projectId: number;
	    environmentId: number;
	    x: number;
	    y: number;
	    templateId: number;
	    serviceRoot: string;
	    overrides: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new CreateNodeFromTemplateRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.label = source["label"];
	        this.projectId = source["projectId"];
	        this.environmentId = source["environmentId"];
	        this.x = source["x"];
	        this.y = source["y"];
	        this.templateId = source["templateId"];
	        this.serviceRoot = source["serviceRoot"];
	        this.overrides = source["overrides"];
	    }
	}
	export class CreateNodeFromTemplateResult {
	    node: store.CanvasNode;
	    warnings: string[];
	    deployStarted: boolean;
	
	    static createFrom(source: any = {}) {
	        return new CreateNodeFromTemplateResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.node = this.convertValues(source["node"], store.CanvasNode);
	        this.warnings = source["warnings"];
	        this.deployStarted = source["deployStarted"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ReferenceDependent {
	    sourceNodeId: string;
	    sourceLabel: string;
	    varKey: string;
	    token: string;
	
	    static createFrom(source: any = {}) {
	        return new ReferenceDependent(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sourceNodeId = source["sourceNodeId"];
	        this.sourceLabel = source["sourceLabel"];
	        this.varKey = source["varKey"];
	        this.token = source["token"];
	    }
	}
	export class DeleteServicePreview {
	    label: string;
	    isRunning: boolean;
	    managedVolumeCount: number;
	    dependents: ReferenceDependent[];
	    activeAliases?: string[];
	
	    static createFrom(source: any = {}) {
	        return new DeleteServicePreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.label = source["label"];
	        this.isRunning = source["isRunning"];
	        this.managedVolumeCount = source["managedVolumeCount"];
	        this.dependents = this.convertValues(source["dependents"], ReferenceDependent);
	        this.activeAliases = source["activeAliases"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class DeploymentHistorySummary {
	    totalDeployments: number;
	    recentWindow: number;
	    successCount: number;
	    failureCount: number;
	    interruptedCount: number;
	    // Go type: time
	    lastDeployAt?: any;
	    // Go type: time
	    lastFailureAt?: any;
	    lastFailureReason: string;
	    lastBuildDurationMs: number;
	    lastBootDurationMs: number;
	    lastRunDurationMs: number;
	
	    static createFrom(source: any = {}) {
	        return new DeploymentHistorySummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.totalDeployments = source["totalDeployments"];
	        this.recentWindow = source["recentWindow"];
	        this.successCount = source["successCount"];
	        this.failureCount = source["failureCount"];
	        this.interruptedCount = source["interruptedCount"];
	        this.lastDeployAt = this.convertValues(source["lastDeployAt"], null);
	        this.lastFailureAt = this.convertValues(source["lastFailureAt"], null);
	        this.lastFailureReason = source["lastFailureReason"];
	        this.lastBuildDurationMs = source["lastBuildDurationMs"];
	        this.lastBootDurationMs = source["lastBootDurationMs"];
	        this.lastRunDurationMs = source["lastRunDurationMs"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class DeploymentTimelineItem {
	    deploymentId: number;
	    status: string;
	    imageTag: string;
	    // Go type: time
	    createdAt: any;
	    // Go type: time
	    finishedAt?: any;
	    buildDurationMs: number;
	    bootDurationMs: number;
	    runDurationMs: number;
	    exitCode?: number;
	    error: string;
	    oomKilled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new DeploymentTimelineItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.deploymentId = source["deploymentId"];
	        this.status = source["status"];
	        this.imageTag = source["imageTag"];
	        this.createdAt = this.convertValues(source["createdAt"], null);
	        this.finishedAt = this.convertValues(source["finishedAt"], null);
	        this.buildDurationMs = source["buildDurationMs"];
	        this.bootDurationMs = source["bootDurationMs"];
	        this.runDurationMs = source["runDurationMs"];
	        this.exitCode = source["exitCode"];
	        this.error = source["error"];
	        this.oomKilled = source["oomKilled"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class DockerfileBuildInfo {
	    buildMode: boolean;
	    parsed: boolean;
	    hasBuildStep: boolean;
	    declaredArgs: string[];
	    buildStageArgs: string[];
	
	    static createFrom(source: any = {}) {
	        return new DockerfileBuildInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.buildMode = source["buildMode"];
	        this.parsed = source["parsed"];
	        this.hasBuildStep = source["hasBuildStep"];
	        this.declaredArgs = source["declaredArgs"];
	        this.buildStageArgs = source["buildStageArgs"];
	    }
	}
	export class EnvironmentStackNodeResult {
	    nodeId: string;
	    label: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new EnvironmentStackNodeResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.nodeId = source["nodeId"];
	        this.label = source["label"];
	        this.error = source["error"];
	    }
	}
	export class EnvironmentStackResult {
	    action: string;
	    total: number;
	    succeeded: number;
	    failed: number;
	    results: EnvironmentStackNodeResult[];
	
	    static createFrom(source: any = {}) {
	        return new EnvironmentStackResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.action = source["action"];
	        this.total = source["total"];
	        this.succeeded = source["succeeded"];
	        this.failed = source["failed"];
	        this.results = this.convertValues(source["results"], EnvironmentStackNodeResult);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ExportedFile {
	    name: string;
	    content: string;
	
	    static createFrom(source: any = {}) {
	        return new ExportedFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.content = source["content"];
	    }
	}
	export class ExportResult {
	    format: string;
	    files: ExportedFile[];
	    report: cloudconfig.Report;
	
	    static createFrom(source: any = {}) {
	        return new ExportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.format = source["format"];
	        this.files = this.convertValues(source["files"], ExportedFile);
	        this.report = this.convertValues(source["report"], cloudconfig.Report);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class ImageSummary {
	    id: string;
	    parentId?: string;
	    repoTags: string[];
	    size: number;
	    sharedSize: number;
	    containers: number;
	    created: number;
	    dangling: boolean;
	    managed: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ImageSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.parentId = source["parentId"];
	        this.repoTags = source["repoTags"];
	        this.size = source["size"];
	        this.sharedSize = source["sharedSize"];
	        this.containers = source["containers"];
	        this.created = source["created"];
	        this.dangling = source["dangling"];
	        this.managed = source["managed"];
	    }
	}
	export class ServicePreview {
	    name: string;
	    mode: string;
	    image?: string;
	    port?: string;
	
	    static createFrom(source: any = {}) {
	        return new ServicePreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.mode = source["mode"];
	        this.image = source["image"];
	        this.port = source["port"];
	    }
	}
	export class ImportPreview {
	    format: string;
	    services: ServicePreview[];
	    report: cloudconfig.Report;
	
	    static createFrom(source: any = {}) {
	        return new ImportPreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.format = source["format"];
	        this.services = this.convertValues(source["services"], ServicePreview);
	        this.report = this.convertValues(source["report"], cloudconfig.Report);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ImportResult {
	    projectId: number;
	    nodes: store.CanvasNode[];
	    report: cloudconfig.Report;
	
	    static createFrom(source: any = {}) {
	        return new ImportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectId = source["projectId"];
	        this.nodes = this.convertValues(source["nodes"], store.CanvasNode);
	        this.report = this.convertValues(source["report"], cloudconfig.Report);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class LinkToSharedRootPreview {
	    nodeLabel: string;
	    isRunning: boolean;
	    managedVolumeCount: number;
	    rootNodeId: string;
	    rootLabel: string;
	    rootEnvName: string;
	    warningKind?: string;
	    warning?: string;
	
	    static createFrom(source: any = {}) {
	        return new LinkToSharedRootPreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.nodeLabel = source["nodeLabel"];
	        this.isRunning = source["isRunning"];
	        this.managedVolumeCount = source["managedVolumeCount"];
	        this.rootNodeId = source["rootNodeId"];
	        this.rootLabel = source["rootLabel"];
	        this.rootEnvName = source["rootEnvName"];
	        this.warningKind = source["warningKind"];
	        this.warning = source["warning"];
	    }
	}
	export class LinkedServiceInfo {
	    isLinked: boolean;
	    rootNodeId?: string;
	    rootLabel?: string;
	    rootEnvironmentId?: number;
	    rootEnvName?: string;
	    hasVolumes?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new LinkedServiceInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.isLinked = source["isLinked"];
	        this.rootNodeId = source["rootNodeId"];
	        this.rootLabel = source["rootLabel"];
	        this.rootEnvironmentId = source["rootEnvironmentId"];
	        this.rootEnvName = source["rootEnvName"];
	        this.hasVolumes = source["hasVolumes"];
	    }
	}
	export class ManagedVolume {
	    name: string;
	    labels: Record<string, string>;
	    mountpoint: string;
	    driver: string;
	    createdAt: string;
	    size: number;
	    refCount: number;
	    projectId: number;
	    nodeId: string;
	    target: string;
	    environment: string;
	
	    static createFrom(source: any = {}) {
	        return new ManagedVolume(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.labels = source["labels"];
	        this.mountpoint = source["mountpoint"];
	        this.driver = source["driver"];
	        this.createdAt = source["createdAt"];
	        this.size = source["size"];
	        this.refCount = source["refCount"];
	        this.projectId = source["projectId"];
	        this.nodeId = source["nodeId"];
	        this.target = source["target"];
	        this.environment = source["environment"];
	    }
	}
	export class MetricPoint {
	    // Go type: time
	    timestamp: any;
	    cpuPercent: number;
	    memoryBytes: number;
	    memoryLimitBytes: number;
	    networkRxBytes: number;
	    networkTxBytes: number;
	    networkRxRateBps: number;
	    networkTxRateBps: number;
	
	    static createFrom(source: any = {}) {
	        return new MetricPoint(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.timestamp = this.convertValues(source["timestamp"], null);
	        this.cpuPercent = source["cpuPercent"];
	        this.memoryBytes = source["memoryBytes"];
	        this.memoryLimitBytes = source["memoryLimitBytes"];
	        this.networkRxBytes = source["networkRxBytes"];
	        this.networkTxBytes = source["networkTxBytes"];
	        this.networkRxRateBps = source["networkRxRateBps"];
	        this.networkTxRateBps = source["networkTxRateBps"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class NetworkSummary {
	    id: string;
	    name: string;
	    driver: string;
	    scope: string;
	    created: string;
	    labels: Record<string, string>;
	    containers: number;
	    managed: boolean;
	
	    static createFrom(source: any = {}) {
	        return new NetworkSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.driver = source["driver"];
	        this.scope = source["scope"];
	        this.created = source["created"];
	        this.labels = source["labels"];
	        this.containers = source["containers"];
	        this.managed = source["managed"];
	    }
	}
	export class StagedEnvVarChange {
	    key: string;
	    value: string;
	    scope: string;
	    delete: boolean;
	
	    static createFrom(source: any = {}) {
	        return new StagedEnvVarChange(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.value = source["value"];
	        this.scope = source["scope"];
	        this.delete = source["delete"];
	    }
	}
	export class NodeConfigStatus {
	    appliedSettings: Record<string, string>;
	    stagedSettings: Record<string, string>;
	    stagedEnvChanges: StagedEnvVarChange[];
	    hasStagedChanges: boolean;
	    activeDeploymentStatus: string;
	
	    static createFrom(source: any = {}) {
	        return new NodeConfigStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.appliedSettings = source["appliedSettings"];
	        this.stagedSettings = source["stagedSettings"];
	        this.stagedEnvChanges = this.convertValues(source["stagedEnvChanges"], StagedEnvVarChange);
	        this.hasStagedChanges = source["hasStagedChanges"];
	        this.activeDeploymentStatus = source["activeDeploymentStatus"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class NodeHealth {
	    nodeId: string;
	    status: string;
	    dockerHealth: string;
	    hostPort: number;
	    hostname: string;
	    internalUrl: string;
	    publicUrl: string;
	    routeProtocol: string;
	
	    static createFrom(source: any = {}) {
	        return new NodeHealth(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.nodeId = source["nodeId"];
	        this.status = source["status"];
	        this.dockerHealth = source["dockerHealth"];
	        this.hostPort = source["hostPort"];
	        this.hostname = source["hostname"];
	        this.internalUrl = source["internalUrl"];
	        this.publicUrl = source["publicUrl"];
	        this.routeProtocol = source["routeProtocol"];
	    }
	}
	export class PruneReport {
	    spaceReclaimed: number;
	    removed?: string[];
	
	    static createFrom(source: any = {}) {
	        return new PruneReport(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.spaceReclaimed = source["spaceReclaimed"];
	        this.removed = source["removed"];
	    }
	}
	export class ReachabilityCheck {
	    status: string;
	    targetUrl: string;
	    statusCode: number;
	    latencyMs: number;
	    error: string;
	    // Go type: time
	    checkedAt?: any;
	
	    static createFrom(source: any = {}) {
	        return new ReachabilityCheck(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	        this.targetUrl = source["targetUrl"];
	        this.statusCode = source["statusCode"];
	        this.latencyMs = source["latencyMs"];
	        this.error = source["error"];
	        this.checkedAt = this.convertValues(source["checkedAt"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class ReferenceIssue {
	    varKey: string;
	    token: string;
	    reason: string;
	
	    static createFrom(source: any = {}) {
	        return new ReferenceIssue(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.varKey = source["varKey"];
	        this.token = source["token"];
	        this.reason = source["reason"];
	    }
	}
	export class ReferenceTarget {
	    nodeId: string;
	    label: string;
	    attributes: string[];
	    customKeys: string[];
	
	    static createFrom(source: any = {}) {
	        return new ReferenceTarget(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.nodeId = source["nodeId"];
	        this.label = source["label"];
	        this.attributes = source["attributes"];
	        this.customKeys = source["customKeys"];
	    }
	}
	export class RollbackEligibility {
	    deploymentId: number;
	    eligible: boolean;
	    method: string;
	    reason: string;
	
	    static createFrom(source: any = {}) {
	        return new RollbackEligibility(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.deploymentId = source["deploymentId"];
	        this.eligible = source["eligible"];
	        this.method = source["method"];
	        this.reason = source["reason"];
	    }
	}
	export class RootServiceSummary {
	    nodeId: string;
	    label: string;
	    environmentId: number;
	    envName: string;
	    envSlug: string;
	    templateId: number;
	    hasVolumes: boolean;
	    warningKind?: string;
	    warning?: string;
	    matchReason?: string;
	
	    static createFrom(source: any = {}) {
	        return new RootServiceSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.nodeId = source["nodeId"];
	        this.label = source["label"];
	        this.environmentId = source["environmentId"];
	        this.envName = source["envName"];
	        this.envSlug = source["envSlug"];
	        this.templateId = source["templateId"];
	        this.hasVolumes = source["hasVolumes"];
	        this.warningKind = source["warningKind"];
	        this.warning = source["warning"];
	        this.matchReason = source["matchReason"];
	    }
	}
	export class RunCommandResult {
	    exitCode: number;
	    output: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new RunCommandResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.exitCode = source["exitCode"];
	        this.output = source["output"];
	        this.error = source["error"];
	    }
	}
	export class RuntimeEvent {
	    kind: string;
	    severity: string;
	    // Go type: time
	    at: any;
	    summary: string;
	
	    static createFrom(source: any = {}) {
	        return new RuntimeEvent(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.severity = source["severity"];
	        this.at = this.convertValues(source["at"], null);
	        this.summary = source["summary"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SandboxRepositoryRef {
	    repoRoot: string;
	    ref: string;
	    commitSha?: string;
	    prNumber?: number;
	
	    static createFrom(source: any = {}) {
	        return new SandboxRepositoryRef(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.repoRoot = source["repoRoot"];
	        this.ref = source["ref"];
	        this.commitSha = source["commitSha"];
	        this.prNumber = source["prNumber"];
	    }
	}
	export class SandboxServiceRule {
	    sourceNodeId: string;
	    mode: string;
	    dataMode?: string;
	    consistency?: string;
	
	    static createFrom(source: any = {}) {
	        return new SandboxServiceRule(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sourceNodeId = source["sourceNodeId"];
	        this.mode = source["mode"];
	        this.dataMode = source["dataMode"];
	        this.consistency = source["consistency"];
	    }
	}
	export class SandboxStep {
	    name?: string;
	    serviceLabel: string;
	    cmd: string[];
	    workDir?: string;
	
	    static createFrom(source: any = {}) {
	        return new SandboxStep(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.serviceLabel = source["serviceLabel"];
	        this.cmd = source["cmd"];
	        this.workDir = source["workDir"];
	    }
	}
	export class SandboxPlan {
	    ttlHours?: number;
	    warningHours?: number;
	    graceHours?: number;
	    suspendIdleHours?: number;
	    purpose?: string;
	    steps?: SandboxStep[];
	    onComplete?: string;
	    services?: SandboxServiceRule[];
	    repositories?: SandboxRepositoryRef[];
	
	    static createFrom(source: any = {}) {
	        return new SandboxPlan(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ttlHours = source["ttlHours"];
	        this.warningHours = source["warningHours"];
	        this.graceHours = source["graceHours"];
	        this.suspendIdleHours = source["suspendIdleHours"];
	        this.purpose = source["purpose"];
	        this.steps = this.convertValues(source["steps"], SandboxStep);
	        this.onComplete = source["onComplete"];
	        this.services = this.convertValues(source["services"], SandboxServiceRule);
	        this.repositories = this.convertValues(source["repositories"], SandboxRepositoryRef);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SandboxCreateRequest {
	    name: string;
	    sourceEnvironmentId: number;
	    profileId?: number;
	    plan: SandboxPlan;
	    links?: store.SandboxLink[];
	    startOnCreate?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SandboxCreateRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.sourceEnvironmentId = source["sourceEnvironmentId"];
	        this.profileId = source["profileId"];
	        this.plan = this.convertValues(source["plan"], SandboxPlan);
	        this.links = this.convertValues(source["links"], store.SandboxLink);
	        this.startOnCreate = source["startOnCreate"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SandboxCreateResult {
	    sandbox?: store.Sandbox;
	    stack?: EnvironmentStackResult;
	    started: boolean;
	    startError?: string;
	
	    static createFrom(source: any = {}) {
	        return new SandboxCreateResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sandbox = this.convertValues(source["sandbox"], store.Sandbox);
	        this.stack = this.convertValues(source["stack"], EnvironmentStackResult);
	        this.started = source["started"];
	        this.startError = source["startError"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SandboxDetail {
	    sandbox: store.Sandbox;
	    source: store.Environment;
	    links: store.SandboxLink[];
	    repositories: store.SandboxRepositorySource[];
	    plan: SandboxPlan;
	    latestRun?: store.SandboxTestRun;
	
	    static createFrom(source: any = {}) {
	        return new SandboxDetail(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sandbox = this.convertValues(source["sandbox"], store.Sandbox);
	        this.source = this.convertValues(source["source"], store.Environment);
	        this.links = this.convertValues(source["links"], store.SandboxLink);
	        this.repositories = this.convertValues(source["repositories"], store.SandboxRepositorySource);
	        this.plan = this.convertValues(source["plan"], SandboxPlan);
	        this.latestRun = this.convertValues(source["latestRun"], store.SandboxTestRun);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class SandboxPreview {
	    projectId: number;
	    sourceEnvironmentId: number;
	    profileId?: number;
	    plan: SandboxPlan;
	    repositories: store.SandboxRepositorySource[];
	    services: SandboxServiceRule[];
	    // Go type: time
	    expiresAt: any;
	    // Go type: time
	    warnAt: any;
	    // Go type: time
	    graceEndsAt: any;
	
	    static createFrom(source: any = {}) {
	        return new SandboxPreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectId = source["projectId"];
	        this.sourceEnvironmentId = source["sourceEnvironmentId"];
	        this.profileId = source["profileId"];
	        this.plan = this.convertValues(source["plan"], SandboxPlan);
	        this.repositories = this.convertValues(source["repositories"], store.SandboxRepositorySource);
	        this.services = this.convertValues(source["services"], SandboxServiceRule);
	        this.expiresAt = this.convertValues(source["expiresAt"], null);
	        this.warnAt = this.convertValues(source["warnAt"], null);
	        this.graceEndsAt = this.convertValues(source["graceEndsAt"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SandboxRefreshResult {
	    sandbox?: store.Sandbox;
	    repositories: store.SandboxRepositorySource[];
	    stack?: EnvironmentStackResult;
	
	    static createFrom(source: any = {}) {
	        return new SandboxRefreshResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sandbox = this.convertValues(source["sandbox"], store.Sandbox);
	        this.repositories = this.convertValues(source["repositories"], store.SandboxRepositorySource);
	        this.stack = this.convertValues(source["stack"], EnvironmentStackResult);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	export class SandboxSourceRepo {
	    repoRoot: string;
	    defaultRef: string;
	    serviceLabels: string[];
	    nodeIds: string[];
	    branches?: string[];
	    pullRequestsAvailable: boolean;
	    pullRequestsError?: string;
	    pullRequests?: gitsrc.PullRequest[];
	
	    static createFrom(source: any = {}) {
	        return new SandboxSourceRepo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.repoRoot = source["repoRoot"];
	        this.defaultRef = source["defaultRef"];
	        this.serviceLabels = source["serviceLabels"];
	        this.nodeIds = source["nodeIds"];
	        this.branches = source["branches"];
	        this.pullRequestsAvailable = source["pullRequestsAvailable"];
	        this.pullRequestsError = source["pullRequestsError"];
	        this.pullRequests = this.convertValues(source["pullRequests"], gitsrc.PullRequest);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SandboxSourceRepos {
	    projectId: number;
	    sourceEnvironmentId: number;
	    repositories: SandboxSourceRepo[];
	
	    static createFrom(source: any = {}) {
	        return new SandboxSourceRepos(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectId = source["projectId"];
	        this.sourceEnvironmentId = source["sourceEnvironmentId"];
	        this.repositories = this.convertValues(source["repositories"], SandboxSourceRepo);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class SandboxTestRunRequest {
	    name: string;
	    sourceEnvironmentId: number;
	    profileId?: number;
	    plan: SandboxPlan;
	    links?: store.SandboxLink[];
	    sandboxId?: number;
	    mode?: string;
	
	    static createFrom(source: any = {}) {
	        return new SandboxTestRunRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.sourceEnvironmentId = source["sourceEnvironmentId"];
	        this.profileId = source["profileId"];
	        this.plan = this.convertValues(source["plan"], SandboxPlan);
	        this.links = this.convertValues(source["links"], store.SandboxLink);
	        this.sandboxId = source["sandboxId"];
	        this.mode = source["mode"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SandboxTestStepResult {
	    name: string;
	    serviceLabel: string;
	    nodeId?: string;
	    exitCode: number;
	    output?: string;
	    error?: string;
	    durationMs: number;
	
	    static createFrom(source: any = {}) {
	        return new SandboxTestStepResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.serviceLabel = source["serviceLabel"];
	        this.nodeId = source["nodeId"];
	        this.exitCode = source["exitCode"];
	        this.output = source["output"];
	        this.error = source["error"];
	        this.durationMs = source["durationMs"];
	    }
	}
	export class SandboxTestRunResult {
	    run: store.SandboxTestRun;
	    sandbox?: store.Sandbox;
	    steps: SandboxTestStepResult[];
	    stack?: EnvironmentStackResult;
	
	    static createFrom(source: any = {}) {
	        return new SandboxTestRunResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.run = this.convertValues(source["run"], store.SandboxTestRun);
	        this.sandbox = this.convertValues(source["sandbox"], store.Sandbox);
	        this.steps = this.convertValues(source["steps"], SandboxTestStepResult);
	        this.stack = this.convertValues(source["stack"], EnvironmentStackResult);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class SecretUsage {
	    projectId: number;
	    projectName: string;
	    nodeId: string;
	    nodeLabel: string;
	    varKey: string;
	    isRunning: boolean;
	    overridden?: boolean;
	    receivesViaInjection?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SecretUsage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectId = source["projectId"];
	        this.projectName = source["projectName"];
	        this.nodeId = source["nodeId"];
	        this.nodeLabel = source["nodeLabel"];
	        this.varKey = source["varKey"];
	        this.isRunning = source["isRunning"];
	        this.overridden = source["overridden"];
	        this.receivesViaInjection = source["receivesViaInjection"];
	    }
	}
	export class ServiceDataChoice {
	    sourceNodeId: string;
	    mode: string;
	    consistency?: string;
	
	    static createFrom(source: any = {}) {
	        return new ServiceDataChoice(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sourceNodeId = source["sourceNodeId"];
	        this.mode = source["mode"];
	        this.consistency = source["consistency"];
	    }
	}
	export class ServiceMetrics {
	    nodeId: string;
	    serviceName: string;
	    serviceType: string;
	    status: string;
	    desiredPort: number;
	    hostPort: number;
	    hostname: string;
	    internalUrl: string;
	    publicUrl: string;
	    currentDeployment?: store.Deployment;
	    uptimeMs: number;
	    restartCount: number;
	    oomKilled: boolean;
	    exitCode?: number;
	    dockerHealth: string;
	    imageSizeBytes: number;
	    writableSizeBytes: number;
	    liveMetricsError: string;
	    latestPoint?: MetricPoint;
	    livePoints: MetricPoint[];
	    reachability: ReachabilityCheck;
	    deploymentSummary: DeploymentHistorySummary;
	    recentDeployments: DeploymentTimelineItem[];
	    events: RuntimeEvent[];
	
	    static createFrom(source: any = {}) {
	        return new ServiceMetrics(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.nodeId = source["nodeId"];
	        this.serviceName = source["serviceName"];
	        this.serviceType = source["serviceType"];
	        this.status = source["status"];
	        this.desiredPort = source["desiredPort"];
	        this.hostPort = source["hostPort"];
	        this.hostname = source["hostname"];
	        this.internalUrl = source["internalUrl"];
	        this.publicUrl = source["publicUrl"];
	        this.currentDeployment = this.convertValues(source["currentDeployment"], store.Deployment);
	        this.uptimeMs = source["uptimeMs"];
	        this.restartCount = source["restartCount"];
	        this.oomKilled = source["oomKilled"];
	        this.exitCode = source["exitCode"];
	        this.dockerHealth = source["dockerHealth"];
	        this.imageSizeBytes = source["imageSizeBytes"];
	        this.writableSizeBytes = source["writableSizeBytes"];
	        this.liveMetricsError = source["liveMetricsError"];
	        this.latestPoint = this.convertValues(source["latestPoint"], MetricPoint);
	        this.livePoints = this.convertValues(source["livePoints"], MetricPoint);
	        this.reachability = this.convertValues(source["reachability"], ReachabilityCheck);
	        this.deploymentSummary = this.convertValues(source["deploymentSummary"], DeploymentHistorySummary);
	        this.recentDeployments = this.convertValues(source["recentDeployments"], DeploymentTimelineItem);
	        this.events = this.convertValues(source["events"], RuntimeEvent);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class SettingsWarning {
	    code: string;
	    message: string;
	    field?: string;
	
	    static createFrom(source: any = {}) {
	        return new SettingsWarning(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.message = source["message"];
	        this.field = source["field"];
	    }
	}
	export class ShareTargetEnvironment {
	    environmentId: number;
	    envName: string;
	    envSlug: string;
	    matchedRoot?: RootServiceSummary;
	    roots: RootServiceSummary[];
	
	    static createFrom(source: any = {}) {
	        return new ShareTargetEnvironment(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.environmentId = source["environmentId"];
	        this.envName = source["envName"];
	        this.envSlug = source["envSlug"];
	        this.matchedRoot = this.convertValues(source["matchedRoot"], RootServiceSummary);
	        this.roots = this.convertValues(source["roots"], RootServiceSummary);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class StagedChangePreview {
	    warnings: SettingsWarning[];
	    errors: SettingsWarning[];
	
	    static createFrom(source: any = {}) {
	        return new StagedChangePreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.warnings = this.convertValues(source["warnings"], SettingsWarning);
	        this.errors = this.convertValues(source["errors"], SettingsWarning);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class StatefulServiceSummary {
	    nodeId: string;
	    label: string;
	    templateId: number;
	    volumes: string[];
	    warningKind: string;
	    warning: string;
	
	    static createFrom(source: any = {}) {
	        return new StatefulServiceSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.nodeId = source["nodeId"];
	        this.label = source["label"];
	        this.templateId = source["templateId"];
	        this.volumes = source["volumes"];
	        this.warningKind = source["warningKind"];
	        this.warning = source["warning"];
	    }
	}
	export class SyncApplyNodeResult {
	    nodeId: string;
	    label: string;
	    staged: boolean;
	    redeployed: boolean;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new SyncApplyNodeResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.nodeId = source["nodeId"];
	        this.label = source["label"];
	        this.staged = source["staged"];
	        this.redeployed = source["redeployed"];
	        this.error = source["error"];
	    }
	}
	export class SyncApplyResult {
	    mode: string;
	    actionableCount: number;
	    results: SyncApplyNodeResult[];
	
	    static createFrom(source: any = {}) {
	        return new SyncApplyResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.actionableCount = source["actionableCount"];
	        this.results = this.convertValues(source["results"], SyncApplyNodeResult);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SyncEnvDiff {
	    key: string;
	    sourceValue: string;
	    sourceScope: string;
	    sourceSource: string;
	    sourceSecret: boolean;
	    targetValue: string;
	    targetScope: string;
	    targetSource: string;
	    targetSecret: boolean;
	    proposedValue?: string;
	    proposedScope?: string;
	    action: string;
	    reason?: string;
	
	    static createFrom(source: any = {}) {
	        return new SyncEnvDiff(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.sourceValue = source["sourceValue"];
	        this.sourceScope = source["sourceScope"];
	        this.sourceSource = source["sourceSource"];
	        this.sourceSecret = source["sourceSecret"];
	        this.targetValue = source["targetValue"];
	        this.targetScope = source["targetScope"];
	        this.targetSource = source["targetSource"];
	        this.targetSecret = source["targetSecret"];
	        this.proposedValue = source["proposedValue"];
	        this.proposedScope = source["proposedScope"];
	        this.action = source["action"];
	        this.reason = source["reason"];
	    }
	}
	export class SyncSettingDiff {
	    key: string;
	    sourceValue: string;
	    targetApplied: string;
	    targetStaged?: string;
	    targetEffective: string;
	    action: string;
	    reason?: string;
	    warnings?: string[];
	
	    static createFrom(source: any = {}) {
	        return new SyncSettingDiff(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.sourceValue = source["sourceValue"];
	        this.targetApplied = source["targetApplied"];
	        this.targetStaged = source["targetStaged"];
	        this.targetEffective = source["targetEffective"];
	        this.action = source["action"];
	        this.reason = source["reason"];
	        this.warnings = source["warnings"];
	    }
	}
	export class SyncServicePreview {
	    label: string;
	    sourceNodeId: string;
	    targetNodeId: string;
	    skipped: boolean;
	    skipReason?: string;
	    settings: SyncSettingDiff[];
	    env: SyncEnvDiff[];
	    warnings: SettingsWarning[];
	    actionableCount: number;
	
	    static createFrom(source: any = {}) {
	        return new SyncServicePreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.label = source["label"];
	        this.sourceNodeId = source["sourceNodeId"];
	        this.targetNodeId = source["targetNodeId"];
	        this.skipped = source["skipped"];
	        this.skipReason = source["skipReason"];
	        this.settings = this.convertValues(source["settings"], SyncSettingDiff);
	        this.env = this.convertValues(source["env"], SyncEnvDiff);
	        this.warnings = this.convertValues(source["warnings"], SettingsWarning);
	        this.actionableCount = source["actionableCount"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SyncPreview {
	    scope: string;
	    sourceEnvironmentId: number;
	    targetEnvironmentId: number;
	    sourceEnvName: string;
	    targetEnvName: string;
	    includeSettings: boolean;
	    includeEnv: boolean;
	    services: SyncServicePreview[];
	    unmatchedSource: string[];
	    unmatchedTarget: string[];
	    actionableCount: number;
	
	    static createFrom(source: any = {}) {
	        return new SyncPreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.scope = source["scope"];
	        this.sourceEnvironmentId = source["sourceEnvironmentId"];
	        this.targetEnvironmentId = source["targetEnvironmentId"];
	        this.sourceEnvName = source["sourceEnvName"];
	        this.targetEnvName = source["targetEnvName"];
	        this.includeSettings = source["includeSettings"];
	        this.includeEnv = source["includeEnv"];
	        this.services = this.convertValues(source["services"], SyncServicePreview);
	        this.unmatchedSource = source["unmatchedSource"];
	        this.unmatchedTarget = source["unmatchedTarget"];
	        this.actionableCount = source["actionableCount"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SyncRequest {
	    scope: string;
	    sourceEnvironmentId: number;
	    targetEnvironmentId: number;
	    sourceNodeId?: string;
	    targetNodeId?: string;
	    includeSettings: boolean;
	    includeEnv: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SyncRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.scope = source["scope"];
	        this.sourceEnvironmentId = source["sourceEnvironmentId"];
	        this.targetEnvironmentId = source["targetEnvironmentId"];
	        this.sourceNodeId = source["sourceNodeId"];
	        this.targetNodeId = source["targetNodeId"];
	        this.includeSettings = source["includeSettings"];
	        this.includeEnv = source["includeEnv"];
	    }
	}
	
	
	export class VolumeOverview {
	    name: string;
	    labels: Record<string, string>;
	    mountpoint: string;
	    driver: string;
	    createdAt: string;
	    size: number;
	    refCount: number;
	    projectId: number;
	    nodeId: string;
	    target: string;
	    environment: string;
	    nodeLabel: string;
	    orphaned: boolean;
	    managed: boolean;
	
	    static createFrom(source: any = {}) {
	        return new VolumeOverview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.labels = source["labels"];
	        this.mountpoint = source["mountpoint"];
	        this.driver = source["driver"];
	        this.createdAt = source["createdAt"];
	        this.size = source["size"];
	        this.refCount = source["refCount"];
	        this.projectId = source["projectId"];
	        this.nodeId = source["nodeId"];
	        this.target = source["target"];
	        this.environment = source["environment"];
	        this.nodeLabel = source["nodeLabel"];
	        this.orphaned = source["orphaned"];
	        this.managed = source["managed"];
	    }
	}

}

export namespace dockerfile {
	
	export class ExposePort {
	    port: number;
	    protocol: string;
	
	    static createFrom(source: any = {}) {
	        return new ExposePort(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.port = source["port"];
	        this.protocol = source["protocol"];
	    }
	}

}

export namespace dockerwatch {
	
	export class DaemonStatus {
	    state: string;
	    apiVersion?: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new DaemonStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.state = source["state"];
	        this.apiVersion = source["apiVersion"];
	        this.error = source["error"];
	    }
	}

}

export namespace draftpack {
	
	export class AppSecretPayload {
	    key: string;
	    value?: string;
	    description?: string;
	    valueOmitted?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AppSecretPayload(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.value = source["value"];
	        this.description = source["description"];
	        this.valueOmitted = source["valueOmitted"];
	    }
	}
	export class BindNeed {
	    serviceKey: string;
	    label: string;
	    containerPath: string;
	    originalHost?: string;
	
	    static createFrom(source: any = {}) {
	        return new BindNeed(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.serviceKey = source["serviceKey"];
	        this.label = source["label"];
	        this.containerPath = source["containerPath"];
	        this.originalHost = source["originalHost"];
	    }
	}
	export class BindRemap {
	    containerPath: string;
	    originalHost?: string;
	    readOnly?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new BindRemap(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.containerPath = source["containerPath"];
	        this.originalHost = source["originalHost"];
	        this.readOnly = source["readOnly"];
	    }
	}
	export class Collision {
	    kind: string;
	    field: string;
	    label: string;
	    current: string;
	    suggested?: string;
	    message: string;
	    blocking: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Collision(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.field = source["field"];
	        this.label = source["label"];
	        this.current = source["current"];
	        this.suggested = source["suggested"];
	        this.message = source["message"];
	        this.blocking = source["blocking"];
	    }
	}
	export class EnvVarPayload {
	    key: string;
	    value?: string;
	    scope?: string;
	    secret?: boolean;
	    source?: string;
	    valueOmitted?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new EnvVarPayload(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.value = source["value"];
	        this.scope = source["scope"];
	        this.secret = source["secret"];
	        this.source = source["source"];
	        this.valueOmitted = source["valueOmitted"];
	    }
	}
	export class EnvironmentPayload {
	    key: string;
	    name: string;
	    slug: string;
	    isDefault: boolean;
	
	    static createFrom(source: any = {}) {
	        return new EnvironmentPayload(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.name = source["name"];
	        this.slug = source["slug"];
	        this.isDefault = source["isDefault"];
	    }
	}
	export class ExportOptions {
	    includeSecretValues: boolean;
	    includeAppSecrets: boolean;
	    includeBindHostPaths: boolean;
	    includeServiceRoots: boolean;
	    includeProjectEnvVars: boolean;
	    includeSandboxProfiles: boolean;
	    includeCanvasLayout: boolean;
	    includeGitSettings: boolean;
	    includeSourceConfig: boolean;
	    environmentIds?: number[];
	
	    static createFrom(source: any = {}) {
	        return new ExportOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.includeSecretValues = source["includeSecretValues"];
	        this.includeAppSecrets = source["includeAppSecrets"];
	        this.includeBindHostPaths = source["includeBindHostPaths"];
	        this.includeServiceRoots = source["includeServiceRoots"];
	        this.includeProjectEnvVars = source["includeProjectEnvVars"];
	        this.includeSandboxProfiles = source["includeSandboxProfiles"];
	        this.includeCanvasLayout = source["includeCanvasLayout"];
	        this.includeGitSettings = source["includeGitSettings"];
	        this.includeSourceConfig = source["includeSourceConfig"];
	        this.environmentIds = source["environmentIds"];
	    }
	}
	export class Note {
	    kind: string;
	    code: string;
	    field?: string;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new Note(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.code = source["code"];
	        this.field = source["field"];
	        this.message = source["message"];
	    }
	}
	export class Report {
	    notes: Note[];
	
	    static createFrom(source: any = {}) {
	        return new Report(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.notes = this.convertValues(source["notes"], Note);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SandboxProfilePayload {
	    name: string;
	    description?: string;
	    planJson: string;
	    isDefault?: boolean;
	    sourceEnvironmentKey?: string;
	
	    static createFrom(source: any = {}) {
	        return new SandboxProfilePayload(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	        this.planJson = source["planJson"];
	        this.isDefault = source["isDefault"];
	        this.sourceEnvironmentKey = source["sourceEnvironmentKey"];
	    }
	}
	export class ProjectVarPayload {
	    key: string;
	    value?: string;
	    secret?: boolean;
	    valueOmitted?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ProjectVarPayload(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.value = source["value"];
	        this.secret = source["secret"];
	        this.valueOmitted = source["valueOmitted"];
	    }
	}
	export class ServiceLinkPayload {
	    rootEnvironmentKey: string;
	    rootServiceLabel: string;
	
	    static createFrom(source: any = {}) {
	        return new ServiceLinkPayload(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.rootEnvironmentKey = source["rootEnvironmentKey"];
	        this.rootServiceLabel = source["rootServiceLabel"];
	    }
	}
	export class UserTemplatePayload {
	    name: string;
	    description?: string;
	    category?: string;
	    icon?: string;
	    color?: string;
	    mode?: string;
	    image?: string;
	    imageTags?: string;
	    port?: number;
	    dockerfile?: string;
	    cmdOverride?: string;
	    entrypoint?: string;
	    workingDir?: string;
	    envVars?: string;
	    labels?: string;
	    volumes?: string;
	    schema?: string;
	    defaultSettings?: string;
	
	    static createFrom(source: any = {}) {
	        return new UserTemplatePayload(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	        this.category = source["category"];
	        this.icon = source["icon"];
	        this.color = source["color"];
	        this.mode = source["mode"];
	        this.image = source["image"];
	        this.imageTags = source["imageTags"];
	        this.port = source["port"];
	        this.dockerfile = source["dockerfile"];
	        this.cmdOverride = source["cmdOverride"];
	        this.entrypoint = source["entrypoint"];
	        this.workingDir = source["workingDir"];
	        this.envVars = source["envVars"];
	        this.labels = source["labels"];
	        this.volumes = source["volumes"];
	        this.schema = source["schema"];
	        this.defaultSettings = source["defaultSettings"];
	    }
	}
	export class TemplateRef {
	    builtinName?: string;
	    user?: UserTemplatePayload;
	
	    static createFrom(source: any = {}) {
	        return new TemplateRef(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.builtinName = source["builtinName"];
	        this.user = this.convertValues(source["user"], UserTemplatePayload);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ServicePayload {
	    key: string;
	    environmentKey: string;
	    label: string;
	    x?: number;
	    y?: number;
	    template?: TemplateRef;
	    settings?: Record<string, string>;
	    env?: EnvVarPayload[];
	    serviceLink?: ServiceLinkPayload;
	    needsServiceRoot?: boolean;
	    serviceRootHint?: string;
	    bindRemaps?: BindRemap[];
	
	    static createFrom(source: any = {}) {
	        return new ServicePayload(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.environmentKey = source["environmentKey"];
	        this.label = source["label"];
	        this.x = source["x"];
	        this.y = source["y"];
	        this.template = this.convertValues(source["template"], TemplateRef);
	        this.settings = source["settings"];
	        this.env = this.convertValues(source["env"], EnvVarPayload);
	        this.serviceLink = this.convertValues(source["serviceLink"], ServiceLinkPayload);
	        this.needsServiceRoot = source["needsServiceRoot"];
	        this.serviceRootHint = source["serviceRootHint"];
	        this.bindRemaps = this.convertValues(source["bindRemaps"], BindRemap);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ProjectPayload {
	    name: string;
	    description?: string;
	    pathHint?: string;
	
	    static createFrom(source: any = {}) {
	        return new ProjectPayload(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	        this.pathHint = source["pathHint"];
	    }
	}
	export class Pack {
	    format: string;
	    version: number;
	    // Go type: time
	    exportedAt: any;
	    scope: string;
	    options: ExportOptions;
	    project?: ProjectPayload;
	    environments?: EnvironmentPayload[];
	    services?: ServicePayload[];
	    projectEnvVars?: ProjectVarPayload[];
	    sandboxProfiles?: SandboxProfilePayload[];
	    appSecrets?: AppSecretPayload[];
	    report: Report;
	
	    static createFrom(source: any = {}) {
	        return new Pack(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.format = source["format"];
	        this.version = source["version"];
	        this.exportedAt = this.convertValues(source["exportedAt"], null);
	        this.scope = source["scope"];
	        this.options = this.convertValues(source["options"], ExportOptions);
	        this.project = this.convertValues(source["project"], ProjectPayload);
	        this.environments = this.convertValues(source["environments"], EnvironmentPayload);
	        this.services = this.convertValues(source["services"], ServicePayload);
	        this.projectEnvVars = this.convertValues(source["projectEnvVars"], ProjectVarPayload);
	        this.sandboxProfiles = this.convertValues(source["sandboxProfiles"], SandboxProfilePayload);
	        this.appSecrets = this.convertValues(source["appSecrets"], AppSecretPayload);
	        this.report = this.convertValues(source["report"], Report);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ExportResult {
	    pack?: Pack;
	    json: string;
	    path?: string;
	    fileName?: string;
	    report: Report;
	
	    static createFrom(source: any = {}) {
	        return new ExportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.pack = this.convertValues(source["pack"], Pack);
	        this.json = source["json"];
	        this.path = source["path"];
	        this.fileName = source["fileName"];
	        this.report = this.convertValues(source["report"], Report);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ImportOptions {
	    mode: string;
	    projectName?: string;
	    projectPath?: string;
	    projectId?: number;
	    environmentId?: number;
	    serviceLabelOverrides?: Record<string, string>;
	    environmentNameOverrides?: Record<string, string>;
	    serviceRootOverrides?: Record<string, string>;
	    bindPathOverrides?: Record<string, string>;
	    hostPortOverrides?: Record<string, string>;
	    secretValues?: Record<string, string>;
	    appSecretValues?: Record<string, string>;
	    importAppSecrets: boolean;
	    startAfter: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ImportOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.projectName = source["projectName"];
	        this.projectPath = source["projectPath"];
	        this.projectId = source["projectId"];
	        this.environmentId = source["environmentId"];
	        this.serviceLabelOverrides = source["serviceLabelOverrides"];
	        this.environmentNameOverrides = source["environmentNameOverrides"];
	        this.serviceRootOverrides = source["serviceRootOverrides"];
	        this.bindPathOverrides = source["bindPathOverrides"];
	        this.hostPortOverrides = source["hostPortOverrides"];
	        this.secretValues = source["secretValues"];
	        this.appSecretValues = source["appSecretValues"];
	        this.importAppSecrets = source["importAppSecrets"];
	        this.startAfter = source["startAfter"];
	    }
	}
	export class ServiceRootNeed {
	    serviceKey: string;
	    label: string;
	    hint?: string;
	
	    static createFrom(source: any = {}) {
	        return new ServiceRootNeed(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.serviceKey = source["serviceKey"];
	        this.label = source["label"];
	        this.hint = source["hint"];
	    }
	}
	export class ServiceSummary {
	    key: string;
	    label: string;
	    environmentKey: string;
	    mode: string;
	    image?: string;
	    port?: string;
	    needsServiceRoot?: boolean;
	    bindRemapCount?: number;
	
	    static createFrom(source: any = {}) {
	        return new ServiceSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.label = source["label"];
	        this.environmentKey = source["environmentKey"];
	        this.mode = source["mode"];
	        this.image = source["image"];
	        this.port = source["port"];
	        this.needsServiceRoot = source["needsServiceRoot"];
	        this.bindRemapCount = source["bindRemapCount"];
	    }
	}
	export class ImportPreview {
	    packScope: string;
	    projectName: string;
	    suggestedProjectName?: string;
	    environments: EnvironmentPayload[];
	    services: ServiceSummary[];
	    projectEnvVarCount: number;
	    sandboxProfileCount: number;
	    appSecretCount: number;
	    needsProjectPath: boolean;
	    needsServiceRoots?: ServiceRootNeed[];
	    needsBinds?: BindNeed[];
	    needsSecrets?: string[];
	    needsAppSecrets?: string[];
	    collisions?: Collision[];
	    hasBlockingCollision: boolean;
	    report: Report;
	
	    static createFrom(source: any = {}) {
	        return new ImportPreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.packScope = source["packScope"];
	        this.projectName = source["projectName"];
	        this.suggestedProjectName = source["suggestedProjectName"];
	        this.environments = this.convertValues(source["environments"], EnvironmentPayload);
	        this.services = this.convertValues(source["services"], ServiceSummary);
	        this.projectEnvVarCount = source["projectEnvVarCount"];
	        this.sandboxProfileCount = source["sandboxProfileCount"];
	        this.appSecretCount = source["appSecretCount"];
	        this.needsProjectPath = source["needsProjectPath"];
	        this.needsServiceRoots = this.convertValues(source["needsServiceRoots"], ServiceRootNeed);
	        this.needsBinds = this.convertValues(source["needsBinds"], BindNeed);
	        this.needsSecrets = source["needsSecrets"];
	        this.needsAppSecrets = source["needsAppSecrets"];
	        this.collisions = this.convertValues(source["collisions"], Collision);
	        this.hasBlockingCollision = source["hasBlockingCollision"];
	        this.report = this.convertValues(source["report"], Report);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ImportResult {
	    projectId: number;
	    environmentIds?: number[];
	    nodeIds?: string[];
	    report: Report;
	
	    static createFrom(source: any = {}) {
	        return new ImportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectId = source["projectId"];
	        this.environmentIds = source["environmentIds"];
	        this.nodeIds = source["nodeIds"];
	        this.report = this.convertValues(source["report"], Report);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	export class PreviewOptions {
	    mode?: string;
	    projectId?: number;
	    environmentId?: number;
	    projectName?: string;
	    projectPath?: string;
	    serviceLabelOverrides?: Record<string, string>;
	    environmentNameOverrides?: Record<string, string>;
	    hostPortOverrides?: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new PreviewOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.projectId = source["projectId"];
	        this.environmentId = source["environmentId"];
	        this.projectName = source["projectName"];
	        this.projectPath = source["projectPath"];
	        this.serviceLabelOverrides = source["serviceLabelOverrides"];
	        this.environmentNameOverrides = source["environmentNameOverrides"];
	        this.hostPortOverrides = source["hostPortOverrides"];
	    }
	}
	
	
	
	
	
	
	
	
	

}

export namespace gitsrc {
	
	export class PullRequest {
	    number: number;
	    title: string;
	    headRef: string;
	    headSha?: string;
	    url?: string;
	    author?: string;
	
	    static createFrom(source: any = {}) {
	        return new PullRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.number = source["number"];
	        this.title = source["title"];
	        this.headRef = source["headRef"];
	        this.headSha = source["headSha"];
	        this.url = source["url"];
	        this.author = source["author"];
	    }
	}

}

export namespace image {
	
	export class AttestationProperties {
	    For: string;
	
	    static createFrom(source: any = {}) {
	        return new AttestationProperties(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.For = source["For"];
	    }
	}
	export class ImageProperties {
	    Platform: v1.Platform;
	    // Go type: struct { Unpacked int64 "json:\"Unpacked\"" }
	    Size: any;
	    Containers: string[];
	
	    static createFrom(source: any = {}) {
	        return new ImageProperties(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Platform = this.convertValues(source["Platform"], v1.Platform);
	        this.Size = this.convertValues(source["Size"], Object);
	        this.Containers = source["Containers"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ManifestSummary {
	    ID: string;
	    Descriptor: v1.Descriptor;
	    Available: boolean;
	    // Go type: struct { Content int64 "json:\"Content\""; Total int64 "json:\"Total\"" }
	    Size: any;
	    Kind: string;
	    ImageData?: ImageProperties;
	    AttestationData?: AttestationProperties;
	
	    static createFrom(source: any = {}) {
	        return new ManifestSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ID = source["ID"];
	        this.Descriptor = this.convertValues(source["Descriptor"], v1.Descriptor);
	        this.Available = source["Available"];
	        this.Size = this.convertValues(source["Size"], Object);
	        this.Kind = source["Kind"];
	        this.ImageData = this.convertValues(source["ImageData"], ImageProperties);
	        this.AttestationData = this.convertValues(source["AttestationData"], AttestationProperties);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Summary {
	    Containers: number;
	    Created: number;
	    Id: string;
	    Labels: Record<string, string>;
	    ParentId: string;
	    Descriptor?: v1.Descriptor;
	    Manifests?: ManifestSummary[];
	    RepoDigests: string[];
	    RepoTags: string[];
	    SharedSize: number;
	    Size: number;
	    VirtualSize?: number;
	
	    static createFrom(source: any = {}) {
	        return new Summary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Containers = source["Containers"];
	        this.Created = source["Created"];
	        this.Id = source["Id"];
	        this.Labels = source["Labels"];
	        this.ParentId = source["ParentId"];
	        this.Descriptor = this.convertValues(source["Descriptor"], v1.Descriptor);
	        this.Manifests = this.convertValues(source["Manifests"], ManifestSummary);
	        this.RepoDigests = source["RepoDigests"];
	        this.RepoTags = source["RepoTags"];
	        this.SharedSize = source["SharedSize"];
	        this.Size = source["Size"];
	        this.VirtualSize = source["VirtualSize"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace main {
	
	export class AppSettings {
	    compactSidebar: boolean;
	    localDomainPreference: string;
	    localDraftDomainEnabled: boolean;
	    proxyPortMode: string;
	    proxyPort: number;
	    proxyFallbackPort: number;
	
	    static createFrom(source: any = {}) {
	        return new AppSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.compactSidebar = source["compactSidebar"];
	        this.localDomainPreference = source["localDomainPreference"];
	        this.localDraftDomainEnabled = source["localDraftDomainEnabled"];
	        this.proxyPortMode = source["proxyPortMode"];
	        this.proxyPort = source["proxyPort"];
	        this.proxyFallbackPort = source["proxyFallbackPort"];
	    }
	}
	export class DaemonConnectionInfo {
	    addr: string;
	    token: string;
	
	    static createFrom(source: any = {}) {
	        return new DaemonConnectionInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.addr = source["addr"];
	        this.token = source["token"];
	    }
	}
	export class ProjectService {
	    id: string;
	    projectId: number;
	    environmentId: number;
	    environmentName: string;
	    name: string;
	    type: string;
	    image: string;
	    port: number;
	    status: string;
	    hostname: string;
	    hostPort: number;
	    dockerfile: string;
	    serviceRoot: string;
	    // Go type: time
	    updatedAt: any;
	
	    static createFrom(source: any = {}) {
	        return new ProjectService(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.projectId = source["projectId"];
	        this.environmentId = source["environmentId"];
	        this.environmentName = source["environmentName"];
	        this.name = source["name"];
	        this.type = source["type"];
	        this.image = source["image"];
	        this.port = source["port"];
	        this.status = source["status"];
	        this.hostname = source["hostname"];
	        this.hostPort = source["hostPort"];
	        this.dockerfile = source["dockerfile"];
	        this.serviceRoot = source["serviceRoot"];
	        this.updatedAt = this.convertValues(source["updatedAt"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class EnvironmentServices {
	    id: number;
	    name: string;
	    slug: string;
	    isDefault: boolean;
	    services: ProjectService[];
	    running: number;
	    stopped: number;
	    building: number;
	    failed: number;
	    status: string;
	    // Go type: time
	    lastActive?: any;
	
	    static createFrom(source: any = {}) {
	        return new EnvironmentServices(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.slug = source["slug"];
	        this.isDefault = source["isDefault"];
	        this.services = this.convertValues(source["services"], ProjectService);
	        this.running = source["running"];
	        this.stopped = source["stopped"];
	        this.building = source["building"];
	        this.failed = source["failed"];
	        this.status = source["status"];
	        this.lastActive = this.convertValues(source["lastActive"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class GitHookStatus {
	    supported: boolean;
	    commitForeign: boolean;
	    pushForeign: boolean;
	    pullForeign: boolean;
	
	    static createFrom(source: any = {}) {
	        return new GitHookStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.supported = source["supported"];
	        this.commitForeign = source["commitForeign"];
	        this.pushForeign = source["pushForeign"];
	        this.pullForeign = source["pullForeign"];
	    }
	}
	
	export class ProjectServicesSummary {
	    projectId: number;
	    status: string;
	    environments: EnvironmentServices[];
	    services: ProjectService[];
	    // Go type: time
	    lastActive?: any;
	
	    static createFrom(source: any = {}) {
	        return new ProjectServicesSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectId = source["projectId"];
	        this.status = source["status"];
	        this.environments = this.convertValues(source["environments"], EnvironmentServices);
	        this.services = this.convertValues(source["services"], ProjectService);
	        this.lastActive = this.convertValues(source["lastActive"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class RouteRow {
	    hostname: string;
	    projectId: number;
	    nodeId: string;
	    environment: string;
	    protocol: string;
	    targetHost: string;
	    targetPort: number;
	    hostPort: number;
	    projectName: string;
	    serviceName: string;
	
	    static createFrom(source: any = {}) {
	        return new RouteRow(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hostname = source["hostname"];
	        this.projectId = source["projectId"];
	        this.nodeId = source["nodeId"];
	        this.environment = source["environment"];
	        this.protocol = source["protocol"];
	        this.targetHost = source["targetHost"];
	        this.targetPort = source["targetPort"];
	        this.hostPort = source["hostPort"];
	        this.projectName = source["projectName"];
	        this.serviceName = source["serviceName"];
	    }
	}

}

export namespace network {
	
	export class EndpointIPAMConfig {
	
	
	    static createFrom(source: any = {}) {
	        return new EndpointIPAMConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	export class EndpointSettings {
	    // Go type: EndpointIPAMConfig
	    IPAMConfig?: any;
	    Links: string[];
	    Aliases: string[];
	    MacAddress: string;
	    DriverOpts: Record<string, string>;
	    GwPriority: number;
	    NetworkID: string;
	    EndpointID: string;
	    Gateway: string;
	    IPAddress: string;
	    IPPrefixLen: number;
	    IPv6Gateway: string;
	    GlobalIPv6Address: string;
	    GlobalIPv6PrefixLen: number;
	    DNSNames: string[];
	
	    static createFrom(source: any = {}) {
	        return new EndpointSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.IPAMConfig = this.convertValues(source["IPAMConfig"], null);
	        this.Links = source["Links"];
	        this.Aliases = source["Aliases"];
	        this.MacAddress = source["MacAddress"];
	        this.DriverOpts = source["DriverOpts"];
	        this.GwPriority = source["GwPriority"];
	        this.NetworkID = source["NetworkID"];
	        this.EndpointID = source["EndpointID"];
	        this.Gateway = source["Gateway"];
	        this.IPAddress = source["IPAddress"];
	        this.IPPrefixLen = source["IPPrefixLen"];
	        this.IPv6Gateway = source["IPv6Gateway"];
	        this.GlobalIPv6Address = source["GlobalIPv6Address"];
	        this.GlobalIPv6PrefixLen = source["GlobalIPv6PrefixLen"];
	        this.DNSNames = source["DNSNames"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace networking {
	
	export class LocalDomainStatus {
	    proxyAddr: string;
	    proxyPort: number;
	    proxyOnDefault: boolean;
	    hostsConfigured: boolean;
	    hostsError: string;
	    mode: string;
	    publicSuffix: string;
	    loopbackSuffix: string;
	    draftEnabled: boolean;
	    resolverInstalled: boolean;
	    dnsListening: boolean;
	    dnsVerified: boolean;
	    dnsAddr: string;
	    dnsError: string;
	
	    static createFrom(source: any = {}) {
	        return new LocalDomainStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.proxyAddr = source["proxyAddr"];
	        this.proxyPort = source["proxyPort"];
	        this.proxyOnDefault = source["proxyOnDefault"];
	        this.hostsConfigured = source["hostsConfigured"];
	        this.hostsError = source["hostsError"];
	        this.mode = source["mode"];
	        this.publicSuffix = source["publicSuffix"];
	        this.loopbackSuffix = source["loopbackSuffix"];
	        this.draftEnabled = source["draftEnabled"];
	        this.resolverInstalled = source["resolverInstalled"];
	        this.dnsListening = source["dnsListening"];
	        this.dnsVerified = source["dnsVerified"];
	        this.dnsAddr = source["dnsAddr"];
	        this.dnsError = source["dnsError"];
	    }
	}

}

export namespace store {
	
	export class AppSecret {
	    key: string;
	    value: string;
	    description: string;
	    // Go type: time
	    createdAt: any;
	    // Go type: time
	    updatedAt: any;
	
	    static createFrom(source: any = {}) {
	        return new AppSecret(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.value = source["value"];
	        this.description = source["description"];
	        this.createdAt = this.convertValues(source["createdAt"], null);
	        this.updatedAt = this.convertValues(source["updatedAt"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CanvasNode {
	    id: string;
	    projectId: number;
	    environmentId: number;
	    label: string;
	    x: number;
	    y: number;
	    uid: string;
	    templateId: number;
	    // Go type: time
	    createdAt: any;
	    // Go type: time
	    updatedAt: any;
	
	    static createFrom(source: any = {}) {
	        return new CanvasNode(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.projectId = source["projectId"];
	        this.environmentId = source["environmentId"];
	        this.label = source["label"];
	        this.x = source["x"];
	        this.y = source["y"];
	        this.uid = source["uid"];
	        this.templateId = source["templateId"];
	        this.createdAt = this.convertValues(source["createdAt"], null);
	        this.updatedAt = this.convertValues(source["updatedAt"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Deployment {
	    id: number;
	    nodeId: string;
	    projectId: number;
	    imageTag: string;
	    containerId: string;
	    sourceSha: string;
	    status: string;
	    sequence: number;
	    hostname: string;
	    hostPort: number;
	    error: string;
	    jobId: string;
	    workerPid: number;
	    // Go type: time
	    startedAt?: any;
	    // Go type: time
	    buildStartedAt?: any;
	    // Go type: time
	    buildFinishedAt?: any;
	    // Go type: time
	    containerStartedAt?: any;
	    // Go type: time
	    containerStoppedAt?: any;
	    exitCode?: number;
	    oomKilled: boolean;
	    // Go type: time
	    lastSeenAt?: any;
	    // Go type: time
	    createdAt: any;
	    // Go type: time
	    updatedAt: any;
	    // Go type: time
	    finishedAt?: any;
	
	    static createFrom(source: any = {}) {
	        return new Deployment(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.nodeId = source["nodeId"];
	        this.projectId = source["projectId"];
	        this.imageTag = source["imageTag"];
	        this.containerId = source["containerId"];
	        this.sourceSha = source["sourceSha"];
	        this.status = source["status"];
	        this.sequence = source["sequence"];
	        this.hostname = source["hostname"];
	        this.hostPort = source["hostPort"];
	        this.error = source["error"];
	        this.jobId = source["jobId"];
	        this.workerPid = source["workerPid"];
	        this.startedAt = this.convertValues(source["startedAt"], null);
	        this.buildStartedAt = this.convertValues(source["buildStartedAt"], null);
	        this.buildFinishedAt = this.convertValues(source["buildFinishedAt"], null);
	        this.containerStartedAt = this.convertValues(source["containerStartedAt"], null);
	        this.containerStoppedAt = this.convertValues(source["containerStoppedAt"], null);
	        this.exitCode = source["exitCode"];
	        this.oomKilled = source["oomKilled"];
	        this.lastSeenAt = this.convertValues(source["lastSeenAt"], null);
	        this.createdAt = this.convertValues(source["createdAt"], null);
	        this.updatedAt = this.convertValues(source["updatedAt"], null);
	        this.finishedAt = this.convertValues(source["finishedAt"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class EnvVarConflict {
	    key: string;
	    databaseValue: string;
	    fileValue: string;
	
	    static createFrom(source: any = {}) {
	        return new EnvVarConflict(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.databaseValue = source["databaseValue"];
	        this.fileValue = source["fileValue"];
	    }
	}
	export class EnvFileSyncResult {
	    path: string;
	    imported: number;
	    updated: number;
	    unchanged: number;
	    skipped: number;
	    exported: number;
	    conflicts: EnvVarConflict[];
	
	    static createFrom(source: any = {}) {
	        return new EnvFileSyncResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.imported = source["imported"];
	        this.updated = source["updated"];
	        this.unchanged = source["unchanged"];
	        this.skipped = source["skipped"];
	        this.exported = source["exported"];
	        this.conflicts = this.convertValues(source["conflicts"], EnvVarConflict);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class EnvVar {
	    nodeId: string;
	    key: string;
	    value: string;
	    scope: string;
	    secret: boolean;
	    source: string;
	    envFile: string;
	    // Go type: time
	    createdAt: any;
	    // Go type: time
	    updatedAt: any;
	
	    static createFrom(source: any = {}) {
	        return new EnvVar(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.nodeId = source["nodeId"];
	        this.key = source["key"];
	        this.value = source["value"];
	        this.scope = source["scope"];
	        this.secret = source["secret"];
	        this.source = source["source"];
	        this.envFile = source["envFile"];
	        this.createdAt = this.convertValues(source["createdAt"], null);
	        this.updatedAt = this.convertValues(source["updatedAt"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class EnvVarStageUpsert {
	    key: string;
	    value: string;
	    scope: string;
	    source: string;
	    envFile: string;
	
	    static createFrom(source: any = {}) {
	        return new EnvVarStageUpsert(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.value = source["value"];
	        this.scope = source["scope"];
	        this.source = source["source"];
	        this.envFile = source["envFile"];
	    }
	}
	export class Environment {
	    id: number;
	    projectId: number;
	    name: string;
	    slug: string;
	    isDefault: boolean;
	    // Go type: time
	    createdAt: any;
	    // Go type: time
	    updatedAt: any;
	
	    static createFrom(source: any = {}) {
	        return new Environment(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.projectId = source["projectId"];
	        this.name = source["name"];
	        this.slug = source["slug"];
	        this.isDefault = source["isDefault"];
	        this.createdAt = this.convertValues(source["createdAt"], null);
	        this.updatedAt = this.convertValues(source["updatedAt"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Project {
	    id: number;
	    name: string;
	    path: string;
	    description: string;
	    // Go type: time
	    createdAt: any;
	    // Go type: time
	    updatedAt: any;
	
	    static createFrom(source: any = {}) {
	        return new Project(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.path = source["path"];
	        this.description = source["description"];
	        this.createdAt = this.convertValues(source["createdAt"], null);
	        this.updatedAt = this.convertValues(source["updatedAt"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ProjectEnvVar {
	    projectId: number;
	    key: string;
	    value: string;
	    scope: string;
	    secret: boolean;
	    // Go type: time
	    createdAt: any;
	    // Go type: time
	    updatedAt: any;
	
	    static createFrom(source: any = {}) {
	        return new ProjectEnvVar(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectId = source["projectId"];
	        this.key = source["key"];
	        this.value = source["value"];
	        this.scope = source["scope"];
	        this.secret = source["secret"];
	        this.createdAt = this.convertValues(source["createdAt"], null);
	        this.updatedAt = this.convertValues(source["updatedAt"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Sandbox {
	    id: number;
	    projectId: number;
	    environmentId: number;
	    sourceEnvironmentId: number;
	    profileId: number;
	    name: string;
	    purpose: string;
	    status: string;
	    planJson: string;
	    // Go type: time
	    expiresAt: any;
	    // Go type: time
	    warnAt: any;
	    // Go type: time
	    graceEndsAt: any;
	    // Go type: time
	    suspendedAt?: any;
	    // Go type: time
	    createdAt: any;
	    // Go type: time
	    updatedAt: any;
	
	    static createFrom(source: any = {}) {
	        return new Sandbox(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.projectId = source["projectId"];
	        this.environmentId = source["environmentId"];
	        this.sourceEnvironmentId = source["sourceEnvironmentId"];
	        this.profileId = source["profileId"];
	        this.name = source["name"];
	        this.purpose = source["purpose"];
	        this.status = source["status"];
	        this.planJson = source["planJson"];
	        this.expiresAt = this.convertValues(source["expiresAt"], null);
	        this.warnAt = this.convertValues(source["warnAt"], null);
	        this.graceEndsAt = this.convertValues(source["graceEndsAt"], null);
	        this.suspendedAt = this.convertValues(source["suspendedAt"], null);
	        this.createdAt = this.convertValues(source["createdAt"], null);
	        this.updatedAt = this.convertValues(source["updatedAt"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SandboxLink {
	    id: number;
	    sandboxId: number;
	    kind: string;
	    value: string;
	    label: string;
	    // Go type: time
	    createdAt: any;
	
	    static createFrom(source: any = {}) {
	        return new SandboxLink(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.sandboxId = source["sandboxId"];
	        this.kind = source["kind"];
	        this.value = source["value"];
	        this.label = source["label"];
	        this.createdAt = this.convertValues(source["createdAt"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SandboxProfile {
	    id: number;
	    projectId: number;
	    sourceEnvironmentId: number;
	    name: string;
	    description: string;
	    planJson: string;
	    isDefault: boolean;
	    // Go type: time
	    createdAt: any;
	    // Go type: time
	    updatedAt: any;
	
	    static createFrom(source: any = {}) {
	        return new SandboxProfile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.projectId = source["projectId"];
	        this.sourceEnvironmentId = source["sourceEnvironmentId"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.planJson = source["planJson"];
	        this.isDefault = source["isDefault"];
	        this.createdAt = this.convertValues(source["createdAt"], null);
	        this.updatedAt = this.convertValues(source["updatedAt"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SandboxProjectSettings {
	    projectId: number;
	    defaultTtlHours: number;
	    warningHours: number;
	    graceHours: number;
	    suspendIdleHours: number;
	    // Go type: time
	    createdAt: any;
	    // Go type: time
	    updatedAt: any;
	
	    static createFrom(source: any = {}) {
	        return new SandboxProjectSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectId = source["projectId"];
	        this.defaultTtlHours = source["defaultTtlHours"];
	        this.warningHours = source["warningHours"];
	        this.graceHours = source["graceHours"];
	        this.suspendIdleHours = source["suspendIdleHours"];
	        this.createdAt = this.convertValues(source["createdAt"], null);
	        this.updatedAt = this.convertValues(source["updatedAt"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SandboxRepositorySource {
	    id: number;
	    sandboxId: number;
	    repoRoot: string;
	    ref: string;
	    commitSha: string;
	    // Go type: time
	    createdAt: any;
	
	    static createFrom(source: any = {}) {
	        return new SandboxRepositorySource(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.sandboxId = source["sandboxId"];
	        this.repoRoot = source["repoRoot"];
	        this.ref = source["ref"];
	        this.commitSha = source["commitSha"];
	        this.createdAt = this.convertValues(source["createdAt"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SandboxTestRun {
	    id: number;
	    projectId: number;
	    profileId: number;
	    sandboxId: number;
	    sourceEnvironmentId: number;
	    name: string;
	    mode: string;
	    status: string;
	    planJson: string;
	    stepsJson: string;
	    error?: string;
	    // Go type: time
	    startedAt: any;
	    // Go type: time
	    finishedAt?: any;
	    // Go type: time
	    createdAt: any;
	    // Go type: time
	    updatedAt: any;
	
	    static createFrom(source: any = {}) {
	        return new SandboxTestRun(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.projectId = source["projectId"];
	        this.profileId = source["profileId"];
	        this.sandboxId = source["sandboxId"];
	        this.sourceEnvironmentId = source["sourceEnvironmentId"];
	        this.name = source["name"];
	        this.mode = source["mode"];
	        this.status = source["status"];
	        this.planJson = source["planJson"];
	        this.stepsJson = source["stepsJson"];
	        this.error = source["error"];
	        this.startedAt = this.convertValues(source["startedAt"], null);
	        this.finishedAt = this.convertValues(source["finishedAt"], null);
	        this.createdAt = this.convertValues(source["createdAt"], null);
	        this.updatedAt = this.convertValues(source["updatedAt"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ServiceTemplate {
	    id: number;
	    name: string;
	    description: string;
	    category: string;
	    icon: string;
	    color: string;
	    mode: string;
	    image: string;
	    imageTags: string;
	    port: number;
	    dockerfile: string;
	    cmdOverride: string;
	    entrypoint: string;
	    workingDir: string;
	    envVars: string;
	    labels: string;
	    volumes: string;
	    schema: string;
	    defaultSettings: string;
	    builtin: boolean;
	    // Go type: time
	    createdAt: any;
	    // Go type: time
	    updatedAt: any;
	
	    static createFrom(source: any = {}) {
	        return new ServiceTemplate(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.category = source["category"];
	        this.icon = source["icon"];
	        this.color = source["color"];
	        this.mode = source["mode"];
	        this.image = source["image"];
	        this.imageTags = source["imageTags"];
	        this.port = source["port"];
	        this.dockerfile = source["dockerfile"];
	        this.cmdOverride = source["cmdOverride"];
	        this.entrypoint = source["entrypoint"];
	        this.workingDir = source["workingDir"];
	        this.envVars = source["envVars"];
	        this.labels = source["labels"];
	        this.volumes = source["volumes"];
	        this.schema = source["schema"];
	        this.defaultSettings = source["defaultSettings"];
	        this.builtin = source["builtin"];
	        this.createdAt = this.convertValues(source["createdAt"], null);
	        this.updatedAt = this.convertValues(source["updatedAt"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace types {
	
	export class DiskUsage {
	    LayersSize: number;
	    Images: image.Summary[];
	    Containers: container.Summary[];
	    Volumes: volume.Volume[];
	    BuildCache: build.CacheRecord[];
	
	    static createFrom(source: any = {}) {
	        return new DiskUsage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.LayersSize = source["LayersSize"];
	        this.Images = this.convertValues(source["Images"], image.Summary);
	        this.Containers = this.convertValues(source["Containers"], container.Summary);
	        this.Volumes = this.convertValues(source["Volumes"], volume.Volume);
	        this.BuildCache = this.convertValues(source["BuildCache"], build.CacheRecord);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace v1 {
	
	export class Platform {
	    architecture: string;
	    os: string;
	    "os.version"?: string;
	    "os.features"?: string[];
	    variant?: string;
	
	    static createFrom(source: any = {}) {
	        return new Platform(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.architecture = source["architecture"];
	        this.os = source["os"];
	        this["os.version"] = source["os.version"];
	        this["os.features"] = source["os.features"];
	        this.variant = source["variant"];
	    }
	}
	export class Descriptor {
	    mediaType: string;
	    digest: string;
	    size: number;
	    urls?: string[];
	    annotations?: Record<string, string>;
	    data?: number[];
	    platform?: Platform;
	    artifactType?: string;
	
	    static createFrom(source: any = {}) {
	        return new Descriptor(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mediaType = source["mediaType"];
	        this.digest = source["digest"];
	        this.size = source["size"];
	        this.urls = source["urls"];
	        this.annotations = source["annotations"];
	        this.data = source["data"];
	        this.platform = this.convertValues(source["platform"], Platform);
	        this.artifactType = source["artifactType"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace volume {
	
	export class Info {
	
	
	    static createFrom(source: any = {}) {
	        return new Info(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	export class CapacityRange {
	    RequiredBytes: number;
	    LimitBytes: number;
	
	    static createFrom(source: any = {}) {
	        return new CapacityRange(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.RequiredBytes = source["RequiredBytes"];
	        this.LimitBytes = source["LimitBytes"];
	    }
	}
	export class TopologyRequirement {
	
	
	    static createFrom(source: any = {}) {
	        return new TopologyRequirement(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	export class TypeBlock {
	
	
	    static createFrom(source: any = {}) {
	        return new TypeBlock(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	export class TypeMount {
	
	
	    static createFrom(source: any = {}) {
	        return new TypeMount(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	
	    }
	}
	export class AccessMode {
	    // Go type: TypeMount
	    ""?: any;
	    // Go type: TypeBlock
	    ""?: any;
	
	    static createFrom(source: any = {}) {
	        return new AccessMode(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this[""] = this.convertValues(source[""], null);
	        this[""] = this.convertValues(source[""], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ClusterVolumeSpec {
	    // Go type: AccessMode
	    ""?: any;
	    // Go type: TopologyRequirement
	    ""?: any;
	    // Go type: CapacityRange
	    ""?: any;
	
	    static createFrom(source: any = {}) {
	        return new ClusterVolumeSpec(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this[""] = this.convertValues(source[""], null);
	        this[""] = this.convertValues(source[""], null);
	        this[""] = this.convertValues(source[""], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ClusterVolume {
	    ID: string;
	    // Go type: ClusterVolumeSpec
	    Spec: any;
	    // Go type: Info
	    ""?: any;
	
	    static createFrom(source: any = {}) {
	        return new ClusterVolume(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ID = source["ID"];
	        this.Spec = this.convertValues(source["Spec"], null);
	        this[""] = this.convertValues(source[""], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class UsageData {
	    RefCount: number;
	    Size: number;
	
	    static createFrom(source: any = {}) {
	        return new UsageData(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.RefCount = source["RefCount"];
	        this.Size = source["Size"];
	    }
	}
	export class Volume {
	    ClusterVolume?: ClusterVolume;
	    CreatedAt?: string;
	    Driver: string;
	    Labels: Record<string, string>;
	    Mountpoint: string;
	    Name: string;
	    Options: Record<string, string>;
	    Scope: string;
	    Status?: Record<string, any>;
	    UsageData?: UsageData;
	
	    static createFrom(source: any = {}) {
	        return new Volume(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ClusterVolume = this.convertValues(source["ClusterVolume"], ClusterVolume);
	        this.CreatedAt = source["CreatedAt"];
	        this.Driver = source["Driver"];
	        this.Labels = source["Labels"];
	        this.Mountpoint = source["Mountpoint"];
	        this.Name = source["Name"];
	        this.Options = source["Options"];
	        this.Scope = source["Scope"];
	        this.Status = source["Status"];
	        this.UsageData = this.convertValues(source["UsageData"], UsageData);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

