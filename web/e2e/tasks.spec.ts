import { expect, test } from "@playwright/test"

const headers = { "X-Vrac-Client": "e2e" }

test("the tasks view groups open to-dos by project and sets waiting ones apart", async ({ page }) => {
  const post = async (path: string, data: object) => {
    const res = await page.request.post(path, { headers, data: { request_id: crypto.randomUUID(), ...data } })
    expect(res.ok(), await res.text()).toBeTruthy()
  }
  for (const name of ["waiting", "next", "someday"]) await post("/api/v1/types", { name })
  await post("/api/v1/nodes", {
    nodes: [{ input: "Globex #project", children: [{ input: "Send the estimate #task" }] }, { input: "Fix the gate #task" }],
    create_types: ["project"],
  })
  await post("/api/v1/nodes", {
    nodes: [
      { input: "Answer from [[Globex]] on the estimate #task #waiting" },
      { input: "Call the bank #task #next" },
      { input: "Learn to sail #task #someday" },
    ],
  })

  await page.goto("/tasks")
  const globex = page.locator("section").filter({ has: page.getByRole("heading", { name: /^Globex/ }) })
  await expect(globex.getByText("Send the estimate")).toBeVisible()
  const waiting = page.locator("section").filter({ hasText: "Waiting" })
  await expect(waiting.getByText("Answer from")).toBeVisible()
  await expect(page.locator("section").filter({ hasText: "Other" }).getByText("Fix the gate")).toBeVisible()
  await expect(page.locator("section").filter({ hasText: "Next" }).getByText("Call the bank")).toBeVisible()
  await expect(page.locator("section").filter({ hasText: "Someday" }).getByText("Learn to sail")).toBeVisible()

  // Checking a to-do takes it off the view.
  await globex.getByRole("checkbox", { name: "Done" }).click()
  await expect(page.getByText("Send the estimate")).toBeHidden()
})
