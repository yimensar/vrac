import { test as setup, expect } from "@playwright/test"

// Creates the owner through the first-run form, with a virtual passkey, and
// keeps the session for the other tests.
setup("create the owner", async ({ page }) => {
  const cdp = await page.context().newCDPSession(page)
  await cdp.send("WebAuthn.enable")
  await cdp.send("WebAuthn.addVirtualAuthenticator", {
    options: { protocol: "ctap2", transport: "internal", hasResidentKey: true, hasUserVerification: true, isUserVerified: true, automaticPresenceSimulation: true },
  })
  await page.goto("/")
  await page.getByLabel("Username").fill("owner")
  await page.getByLabel("Password", { exact: true }).fill("correct horse battery")
  await page.getByLabel("Confirm the password").fill("correct horse battery")
  await page.getByRole("button", { name: "Create the account and its passkey" }).click()
  await expect(page.getByLabel("Username")).toBeHidden()
  // A fresh workspace has no tags; the tests use a to-do tag, as most do.
  const created = await page.request.post("/api/v1/types", {
    headers: { "X-Vrac-Client": "e2e" },
    data: { request_id: crypto.randomUUID(), name: "task", todo: true },
  })
  expect(created.ok()).toBeTruthy()
  await page.context().storageState({ path: "../tmp/e2e/state.json" })
})
