import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { App, makeClient } from "./App"
import { fakeServer } from "./test-server"

const FIND = "Search notes, tags, views, commands…"

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
    const input = await screen.findByPlaceholderText(FIND)
    // "Create a tag" opens its menu; the field then takes the name.
    await userEvent.type(input, "new")
    await userEvent.click(await screen.findByRole("option", { name: /Create a tag/ }))
    await userEvent.type(await screen.findByPlaceholderText("Name of the new tag"), "idea")
    expect(screen.getByRole("navigation", { name: "Menu path" })).toHaveTextContent("New tag")
    await userEvent.keyboard("{ArrowDown}{Enter}")
    await waitFor(() => expect(server.calls.find((c) => c.url === "/api/v1/types" && c.method === "POST")?.body).toMatchObject({ name: "idea", todo: true }))
    // → on the new tag opens its actions: make it a plain tag.
    await userEvent.keyboard("{Meta>}k{/Meta}")
    await userEvent.type(await screen.findByPlaceholderText(FIND), "#ide")
    await screen.findByRole("option", { name: /#idea/ })
    await userEvent.keyboard("{ArrowRight}")
    await screen.findByPlaceholderText("Do with #idea…")
    await userEvent.keyboard("plain{Enter}")
    await waitFor(() => expect(server.calls.find((c) => c.method === "PATCH")).toMatchObject({ url: "/api/v1/types/idea", body: { todo: false } }))
  })

  it("acts on a node from the palette: done, then a date from a phrase", async () => {
    const server = fakeServer()
    render(<App client={makeClient()} />)
    await userEvent.click(await screen.findByLabelText("New line"))
    await screen.findByRole("textbox", { name: "New line" })
    // The first Enter takes the tag from the completion menu.
    await userEvent.keyboard("Buy milk #task{Enter}{Enter}")
    await waitFor(() => expect(server.tree()).toEqual(["Buy milk #task"]))
    await userEvent.keyboard("{Meta>}k{/Meta}")
    await userEvent.type(await screen.findByPlaceholderText(FIND), "milk")
    const row = await screen.findByRole("option", { name: /Buy milk/ })
    await userEvent.click(within(row).getByRole("button", { name: "Actions" }))
    await screen.findByPlaceholderText("Do with this node…")
    await userEvent.click(await screen.findByRole("option", { name: /Mark done/ }))
    await waitFor(() => expect(server.calls.find((c) => c.method === "PATCH" && c.body.status === "done")).toBeTruthy())
    // The date menu understands a phrase.
    await userEvent.keyboard("{Meta>}k{/Meta}")
    await userEvent.type(await screen.findByPlaceholderText(FIND), "milk")
    await userEvent.click(within(await screen.findByRole("option", { name: /Buy milk/ })).getByRole("button", { name: "Actions" }))
    await userEvent.click(await screen.findByRole("option", { name: /Set a date/ }))
    await userEvent.type(await screen.findByPlaceholderText("Friday, in 3 days, next week, 12 nov 14h…"), "friday{Enter}")
    await waitFor(() => expect(server.calls.find((c) => c.method === "PATCH" && c.body.date_text === "friday")).toBeTruthy())
  })

  it("writes a line in today's journal from the palette", async () => {
    const server = fakeServer()
    render(<App client={makeClient()} />)
    await screen.findByLabelText("New line")
    await userEvent.keyboard("{Meta>}k{/Meta}")
    await userEvent.type(await screen.findByPlaceholderText(FIND), "write{Enter}")
    await userEvent.type(await screen.findByPlaceholderText("A line in today's journal…"), "hello from the box{Enter}")
    await waitFor(() => expect(server.tree()).toEqual(["hello from the box"]))
  })

  it("opens a journal day from a phrase, whether it exists or not", async () => {
    fakeServer()
    render(<App client={makeClient()} />)
    await screen.findByLabelText("New line")
    await userEvent.keyboard("{Meta>}k{/Meta}")
    await userEvent.type(await screen.findByPlaceholderText(FIND), "yesterday")
    // The fake server's today is not the browser's: the day shows by its date.
    await userEvent.click(await screen.findByRole("option", { name: /November 4|4 November/ }))
    await waitFor(() => expect(window.location.pathname).toBe("/day/2026-11-04"))
  })

  it("keeps a line being written when the page changes", async () => {
    const server = fakeServer()
    render(<App client={makeClient()} />)
    await screen.findByLabelText("New line")
    // Typed in the new line, not entered, then away through the palette.
    await userEvent.keyboard("half a task")
    expect(await screen.findByRole("textbox", { name: "New line" })).toHaveTextContent("half a task")
    await userEvent.keyboard("{Meta>}k{/Meta}")
    await userEvent.type(await screen.findByPlaceholderText(FIND), "agenda{Enter}")
    await waitFor(() => expect(window.location.pathname).toBe("/agenda"))
    await waitFor(() => expect(server.tree()).toEqual(["half a task"]))
  })

  it("goes back up a palette menu with Backspace", async () => {
    fakeServer()
    render(<App client={makeClient()} />)
    await screen.findByLabelText("New line")
    await userEvent.keyboard("{Meta>}k{/Meta}")
    await userEvent.type(await screen.findByPlaceholderText(FIND), "theme{Enter}")
    expect(await screen.findByRole("option", { name: /Dark/ })).toBeInTheDocument()
    await userEvent.keyboard("{Backspace}")
    expect(await screen.findByPlaceholderText(FIND)).toHaveValue("")
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

  it("keeps a line break written with Shift+Enter while typing goes on", async () => {
    const server = fakeServer()
    render(<App client={makeClient()} />)
    await userEvent.click(await screen.findByLabelText("New line"))
    await screen.findByRole("textbox", { name: "New line" })
    await userEvent.keyboard("first{Shift>}{Enter}{/Shift}second{Enter}")
    await waitFor(() => expect(server.tree()).toEqual(["first\nsecond"]))
  })

  it("formats with keys and slash commands: bold, code, link", async () => {
    fakeServer()
    render(<App client={makeClient()} />)
    await userEvent.click(await screen.findByLabelText("New line"))
    await screen.findByRole("textbox", { name: "New line" })
    // ⌘B (Ctrl here) opens the markers and the caret stays inside them;
    // the caret cannot leave them in this DOM, so each goes on its own line.
    const editor = screen.getByRole("textbox", { name: "New line" })
    await userEvent.keyboard("say {Control>}b{/Control}hi")
    expect(editor).toHaveTextContent("say **hi**")
    await userEvent.keyboard("{Control>}a{/Control}{Backspace}run /cod")
    await userEvent.click((await screen.findAllByRole("option", { name: /^Code/ }))[0])
    await userEvent.keyboard("ls")
    expect(editor).toHaveTextContent("run `ls`")
  })

  it("edits a table as a table: cells, Tab, Enter, then a row below", async () => {
    const server = fakeServer()
    render(<App client={makeClient()} />)
    await userEvent.click(await screen.findByLabelText("New line"))
    await screen.findByRole("textbox", { name: "New line" })
    await userEvent.keyboard("/table 2x1")
    await userEvent.click(await screen.findByRole("option", { name: /Table 2 × 1/ }))
    // The row turns into cells, the first header cell focused.
    // The row reopens on the table, so the cell is looked up again.
    await waitFor(() => expect(screen.getByRole("textbox", { name: "Row 1, column 1" })).toHaveFocus())
    await userEvent.keyboard("TV{Tab}Price{Tab}77OLED811{Tab}2199 €")
    // Tab past the last cell adds a row; Enter on an empty last row leaves.
    await userEvent.keyboard("{Tab}")
    expect(screen.getByRole("textbox", { name: "Row 3, column 1" })).toHaveFocus()
    await userEvent.keyboard("{Backspace}")
    expect(screen.queryByRole("textbox", { name: "Row 3, column 1" })).toBeNull()
    // Enter on a filled last row adds one; on the empty row it leaves,
    // dropping it.
    await userEvent.keyboard("{Enter}")
    expect(screen.getByRole("textbox", { name: "Row 3, column 1" })).toHaveFocus()
    await userEvent.keyboard("{Enter}")
    await waitFor(() => expect(server.tree()).toEqual(["| TV | Price |\n|---|---|\n| 77OLED811 | 2199 € |"]))
    // The saved row reads as a table and opens as one again.
    expect(await screen.findByRole("columnheader", { name: "TV" })).toBeInTheDocument()
  })

  it("selects rows past the row's edge, then deletes them together", async () => {
    const server = fakeServer()
    render(<App client={makeClient()} />)
    await userEvent.click(await screen.findByLabelText("New line"))
    await screen.findByRole("textbox", { name: "New line" })
    await userEvent.keyboard("one{Enter}two{Enter}three{Enter}")
    await waitFor(() => expect(server.tree()).toEqual(["one", "two", "three"]))
    await userEvent.click(await screen.findByText("one"))
    await waitFor(() => expect(document.querySelector('[contenteditable="true"]')).toBeTruthy())
    await userEvent.keyboard("{Shift>}{ArrowDown}{/Shift}")
    await waitFor(() => expect(document.querySelectorAll("[data-selected]")).toHaveLength(2))
    // The selection copies as an outline and deletes as one.
    await userEvent.keyboard("{Shift>}{ArrowDown}{/Shift}")
    expect(document.querySelectorAll("[data-selected]")).toHaveLength(3)
    await userEvent.keyboard("{Shift>}{ArrowUp}{/Shift}")
    expect(document.querySelectorAll("[data-selected]")).toHaveLength(2)
    await userEvent.keyboard("{Backspace}")
    await waitFor(() => expect(server.tree()).toEqual(["three"]))
  })

  it("opens the reference menu with the subjects in use, before typing", async () => {
    fakeServer()
    render(<App client={makeClient()} />)
    await userEvent.click(await screen.findByLabelText("New line"))
    await screen.findByRole("textbox", { name: "New line" })
    await userEvent.keyboard("see @")
    expect(await screen.findByRole("option", { name: /Northwind/ })).toBeInTheDocument()
    await userEvent.keyboard("{Escape}")
    await userEvent.keyboard("{Meta>}k{/Meta}")
    await screen.findByPlaceholderText(FIND)
    expect(await screen.findByRole("option", { name: /Northwind/ })).toBeInTheDocument()
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
