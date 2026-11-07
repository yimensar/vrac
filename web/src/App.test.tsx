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

  it("offers types and commands after a slash", async () => {
    const server = fakeServer()
    render(<App client={makeClient()} />)
    await userEvent.click(await screen.findByLabelText("New line"))
    await screen.findByRole("textbox", { name: "New line" })
    await userEvent.keyboard("call Marc /ta")
    // The query narrows the menu to #task; Enter inserts the type.
    const option = await screen.findByRole("option", { name: /#task/ })
    expect(screen.queryByRole("option", { name: /Tomorrow/ })).toBeNull()
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
