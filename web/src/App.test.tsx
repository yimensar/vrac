import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { App, makeClient } from "./App"

type Call = { method: string; url: string; body: any }

const northwind = { id: "abcdefghijklmnopqrstuvwxyz", name: "Northwind", types: ["project"] }
const types = [
  { name: "task", builtin: true, color: "blue", count: 0 },
  { name: "meeting", builtin: true, color: "violet", count: 0 },
  { name: "project", builtin: true, color: "emerald", count: 1 },
]

let calls: Call[]

function mockServer(authenticated = true) {
  calls = []
  vi.stubGlobal("fetch", vi.fn(async (url: string, init?: RequestInit) => {
    const method = init?.method ?? "GET"
    const body = init?.body ? JSON.parse(String(init.body)) : undefined
    calls.push({ method, url, body })
    const json = (data: unknown, status = 200) => new Response(JSON.stringify(data), { status, headers: { "Content-Type": "application/json" } })
    if (url === "/api/v1/session") return json({ owner: true, authenticated, today: "2026-11-05", timezone: "Europe/Paris", version: "test", mcp_url: "http://localhost/mcp" })
    if (url === "/api/v1/today") return json({ today: "2026-11-05", day: { day: "2026-11-05", exists: false, children: { nodes: [], refs: {} } }, agenda: { nodes: [], refs: {} }, due: { nodes: [], refs: {} } })
    if (url === "/api/v1/types") return json({ types })
    if (url.startsWith("/api/v1/resolve")) return json({ candidates: [northwind] })
    if (url === "/api/v1/nodes" && method === "POST") return json({ nodes: [], refs: {} })
    if (url === "/api/v1/login") return json({ authenticated: true })
    return json({ error: "not_found", message: "not found" }, 404)
  }))
}

beforeEach(() => {
  window.history.pushState({}, "", "/")
})
afterEach(() => vi.unstubAllGlobals())

describe("app", () => {
  it("asks for the password when signed out", async () => {
    mockServer(false)
    render(<App client={makeClient()} />)
    await userEvent.type(await screen.findByLabelText("Password"), "correct horse battery")
    await userEvent.click(screen.getByRole("button", { name: "Sign in" }))
    await waitFor(() => expect(calls.some((c) => c.url === "/api/v1/login" && c.body.password === "correct horse battery")).toBe(true))
  })

  it("writes in today's journal with a completed reference and type", async () => {
    mockServer()
    render(<App client={makeClient()} />)
    const box = await screen.findByPlaceholderText(/Write…/)
    await userEvent.type(box, "Call [[[[Su")
    await userEvent.click(await screen.findByRole("option", { name: /Northwind/ }))
    await userEvent.type(box, "#ta")
    await userEvent.keyboard("{Enter}")
    await userEvent.type(box, "{Enter}")
    await waitFor(() => expect(calls.some((c) => c.url === "/api/v1/nodes")).toBe(true))
    const create = calls.find((c) => c.url === "/api/v1/nodes")!
    expect(create.body.placement).toEqual({ day: "2026-11-05" })
    expect(create.body.nodes[0].input).toBe(`Call [[Northwind|${northwind.id}]] #task`)
    expect(create.body.parse_dates).toBe(true)
  })

  it("creates an unknown reference only when asked", async () => {
    mockServer()
    render(<App client={makeClient()} />)
    const box = await screen.findByPlaceholderText(/Write…/)
    await userEvent.type(box, "Lunch with [[[[Dupont")
    await userEvent.click(await screen.findByRole("option", { name: "Create “Dupont” as #person" }))
    await userEvent.type(box, "{Enter}")
    await waitFor(() => expect(calls.some((c) => c.url === "/api/v1/nodes")).toBe(true))
    const create = calls.find((c) => c.url === "/api/v1/nodes")!
    expect(create.body.nodes[0].input).toBe("Lunch with [[Dupont]]")
    expect(create.body.create).toEqual([{ name: "Dupont", types: ["person"] }])
  })
})
