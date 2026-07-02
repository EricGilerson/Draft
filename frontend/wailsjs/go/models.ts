export namespace deploy {
	
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

export namespace main {
	
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
	export class ProjectService {
	    id: string;
	    projectId: number;
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
	    }
	}

}

export namespace store {
	
	export class CanvasNode {
	    id: string;
	    projectId: number;
	    label: string;
	    x: number;
	    y: number;
	    uid: string;
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
	        this.label = source["label"];
	        this.x = source["x"];
	        this.y = source["y"];
	        this.uid = source["uid"];
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

}

