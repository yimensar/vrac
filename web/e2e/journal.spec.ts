import { expect, test, type APIRequestContext, type Page } from "@playwright/test"

// The editor against a real server: what is typed must reach the database,
// in the right place, and come back after a reload.

type Node = { id: string; parent_id: string; text: string; types: string[]; status?: string }

async function todayTree(request: APIRequestContext): Promise<Node[]> {
  const today = await (await request.get("/api/v1/today")).json()
  const all: Node[] = []
  const walk = async (nodes: Node[]) => {
    for (const n of nodes) {
      all.push(n)
      const children = await (await request.get(`/api/v1/nodes/${n.id}/children`)).json()
      await walk(children.nodes)
    }
  }
  await walk(today.day.children.nodes)
  return all
}

async function node(request: APIRequestContext, text: string): Promise<Node> {
  let found: Node | undefined
  await expect.poll(async () => (found = (await todayTree(request)).find((n) => n.text === text))).toBeTruthy()
  return found!
}

const row = (page: Page, text: string) => page.getByRole("textbox", { name: "Edit" }).filter({ hasText: text })

test.beforeEach(async ({ page }) => {
  await page.goto("/")
  await page.getByRole("textbox", { name: "New line" }).click()
})

test("lines typed in the journal are saved in order", async ({ page, request }) => {
  await page.keyboard.type("Order one")
  await page.keyboard.press("Enter")
  await page.keyboard.type("Order two")
  await page.keyboard.press("Enter")
  await node(request, "Order two")
  const texts = (await todayTree(request)).map((n) => n.text)
  expect(texts.indexOf("Order one")).toBeLessThan(texts.indexOf("Order two"))

  await page.reload()
  await expect(row(page, "Order one")).toBeVisible()
  await expect(row(page, "Order two")).toBeVisible()
})

test("Tab nests the line under the one above", async ({ page, request }) => {
  await page.keyboard.type("Nest parent")
  await page.keyboard.press("Enter")
  await page.keyboard.type("Nest child")
  await page.keyboard.press("Tab")
  await page.keyboard.press("Enter")
  const parent = await node(request, "Nest parent")
  const child = await node(request, "Nest child")
  expect(child.parent_id).toBe(parent.id)
})

test("an edited line keeps its edit", async ({ page, request }) => {
  await page.keyboard.type("Edit me")
  await page.keyboard.press("Enter")
  await node(request, "Edit me")
  await row(page, "Edit me").click()
  await page.keyboard.press("End")
  await page.keyboard.type(" now")
  await page.keyboard.press("Escape")
  await node(request, "Edit me now")

  await page.reload()
  await expect(row(page, "Edit me now")).toBeVisible()
})

test("a #task line is a to-do that can be checked", async ({ page, request }) => {
  await page.keyboard.type("Call the bank #task")
  await page.keyboard.press("Enter") // picks the suggested tag
  await page.keyboard.press("Enter")
  const task = await node(request, "Call the bank #task")
  expect(task.types).toEqual(["task"])
  expect(task.status).toBe("open")

  await page.getByRole("checkbox", { name: "Done" }).last().click()
  await expect.poll(async () => (await node(request, "Call the bank #task")).status).toBe("done")
})
