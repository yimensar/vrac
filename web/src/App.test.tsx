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

  it("opens on today alone, and the journal scrolls through the days", async () => {
    const server = fakeServer()
    const { unmount } = render(<App client={makeClient()} />)
    await screen.findByLabelText("New line")
    expect(screen.getByRole("link", { name: "Today" })).toBeInTheDocument()
    // Nothing above or below today: no open to-dos, no older days.
    expect(server.calls.some((c) => c.url === "/api/v1/search" || c.url.startsWith("/api/v1/days?"))).toBe(false)
    unmount()
    window.history.pushState({}, "", "/journal")
    render(<App client={makeClient()} />)
    // Today is written in the journal too.
    await screen.findByLabelText("New line")
    await waitFor(() => expect(server.calls.some((c) => c.url.startsWith("/api/v1/days?"))).toBe(true))
  })

  it("creates a tag from the palette, then acts on it in its own menu", async () => {
    const server = fakeServer()
    render(<App client={makeClient()} />)
    await screen.findByLabelText("New line")
    await userEvent.keyboard("{Meta>}k{/Meta}")
    const input = await screen.findByPlaceholderText("Find a note, a tag, a view, a setting…")
    // "Create a tag" opens its menu; the field then takes the name.
    await userEvent.type(input, "new")
    await userEvent.click(await screen.findByRole("option", { name: /Create a tag/ }))
    await userEvent.type(await screen.findByPlaceholderText("Name of the new tag"), "idea")
    expect(screen.getByRole("navigation", { name: "Menu path" })).toHaveTextContent("New tag")
    await userEvent.keyboard("{ArrowDown}{Enter}")
    await waitFor(() => expect(server.calls.find((c) => c.url === "/api/v1/types" && c.method === "POST")?.body).toMatchObject({ name: "idea", todo: true }))
    // The new tag has its own menu: make it a plain tag.
    await userEvent.keyboard("{Meta>}k{/Meta}")
    await userEvent.type(await screen.findByPlaceholderText("Find a note, a tag, a view, a setting…"), "#ide")
    await userEvent.click(await screen.findByRole("option", { name: /idea/ }))
    await screen.findByPlaceholderText("Do with #idea…")
    await userEvent.keyboard("plain{Enter}")
    await waitFor(() => expect(server.calls.find((c) => c.method === "PATCH")).toMatchObject({ url: "/api/v1/types/idea", body: { todo: false } }))
  })

  it("goes back up a palette menu with Backspace", async () => {
    fakeServer()
    render(<App client={makeClient()} />)
    await screen.findByLabelText("New line")
    await userEvent.keyboard("{Meta>}k{/Meta}")
    await userEvent.type(await screen.findByPlaceholderText("Find a note, a tag, a view, a setting…"), "theme{Enter}")
    expect(await screen.findByRole("option", { name: "Dark" })).toBeInTheDocument()
    await userEvent.keyboard("{Backspace}")
    expect(await screen.findByPlaceholderText("Find a note, a tag, a view, a setting…")).toHaveValue("")
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

  it("dates a line from a phrase with /due", async () => {
    const server = fakeServer()
    render(<App client={makeClient()} />)
    await userEvent.click(await screen.findByLabelText("New line"))
    await screen.findByRole("textbox", { name: "New line" })
    await userEvent.keyboard("call Marc /due friday")
    await userEvent.click(await screen.findByRole("option", { name: /Date: friday/ }))
    // The phrase shows at once, and leaves with the line.
    expect(screen.getByText("friday…")).toBeInTheDocument()
    await userEvent.keyboard("{Enter}")
    await waitFor(() => expect(server.tree()).toEqual(["call Marc"]))
    expect(server.calls.find((c) => c.method === "POST" && c.url === "/api/v1/nodes")!.body.nodes).toEqual([{ input: "call Marc", date_text: "friday" }])
  })

  it("creates a reference with a tag in one go", async () => {
    const server = fakeServer()
    render(<App client={makeClient()} />)
    await userEvent.click(await screen.findByLabelText("New line"))
    await screen.findByRole("textbox", { name: "New line" })
    // user-event reads "[[" as an escaped bracket: four give two.
    await userEvent.keyboard("lunch with [[[[Dupont")
    // Create, then pick the tag.
    await userEvent.click(await screen.findByRole("option", { name: /Create “Dupont”/ }))
    await userEvent.click(await screen.findByRole("option", { name: /“Dupont” #person/ }))
    await userEvent.keyboard("{Enter}")
    await waitFor(() => expect(server.tree()).toEqual(["lunch with [[Dupont]]"]))
    const body = server.calls.find((c) => c.method === "POST" && c.url === "/api/v1/nodes")!.body
    expect(body.create).toEqual([{ name: "Dupont", types: ["person"] }])
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
