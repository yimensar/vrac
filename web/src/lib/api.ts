// Typed client of the Vrac HTTP API. The interface holds no business rules:
// everything goes through these calls.

export type Node = {
  id: string
  kind: "node" | "journal" | "day"
  parent_id?: string
  text: string
  types: string[]
  /** The checkbox: absent, open or done. Any node may have one. */
  status?: "open" | "done"
  /** A day or a day and time, with an optional end. Any node may have one. */
  date?: string
  end?: string
  aliases: string[]
  day: string
  revision: number
  created_at: string
  updated_at: string
  created_by: string
  updated_by: string
  source: string
  child_count: number
  trashed?: boolean
  ref_ids: string[]
}

/** name is plain; label keeps the #tags written inside the sentence. */
export type Ref = { id: string; name: string; label: string; types: string[]; trashed?: boolean }
export type Refs = Record<string, Ref>
/** subjects, when asked for, lists the tagged nodes each result concerns, most specific first. */
export type Page = { nodes: Node[]; refs: Refs; subjects?: Record<string, string[]>; next_cursor?: string }
export type Brief = { id: string; name: string; label?: string; types: string[]; path?: string[] }
/** A tag: a name, a color, and whether its nodes are to-dos. */
export type Type = { name: string; color: string; todo: boolean; count: number }
export type Creation = { name: string; types?: string[] }
export type Unresolved = { kind: "type" | "node"; name: string; candidates: Brief[] }
/** One list on a node's page: what concerns it from elsewhere, by tag ("" for untagged). */
export type ListCount = { type: string; count: number }

export type Session = { owner: boolean; authenticated: boolean; today: string; timezone: string; version: string; mcp_url: string; provider: string }
export type Ceremony = { flow: string; mode: "create" | "get"; options: { publicKey: Record<string, any> } }
export type Passkey = { id: string; name: string; created_at: string; last_used_at?: string }
export type DayView = { day: string; exists: boolean; node?: Node; children: Page }
export type NodeView = { node: Node; path: Brief[]; children: Page; refs: Refs }
export type Activity = { node: Brief; revision: number; action: string; at: string; actor: string; source: string; latest: boolean }
export type Connection = { id: string; kind: "token" | "client"; name: string; created_at: string; last_used_at?: string }
export type Result = { nodes: Node[]; refs: Refs; created?: Brief[] }
export type Fold = "default" | "expanded" | "collapsed"
export type Scale = "small" | "default" | "large" | "larger"
export type Preferences = { fold: Fold; theme: "system" | "light" | "dark"; scale: Scale }

/** The preferences in effect, kept current by the app (see preferences.ts). */
export const current: Preferences = { fold: "default", theme: "system", scale: "default" }

export type Query = {
  text?: string
  types?: string[]
  untagged?: boolean
  concerns?: string[]
  /** Drops the subjects' own outlines: what refers to them from elsewhere. */
  outside?: boolean
  mentions?: string
  status?: "open" | "done"
  from?: string
  to?: string
  dated?: boolean
  with_subjects?: boolean
  sort?: "date_desc" | "date_asc" | "updated"
  limit?: number
  cursor?: string
}

export type Placement = { parent_id?: string; day?: string; root?: boolean; after_id?: string; first?: boolean }

/** The properties of a node besides its text; date_text is a phrase such
 * as "Friday 14h", and an empty string clears a field. */
export type Fields = { status?: "" | "open" | "done"; date?: string; end?: string; date_text?: string }
export type Draft = Fields & { input: string }
export type Change = Fields & { input?: string; aliases?: string[]; create?: Creation[]; create_types?: string[] }

export class ApiError extends Error {
  status: number
  code: string
  unresolved: Unresolved[]
  constructor(status: number, code: string, message: string, unresolved: Unresolved[] = []) {
    super(message)
    this.status = status
    this.code = code
    this.unresolved = unresolved
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: { "Content-Type": "application/json", "X-Vrac-Client": "web" },
    body: body === undefined ? undefined : JSON.stringify(body),
    credentials: "same-origin",
  })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new ApiError(res.status, data.error ?? "error", data.message ?? res.statusText, data.unresolved)
  return data as T
}

export const requestId = () => crypto.randomUUID()

const q = (params: Record<string, string | number | undefined>) => {
  const s = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) if (v !== undefined && v !== "") s.set(k, String(v))
  const str = s.toString()
  return str ? "?" + str : ""
}

export const api = {
  session: () => request<Session>("GET", "/api/v1/session"),
  setupStart: (username: string, password: string) => request<Ceremony>("POST", "/api/v1/setup/start", { username, password }),
  loginStart: (username: string, password: string) => request<Ceremony>("POST", "/api/v1/login/start", { username, password }),
  signinFinish: (flow: string, credential: unknown, name: string) =>
    request("POST", `/api/v1/signin/finish?flow=${encodeURIComponent(flow)}&name=${encodeURIComponent(name)}`, credential),
  passkeys: () => request<{ passkeys: Passkey[] }>("GET", "/api/v1/passkeys"),
  addPasskeyStart: () => request<Ceremony>("POST", "/api/v1/passkeys/start"),
  addPasskeyFinish: (flow: string, credential: unknown, name: string) =>
    request("POST", `/api/v1/passkeys/finish?flow=${encodeURIComponent(flow)}&name=${encodeURIComponent(name)}`, credential),
  deletePasskey: (id: string) => request("DELETE", `/api/v1/passkeys/${encodeURIComponent(id)}`),
  logout: () => request("POST", "/api/v1/logout"),
  changePassword: (current: string, password: string) => request("POST", "/api/v1/password", { current, password }),

  days: (before?: string) => request<{ days: string[] }>("GET", "/api/v1/days" + q({ before, limit: 10 })),
  day: (day: string) => request<DayView>("GET", `/api/v1/days/${day}`),
  node: (id: string) => request<NodeView>("GET", `/api/v1/nodes/${id}`),
  children: (id: string, cursor?: string) => request<Page>("GET", `/api/v1/nodes/${id}/children` + q({ cursor })),
  lists: (id: string) => request<{ lists: ListCount[] }>("GET", `/api/v1/nodes/${id}/lists`),
  roots: () => request<Page>("GET", "/api/v1/roots"),
  search: (query: Query) => request<Page>("POST", "/api/v1/search", query),
  resolve: (name: string) => request<{ candidates: Brief[] }>("GET", "/api/v1/resolve" + q({ name, limit: 8 })),
  types: () => request<{ types: Type[] }>("GET", "/api/v1/types"),
  type: (name: string) => request<Type>("GET", `/api/v1/types/${encodeURIComponent(name)}`),
  createType: (name: string, todo = false) => request<Type>("POST", "/api/v1/types", { request_id: requestId(), name, todo }),
  setTypeTodo: (name: string, todo: boolean) => request<Type>("PATCH", `/api/v1/types/${encodeURIComponent(name)}`, { todo }),
  activity: (cursor?: string) => request<{ items: Activity[]; next_cursor?: string }>("GET", "/api/v1/activity" + q({ cursor })),

  create: (body: { placement?: Placement; nodes: Draft[]; create?: Creation[]; create_types?: string[]; source?: string }) =>
    request<Result>("POST", "/api/v1/nodes", { request_id: requestId(), ...body }),
  update: (node: Node, change: Change) =>
    request<Result>("PATCH", `/api/v1/nodes/${node.id}`, { request_id: requestId(), expected_revision: node.revision, ...change }),
  move: (node: Node, placement: Placement) =>
    request<Result>("POST", `/api/v1/nodes/${node.id}/move`, { request_id: requestId(), expected_revision: node.revision, placement }),
  trash: (node: Node) => request<Result>("POST", `/api/v1/nodes/${node.id}/trash`, { request_id: requestId(), expected_revision: node.revision }),
  undo: (id: string, revision: number) => request<Result>("POST", `/api/v1/nodes/${id}/undo`, { request_id: requestId(), expected_revision: revision }),

  preferences: () => request<Preferences>("GET", "/api/v1/preferences"),
  setPreferences: (p: Preferences) => request<Preferences>("PUT", "/api/v1/preferences", p),

  connections: () => request<{ connections: Connection[] }>("GET", "/api/v1/connections"),
  createToken: (name: string) => request<{ token: string }>("POST", "/api/v1/tokens", { name }),
  revoke: (c: Connection) => request("DELETE", `/api/v1/connections/${c.kind}/${c.id}`),
  oauthRequest: (query: string) => request<{ client_name: string; redirect_host: string }>("GET", "/api/v1/oauth/request?" + query),
  oauthDecision: (query: string, approve: boolean) => request<{ redirect: string }>("POST", "/api/v1/oauth/decision", { query, approve }),
}
