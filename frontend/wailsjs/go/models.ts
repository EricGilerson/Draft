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
	    status: string;
	    hostname: string;
	    hostPort: number;
	    error: string;
	    jobId: string;
	    workerPid: number;
	    // Go type: time
	    startedAt?: any;
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
	        this.status = source["status"];
	        this.hostname = source["hostname"];
	        this.hostPort = source["hostPort"];
	        this.error = source["error"];
	        this.jobId = source["jobId"];
	        this.workerPid = source["workerPid"];
	        this.startedAt = this.convertValues(source["startedAt"], null);
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

