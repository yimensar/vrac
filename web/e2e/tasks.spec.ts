import { expect, test } from "@playwright/test"

const headers = { "X-Vrac-Client": "e2e" }

test("the tasks view groups open to-dos by project or by status", async ({ page }) => {
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

  const section = (name: RegExp) => page.locator("section").filter({ has: page.getByRole("heading", { name }) })

  // By project, every to-do stays under its project, whatever its status.
  await page.goto("/tasks")
  const globex = section(/^Globex/)
  await expect(globex.getByText("Send the estimate")).toBeVisible()
  await expect(globex.getByText("Answer from")).toBeVisible()
  for (const text of ["Fix the gate", "Call the bank", "Learn to sail"]) await expect(section(/^Other/).getByText(text)).toBeVisible()

  // Checking a to-do takes it off the view.
  await globex.getByRole("checkbox", { name: "Done" }).first().click()
  await expect(page.getByText("Send the estimate")).toBeHidden()

  // Without a group tag, by status.
  const prefs = await (await page.request.get("/api/v1/preferences", { headers })).json()
  expect((await page.request.put("/api/v1/preferences", { headers, data: { ...prefs, tasks_group: "" } })).ok()).toBeTruthy()
  await page.reload()
  await expect(section(/^Next/).getByText("Call the bank")).toBeVisible()
  await expect(section(/^Waiting/).getByText("Answer from")).toBeVisible()
  await expect(section(/^Open/).getByText("Fix the gate")).toBeVisible()
  await expect(section(/^Someday/).getByText("Learn to sail")).toBeVisible()
})
