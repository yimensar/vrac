import { vi } from "vitest"
import type { Node } from "./lib/api"

// An in-memory stand-in for the Vrac API, enough to exercise the interface:
// journal days, node creation, updates, moves and trash.

export type Call = { method: string; url: string; body: any }

type Row = Node & { position: number }

export function fakeServer(opts: { authenticated?: boolean; today?: string; search?: (body: any) => unknown } = {}) {
  const today = opts.today ?? "2026-11-05"
  const calls: Call[] = []
  const rows = new Map<string, Row>()
  let seq = 0
  const id = () => (++seq).toString(36).padStart(26, "a").replace(/[0-9]/g, (d) => "abcdefghij"[Number(d)])
  const dayId = "dayaaaaaaaaaaaaaaaaaaaaaaa"
  const types = [
    { name: "task", color: "blue", todo: true, count: 0 },
    { name: "meeting", color: "violet", todo: false, count: 0 },
    { name: "project", color: "emerald", todo: false, count: 1 },
    { name: "person", color: "rose", todo: false, count: 0 },
  ]
  const northwind = { id: "northwindaaaaaaaaaaaaaaaaa", name: "Northwind", types: ["project"] }

  const children = (parent: string) =>
    [...rows.values()].filter((r) => r.parent_id === parent && !r.trashed).sort((a, b) => a.position - b.position)
  const view = (r: Row): Node => ({ ...r, child_count: children(r.id).length })
  const page = (parent: string) => ({ nodes: children(parent).map(view), refs: {} })
  const parse = (input: string) => {
    const types = [...input.matchAll(/(?:^|\s)#([\p{L}\p{N}_-]+)/gu)].map((m) => m[1])
    // Like the engine, type tokens stay where they are written.
    const text = input.replace(/\[\[([^|\]]+)\|[a-z]{26}\]\]/g, "[[$1]]").trim()
    return { text, types }
  }
  const place = (r: Row, p: any) => {
    const parent = p.parent_id ?? dayId
    const siblings = children(parent).filter((s) => s.id !== r.id)
    let index = siblings.length
    if (p.first) index = 0
    else if (p.after_id) index = siblings.findIndex((s) => s.id === p.after_id) + 1
    siblings.splice(index, 0, r)
    r.parent_id = parent
    siblings.forEach((s, i) => (s.position = i))
  }

  const fetch = vi.fn(async (url: string, init?: RequestInit) => {
    const method = init?.method ?? "GET"
    const body = init?.body ? JSON.parse(String(init.body)) : undefined
    calls.push({ method, url, body })
    const json = (data: unknown, status = 200) => new Response(JSON.stringify(data), { status, headers: { "Content-Type": "application/json" } })
    let m: RegExpMatchArray | null
    if (url === "/api/v1/session") return json({ owner: true, authenticated: opts.authenticated ?? true, today, timezone: "Europe/Paris", version: "test", mcp_url: "" })
    if (url === "/api/v1/login/start") return json({ flow: "f1", mode: "get", options: { publicKey: { challenge: "AA" } } })
    if (url.startsWith("/api/v1/signin/finish")) return json({ authenticated: true })
    if (url === "/api/v1/today") return json({ today, day: { day: today, exists: true, children: page(dayId) }, agenda: { nodes: [], refs: {} }, due: { nodes: [], refs: {} } })
    if (url === "/api/v1/types" && method === "POST") {
      const t = { name: body.name, color: "amber", todo: body.todo, count: 0 }
      types.push(t)
      return json(t)
    }
    if ((m = url.match(/^\/api\/v1\/types\/(.+)$/)) && method === "PATCH") {
      const t = types.find((t) => t.name === decodeURIComponent(m![1]))!
      t.todo = body.todo
      return json(t)
    }
    if (url === "/api/v1/types") return json({ types })
    if (url.startsWith("/api/v1/days?")) return json({ days: [] })
    if (url.startsWith("/api/v1/dates?")) return json(new URL(url, "http://vrac").searchParams.get("text")?.toLowerCase() === "yesterday" ? { date: "2026-11-04" } : {})
    if (url === "/api/v1/search") {
      // Without a stand-in, words match the day's rows.
      const text = String(body.text ?? "").toLowerCase()
      const matching = text ? [...rows.values()].filter((r) => !r.trashed && r.text.toLowerCase().includes(text)).map(view) : []
      return json(opts.search?.(body) ?? { nodes: matching, refs: {} })
    }
    if (url === "/api/v1/preferences") return json({ fold: "default", theme: "system", scale: "default" })
    if (url.startsWith("/api/v1/resolve")) return json({ candidates: [northwind] })
    if ((m = url.match(/^\/api\/v1\/days\/(.+)$/))) return json({ day: m[1], exists: true, node: { id: dayId }, children: page(dayId) })
    if ((m = url.match(/^\/api\/v1\/nodes\/([a-z]+)\/children/))) return json(page(m[1]))
    if (url === "/api/v1/nodes" && method === "POST") {
      const out: Node[] = []
      for (const n of body.nodes) {
        const { text, types } = parse(n.input)
        const r: Row = {
          id: id(), kind: "node", text, types, aliases: [], day: today, revision: 1, created_at: "", updated_at: "", created_by: "owner",
          updated_by: "owner", source: "web", child_count: 0, ref_ids: [], position: 0, status: n.status ?? (types.includes("task") ? "open" : undefined),
          date: n.date_text ? "2026-11-06" : n.date,
        }
        rows.set(r.id, r)
        place(r, body.placement ?? {})
        out.push(view(r))
      }
      return json({ nodes: out, refs: {} })
    }
    if ((m = url.match(/^\/api\/v1\/nodes\/([a-z]+)$/)) && method === "GET") {
      const r = rows.get(m[1])
      return r ? json({ node: view(r), path: [{ id: dayId, name: today, types: [] }], children: page(r.id), refs: {} }) : json({ error: "not_found", message: "not found" }, 404)
    }
    if ((m = url.match(/^\/api\/v1\/nodes\/([a-z]+)(\/(move|trash))?$/))) {
      const r = rows.get(m[1])!
      if (body.expected_revision !== r.revision) return json({ error: "conflict", message: "conflict" }, 409)
      r.revision++
      if (m[3] === "move") place(r, body.placement)
      else if (m[3] === "trash") r.trashed = true
      else {
        if (body.input !== undefined) {
          Object.assign(r, parse(body.input))
          r.status = r.types.includes("task") ? (r.status ?? "open") : undefined
        }
        if (body.status !== undefined) r.status = body.status || undefined
        if (body.date_text) r.date = "2026-11-06"
        if (body.date !== undefined) r.date = body.date || undefined
      }
      return json({ nodes: [view(r)], refs: {} })
    }
    return json({ error: "not_found", message: "not found " + url }, 404)
  })
  vi.stubGlobal("fetch", fetch)

  return {
    calls,
    /** Texts of the day's tree, children indented with two spaces. */
    tree(parent = dayId, depth = 0): string[] {
      return children(parent).flatMap((r) => ["  ".repeat(depth) + r.text, ...this.tree(r.id, depth + 1)])
    },
  }
}
