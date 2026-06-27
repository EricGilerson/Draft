export namespace main {
	
	export class DockerStatus {
	    state: string;
	    apiVersion?: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new DockerStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.state = source["state"];
	        this.apiVersion = source["apiVersion"];
	        this.error = source["error"];
	    }
	}

}

