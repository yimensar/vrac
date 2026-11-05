import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { App, makeClient } from "./App"
import { fakeServer } from "./test-server"

beforeEach(() => window.history.pushState({}, "", "/"))
afterEach(() => vi.unstubAllGlobals())

const focused = () => document.activeElement as HTMLTextAreaElement

async function start() {
  const server = fakeServer()
  render(<App client={makeClient()} />)
  const ghost = await screen.findByPlaceholderText(/Write…/)
  return { server, ghost }
}

describe("sign-in", () => {
  it("asks for the password when signed out", async () => {
    const server = fakeServer({ authenticated: false })
    render(<App client={makeClient()} />)
    await userEvent.type(await screen.findByLabelText("Password"), "correct horse battery")
    await userEvent.click(screen.getByRole("button", { name: "Sign in" }))
    await waitFor(() => expect(server.calls.some((c) => c.url === "/api/v1/login" && c.body.password === "correct horse battery")).toBe(true))
  })
})

describe("writing", () => {
  it("captures several lines in a row without losing focus", async () => {
    const { server, ghost } = await start()
    await userEvent.click(ghost)
    await userEvent.keyboard("first{Enter}second{Enter}")
    await waitFor(() => expect(server.tree()).toEqual(["first", "second"]))
    expect(focused()).toBe(screen.getByPlaceholderText(/Write…/))
    await userEvent.keyboard("third")
    expect(focused().value).toBe("third")
  })

  it("completes references and types", async () => {
    const { server, ghost } = await start()
    await userEvent.type(ghost, "Call [[[[Su")
    await userEvent.click(await screen.findByRole("option", { name: /Northwind/ }))
    await userEvent.keyboard("#ta{Enter}{Enter}")
    await waitFor(() => expect(server.calls.some((c) => c.url === "/api/v1/nodes")).toBe(true))
    const create = server.calls.find((c) => c.url === "/api/v1/nodes")!
    expect(create.body.placement).toMatchObject({ parent_id: expect.stringMatching(/^day/), first: true })
    expect(create.body.nodes[0].input).toBe("Call [[Northwind|northwindaaaaaaaaaaaaaaaaa]] #task")
  })

  it("creates an unknown reference only when asked", async () => {
    const { server, ghost } = await start()
    await userEvent.type(ghost, "Lunch with [[[[Dupont")
    await userEvent.click(await screen.findByRole("option", { name: "Create “Dupont” as #person" }))
    await userEvent.keyboard("{Enter}")
    await waitFor(() => expect(server.calls.some((c) => c.url === "/api/v1/nodes")).toBe(true))
    const create = server.calls.find((c) => c.url === "/api/v1/nodes")!
    expect(create.body.nodes[0].input).toBe("Lunch with [[Dupont]]")
    expect(create.body.create).toEqual([{ name: "Dupont", types: ["person"] }])
  })
})

describe("outline keys", () => {
  it("splits with Enter, indents with Tab and outdents with Shift+Tab", async () => {
    const { server, ghost } = await start()
    await userEvent.click(ghost)
    await userEvent.keyboard("parent{Enter}")
    await userEvent.click(await screen.findByText("parent"))
    await userEvent.keyboard("{End}{Enter}")
    expect(focused().value).toBe("")
    await userEvent.keyboard("child{Tab}")
    await waitFor(() => expect(server.tree()).toEqual(["parent", "  child"]))
    expect(focused().value).toBe("child")
    await userEvent.keyboard("{Enter}grandchild{Tab}")
    await waitFor(() => expect(server.tree()).toEqual(["parent", "  child", "    grandchild"]))
    await userEvent.keyboard("{Shift>}{Tab}{/Shift}")
    await waitFor(() => expect(server.tree()).toEqual(["parent", "  child", "  grandchild"]))
    expect(focused().value).toBe("grandchild")
  })

  it("deletes an empty row and moves to the end of the previous one", async () => {
    const { server, ghost } = await start()
    await userEvent.click(ghost)
    await userEvent.keyboard("one{Enter}")
    await userEvent.click(await screen.findByText("one"))
    await userEvent.keyboard("{End}{Enter}")
    await waitFor(() => expect(server.tree()).toEqual(["one", ""]))
    await userEvent.keyboard("{Backspace}")
    expect(focused().value).toBe("one")
    expect(focused().selectionStart).toBe(3)
    await waitFor(() => expect(server.tree()).toEqual(["one"]))
  })

  it("merges a row into the previous one with Backspace at the start", async () => {
    const { server, ghost } = await start()
    await userEvent.click(ghost)
    await userEvent.keyboard("ab{Enter}cd{Enter}")
    await userEvent.click(await screen.findByText("cd"))
    await userEvent.keyboard("{Home}{Backspace}")
    expect(focused().value).toBe("abcd")
    expect(focused().selectionStart).toBe(2)
    await waitFor(() => expect(server.tree()).toEqual(["abcd"]))
  })

  it("moves between rows with the arrow keys and saves on the way", async () => {
    const { server, ghost } = await start()
    await userEvent.click(ghost)
    await userEvent.keyboard("one{Enter}two{Enter}")
    await userEvent.keyboard("{ArrowUp}")
    expect(focused().value).toBe("two")
    await userEvent.keyboard("{End}!{ArrowUp}")
    expect(focused().value).toBe("one")
    await waitFor(() => expect(server.tree()).toEqual(["one", "two!"]))
    await userEvent.keyboard("{ArrowDown}{ArrowDown}")
    expect(focused()).toBe(screen.getByPlaceholderText(/Write…/))
  })
})
