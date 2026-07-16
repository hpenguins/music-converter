export namespace main {
	
	export class AppSettings {
	    saveCoverFile: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AppSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.saveCoverFile = source["saveCoverFile"];
	    }
	}
	export class DecryptStatus {
	    fileName: string;
	    filePath: string;
	    status: string;
	    progress: number;
	    output?: string;
	    error?: string;
	    title?: string;
	    artist?: string;
	    album?: string;
	    format?: string;
	    coverPath?: string;
	
	    static createFrom(source: any = {}) {
	        return new DecryptStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fileName = source["fileName"];
	        this.filePath = source["filePath"];
	        this.status = source["status"];
	        this.progress = source["progress"];
	        this.output = source["output"];
	        this.error = source["error"];
	        this.title = source["title"];
	        this.artist = source["artist"];
	        this.album = source["album"];
	        this.format = source["format"];
	        this.coverPath = source["coverPath"];
	    }
	}
	export class FileConvertRequest {
	    path: string;
	    format: string;
	
	    static createFrom(source: any = {}) {
	        return new FileConvertRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.format = source["format"];
	    }
	}
	export class FileInfo {
	    path: string;
	    name: string;
	    size: number;
	    selected: boolean;
	
	    static createFrom(source: any = {}) {
	        return new FileInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.name = source["name"];
	        this.size = source["size"];
	        this.selected = source["selected"];
	    }
	}

}

