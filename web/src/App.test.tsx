import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { App, makeClient } from "./App"
import { fakeServer } from "./test-server"

beforeEach(() => window.history.pushState({}, "", "/"))
afterEach(() => vi.unstubAllGlobals())

describe("app", () => {
  it("signs in with the password, then a passkey", async () => {
    const server = fakeServer({ authenticated: false })
    // A browser with passkeys: the credential answers the server's challenge.
    const get = vi.fn(async () => ({ toJSON: () => ({ id: "cred", type: "public-key" }) }))
    vi.stubGlobal("PublicKeyCredential", function PublicKeyCredential() {})
    Object.defineProperty(navigator, "credentials", { value: { create: vi.fn(), get }, configurable: true })
    render(<App client={makeClient()} />)
    await userEvent.type(await screen.findByLabelText("Username"), "yan")
    await userEvent.type(screen.getByLabelText("Password"), "correct horse battery")
    await userEvent.click(screen.getByRole("button", { name: "Continue" }))
    await waitFor(() => expect(server.calls.some((c) => c.url.startsWith("/api/v1/signin/finish?flow=f1"))).toBe(true))
    const start = server.calls.find((c) => c.url === "/api/v1/login/start")!
    expect(start.body).toEqual({ username: "yan", password: "correct horse battery" })
    expect(get).toHaveBeenCalledOnce()
    expect(server.calls.find((c) => c.url.startsWith("/api/v1/signin/finish"))!.body).toEqual({ id: "cred", type: "public-key" })
  })

  it("writes in today's journal through the editor", async () => {
    const server = fakeServer()
    render(<App client={makeClient()} />)
    await userEvent.click(await screen.findByLabelText("New line"))
    const editor = await screen.findByRole("textbox", { name: "New line" })
    expect(editor.getAttribute("contenteditable")).toBe("true")
    await userEvent.keyboard("first line{Enter}")
    await waitFor(() => expect(server.tree()).toEqual(["first line"]))
    // The saved row shows the same text, and clicking it edits it.
    await userEvent.click(await screen.findByText("first line"))
    expect((await screen.findByRole("textbox", { name: "Edit" })).getAttribute("contenteditable")).toBe("true")
  })

  it("writes what is typed on the page into today's new line", async () => {
    const server = fakeServer()
    render(<App client={makeClient()} />)
    await screen.findByLabelText("New line")
    // Nothing is focused: the first key opens the new line with it.
    await userEvent.keyboard("hello")
    expect(await screen.findByRole("textbox", { name: "New line" })).toHaveTextContent("hello")
    await userEvent.keyboard("{Enter}")
    await waitFor(() => expect(server.tree()).toEqual(["hello"]))
  })

  it("keeps open tasks in mind by project above the journal", async () => {
    const today = "2026-11-05"
    const task = (id: string, text: string, date?: string) => ({
      id, kind: "node", text, types: ["task"], status: "open", date, aliases: [], day: today, revision: 1, created_at: "", updated_at: "",
      created_by: "owner", updated_by: "owner", source: "web", child_count: 0, ref_ids: [],
    })
    const northwind = "northwindaaaaaaaaaaaaaaaaa"
    fakeServer({
      today,
      search: (body) =>
        body.status === "open"
          ? {
              nodes: [task("taskaaaaaaaaaaaaaaaaaaaaaa", "Buy milk #task"), task("taskbbbbbbbbbbbbbbbbbbbbbb", "Send the plan #task", "2026-11-01")],
              refs: { [northwind]: { id: northwind, name: "Northwind", label: "Northwind", types: ["project"] } },
              subjects: { taskbbbbbbbbbbbbbbbbbbbbbb: [northwind] },
            }
          : { nodes: [], refs: {} },
    })
    render(<App client={makeClient()} />)
    // The to-do under the meeting about Northwind lands in Northwind's
    // group; the other has no subject. The overdue one is counted.
    const group = await screen.findByRole("link", { name: "Northwind" })
    expect(group.closest("div")!.parentElement).toHaveTextContent("1 late")
    expect(screen.getByText("No subject")).toBeInTheDocument()
    // The overdue task is also due: it shows in both lists.
    expect(screen.getAllByText("Send the plan")).toHaveLength(2)
    // Folding a project is remembered on this device.
    await userEvent.click(screen.getAllByRole("button", { name: "Collapse", expanded: true })[1])
    expect(screen.getAllByText("Send the plan")).toHaveLength(1)
    expect(JSON.parse(localStorage.getItem("vrac.folded-subjects")!)).toEqual([northwind])
    localStorage.clear()
  })

  it("offers types and commands after a slash", async () => {
    const server = fakeServer()
    render(<App client={makeClient()} />)
    await userEvent.click(await screen.findByLabelText("New line"))
    await screen.findByRole("textbox", { name: "New line" })
    await userEvent.keyboard("call Marc /ta")
    // The query narrows the menu to #task; Enter inserts the tag.
    const option = await screen.findByRole("option", { name: /#task/ })
    expect(screen.queryByRole("option", { name: /Date/ })).toBeNull()
    expect(option).toHaveAttribute("aria-selected", "true")
    await userEvent.keyboard("{Enter}")
    expect(screen.queryByRole("listbox")).toBeNull()
    await userEvent.keyboard("{Enter}")
    await waitFor(() => expect(server.tree()).toEqual(["call Marc #task"]))
  })

  it("completes a reference after @", async () => {
    const server = fakeServer()
    render(<App client={makeClient()} />)
    await userEvent.click(await screen.findByLabelText("New line"))
    await screen.findByRole("textbox", { name: "New line" })
    await userEvent.keyboard("see @North")
    await userEvent.click(await screen.findByRole("option", { name: /Northwind/ }))
    await userEvent.keyboard("{Enter}")
    await waitFor(() => expect(server.tree()).toEqual(["see [[Northwind]]"]))
    expect(server.calls.find((c) => c.method === "POST" && c.url === "/api/v1/nodes")!.body.nodes[0].input).toBe("see [[Northwind|northwindaaaaaaaaaaaaaaaaa]]")
  })
})
