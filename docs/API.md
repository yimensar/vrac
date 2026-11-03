# API contract

All note routes are authenticated and share the store used by MCP. JSON is camelCase. Errors are `{ "error": "..." }` with an appropriate status. Note state is `active`, `archived`, or `deleted`.

- `GET /api/session`: `{authenticated:boolean, needsSetup:boolean, user:{name:string}, mcp:{url:string,oauth:boolean}}` (user only if authenticated).
- `POST /api/setup`: `{password:string}` creates owner on first run, sets session.
- `POST /api/login`: `{password:string}` sets session; `POST /api/logout` clears it.
- `GET /api/notes?q=&tag=&state=active&limit=50&cursor=&sort=recent`: `{notes:Note[],total:number,nextCursor?:string,tookMs:number,mode:string}`. Sort `recent` or `oldest`; a search ranks relevance.
- `GET /api/notes/{id}`: `Note`.
- `POST /api/notes`: `{content:string,pinned?:boolean,source?:string,idempotencyKey?:string}` -> `Note` (201).
- `PATCH /api/notes/{id}`: `{content?:string,pinned?:boolean,state?:string,version:number}` -> `Note`; 409 if outdated version.
- `GET /api/notes/{id}/history`: `Note[]` newest first.
- `GET /api/tags`: `Tag[]` (`{name,count}`).
- `GET /api/stats`: `{active,archived,tags,attachments}`.
- `GET /api/export`: download complete JSON export, including metadata. Client Markdown export may use currently loaded note.
- `GET /api/attachments/{id}`: authenticated attachment content, content-disposition download.
- `GET /healthz`: health only, no personal data.
- `POST /mcp`: OAuth protected Streamable HTTP. Discovery and OAuth routes are supplied by auth implementation.

`Note`: `{id,content,title,tags:string[],createdAt,updatedAt,state,pinned,version,source,sourceId?,attachments:Attachment[],relations:Relation[],snippet?,matchType?}`. Timestamps are RFC3339. Attachment: `{id,filename,type,size,url}`. Relation: `{targetId,type}`.

Keyboard: Cmd/Ctrl+K searches, N creates when not editing, Esc dismisses, arrows navigate search results. The outline view collapses and focuses individual notes without requiring folders.
