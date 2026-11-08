import { describe, expect, it } from "vitest"
import { render } from "@testing-library/react"
import { QueryClientProvider } from "@tanstack/react-query"
import { makeClient } from "@/App"
import { Content } from "./content"

const wrapper = ({ children }: { children: React.ReactNode }) => <QueryClientProvider client={makeClient()}>{children}</QueryClientProvider>

describe("content", () => {
  it("renders inline Markdown with offsets a click can use", () => {
    const { container } = render(<Content input="Buy **milk** and `eggs` at https://shop.example/a. Done" links />)
    expect(container.querySelector("strong")).toHaveTextContent("milk")
    expect(container.querySelector("code")).toHaveTextContent("eggs")
    const a = container.querySelector("a")!
    expect(a.getAttribute("href")).toBe("https://shop.example/a")
    expect(a).toHaveTextContent("https://shop.example/a")
    // The caret lands inside the bold word, after the markers.
    expect(container.querySelector("strong span")!.getAttribute("data-start")).toBe("6")
    expect(container.textContent).toContain(". Done")
  })

  it("renders lines starting with | as a table, the first line naming the node", () => {
    const { container } = render(<Content input={"Prices #reference\n| TV | Price |\n|---|---|\n| **77OLED811** | 2199 € |"} />, { wrapper })
    expect(container.querySelectorAll("th")).toHaveLength(2)
    expect(container.querySelector("th")).toHaveTextContent("TV")
    expect(container.querySelectorAll("td")).toHaveLength(2)
    expect(container.querySelector("td strong")).toHaveTextContent("77OLED811")
    expect(container.textContent).toContain("Prices")
  })
})
