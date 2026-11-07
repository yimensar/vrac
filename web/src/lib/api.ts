// Typed client of the Vrac HTTP API. The interface holds no business rules:
// everything goes through these calls.

export type Node = {
  id: string
  kind: "node" | "journal" | "day"
  parent_id?: string
  text: string
  types: string[]
  status?: "open" | "done"
  due?: string
  start?: string
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

/** name is plain; label keeps the #types written inside the sentence. */
export type Ref = { id: string; name: string; label: string; types: string[]; trashed?: boolean }
export type Refs = Record<string, Ref>
/** subjects, when asked for, lists the typed nodes each result concerns, most specific first. */
export type Page = { nodes: Node[]; refs: Refs; subjects?: Record<string, string[]>; next_cursor?: string }
export type Brief = { id: string; name: string; label?: string; types: string[]; path?: string[] }
export type Type = { name: string; builtin: boolean; color: string; count: number }
export type Creation = { name: string; types?: string[] }
export type Unresolved = { kind: "type" | "node"; name: string; candidates: Brief[] }

export type Session = { owner: boolean; authenticated: boolean; today: string; timezone: string; version: string; mcp_url: string; provider: string }
export type Ceremony = { flow: string; mode: "create" | "get"; options: { publicKey: Record<string, any> } }
export type Passkey = { id: string; name: string; created_at: string; last_used_at?: string }
export type DayView = { day: string; exists: boolean; node?: Node; children: Page }
export type NodeView = { node: Node; path: Brief[]; children: Page; refs: Refs }
export type Sections = { open_tasks: Node[]; upcoming: Node[]; meetings: Node[]; decisions: Node[]; mentions: Page; refs: Refs }
export type TodayView = { today: string; day: DayView; agenda: Page; due: Page }
export type TypeRow = { node: Node; path: string[]; open_tasks: number; last_activity: string }
export type TypeView = { type: Type; rows: TypeRow[]; refs: Refs }
export type Activity = { node: Brief; revision: number; action: string; at: string; actor: string; source: string; latest: boolean }
export type Connection = { id: string; kind: "token" | "client"; name: string; created_at: string; last_used_at?: string }
export type Result = { nodes: Node[]; refs: Refs; created?: Brief[] }
export type Fold = "default" | "expanded" | "collapsed"
export type Preferences = { fold: Fold; theme: "system" | "light" | "dark"; parse_dates: boolean }

/** The preferences in effect, kept current by the app (see preferences.ts). */
export const current: Preferences = { fold: "default", theme: "system", parse_dates: true }

export type Query = {
  text?: string
  types?: string[]
  concerns?: string[]
  mentions?: string
  status?: "open" | "done"
  from?: string
  to?: string
  scheduled?: boolean
  with_subjects?: boolean
  sort?: "date_desc" | "date_asc" | "updated"
  limit?: number
  cursor?: string
}

export type Placement = { parent_id?: string; day?: string; root?: boolean; after_id?: string; first?: boolean }

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

  today: () => request<TodayView>("GET", "/api/v1/today"),
  days: (before?: string) => request<{ days: string[] }>("GET", "/api/v1/days" + q({ before, limit: 10 })),
  day: (day: string) => request<DayView>("GET", `/api/v1/days/${day}`),
  node: (id: string) => request<NodeView>("GET", `/api/v1/nodes/${id}`),
  children: (id: string, cursor?: string) => request<Page>("GET", `/api/v1/nodes/${id}/children` + q({ cursor })),
  sections: (id: string) => request<Sections>("GET", `/api/v1/nodes/${id}/sections`),
  roots: () => request<Page>("GET", "/api/v1/roots"),
  search: (query: Query) => request<Page>("POST", "/api/v1/search", query),
  resolve: (name: string) => request<{ candidates: Brief[] }>("GET", "/api/v1/resolve" + q({ name, limit: 8 })),
  types: () => request<{ types: Type[] }>("GET", "/api/v1/types"),
  type: (name: string) => request<TypeView>("GET", `/api/v1/types/${encodeURIComponent(name)}`),
  createType: (name: string) => request<Type>("POST", "/api/v1/types", { request_id: requestId(), name }),
  activity: (cursor?: string) => request<{ items: Activity[]; next_cursor?: string }>("GET", "/api/v1/activity" + q({ cursor })),

  create: (body: { placement?: Placement; nodes: { input: string; due?: string; start?: string }[]; create?: Creation[]; create_types?: string[]; parse_dates?: boolean }) =>
    request<Result>("POST", "/api/v1/nodes", { request_id: requestId(), parse_dates: current.parse_dates, ...body }),
  update: (
    node: Node,
    change: { input?: string; status?: string; due?: string; start?: string; end?: string; aliases?: string[]; create?: Creation[]; create_types?: string[] },
  ) => request<Result>("PATCH", `/api/v1/nodes/${node.id}`, { request_id: requestId(), expected_revision: node.revision, parse_dates: current.parse_dates, ...change }),
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
