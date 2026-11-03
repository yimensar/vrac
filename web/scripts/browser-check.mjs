import assert from "node:assert/strict";

// Run against a disposable --dev instance. This script never takes screenshots.
// See README.md for installing Playwright outside the source tree.
const { chromium } = await import(
  process.env.VRAC_PLAYWRIGHT_MODULE || "playwright"
);
const base = process.env.VRAC_TEST_URL || "http://127.0.0.1:18086";
const browser = await chromium.launch({
  headless: true,
  ...(process.env.VRAC_BROWSER_BINARY
    ? { executablePath: process.env.VRAC_BROWSER_BINARY }
    : {}),
});
const runID = Date.now().toString(36);
const results = [];
try {
  for (const [name, viewport] of [
    ["desktop", { width: 1440, height: 1000 }],
    ["tablet", { width: 1024, height: 900 }],
    ["phone", { width: 390, height: 844 }],
  ]) {
    const context = await browser.newContext({ viewport });
    const page = await context.newPage();
    const errors = [];
    page.on("pageerror", (error) => errors.push(error.message));
    await page.goto(base, { waitUntil: "networkidle" });
    await page.getByRole("heading", { level: 1 }).waitFor();
    const project = `Atlas ${name} ${runID}`;
    const meeting = `Release meeting ${name} ${runID}`;
    const decision = `Keep SSO ${name} ${runID}`;
    await page.getByRole("button", { name: /^Capture/ }).click();
    assert.equal(await page.getByRole("dialog").count(), 0);
    await page
      .getByRole("textbox", { name: "Your notes" })
      .fill(`${meeting} [[${project}]] #meeting`);
    await page.getByRole("textbox", { name: "Your notes" }).press("Enter");
    if (name === "phone")
      await page.getByRole("button", { name: "Indent new bullet" }).click();
    else await page.keyboard.press("Tab");
    await page
      .getByRole("textbox", { name: "New bullet 2", exact: true })
      .fill(`${decision} #decision`);
    await page
      .getByRole("textbox", { name: "New bullet 2", exact: true })
      .press("Enter");
    await page
      .getByRole("textbox", { name: "New bullet 3", exact: true })
      .fill("Prepare accounts #task");
    const captured = page.waitForResponse(
      (response) =>
        response.url().endsWith("/api/capture") &&
        response.request().method() === "POST",
    );
    await page.getByRole("button", { name: "Save to journal" }).click();
    const captureResponse = await captured;
    assert.equal(captureResponse.status(), 201, await captureResponse.text());
    const captureBody = await captureResponse.json();
    const root = captureBody.nodes[0];
    await page
      .getByRole("group", { name: "New outline" })
      .waitFor({ state: "hidden" });
    const row = page.locator(`[data-node-id="${root.id}"]`).first();
    await row.waitFor();
    await row
      .getByRole("button", { name: "Expand bullet", exact: true })
      .first()
      .click();
    await row.getByText(decision, { exact: true }).waitFor();
    assert.equal(
      await row.locator(".tag-link").filter({ hasText: "#decision" }).count(),
      1,
    );
    await row
      .getByRole("button", { name: "Bullet actions", exact: true })
      .first()
      .click();
    await page
      .getByRole("menuitem", { name: "Add child", exact: true })
      .click();
    assert.equal(await page.getByRole("dialog").count(), 0);
    const extraText = `Extra ${name} ${runID}`;
    await page.getByRole("textbox", { name: "Your notes" }).fill(extraText);
    await page.getByRole("textbox", { name: "Your notes" }).press("Enter");
    await page
      .getByRole("textbox", { name: "New bullet 2", exact: true })
      .fill("Another sibling");
    const inserted = page.waitForResponse(
      (response) =>
        response.url().endsWith("/api/outline") &&
        response.request().method() === "POST",
    );
    await page.getByRole("button", { name: "Save bullets" }).click();
    const insertion = await inserted;
    assert.equal(insertion.status(), 201);
    const extra = (await insertion.json()).nodes[0];
    await page
      .getByRole("group", { name: "New outline" })
      .waitFor({ state: "hidden" });
    const extraRow = page.locator(`[data-node-id="${extra.id}"]`).first();
    await extraRow.getByText(extraText, { exact: true }).click();
    const inline = extraRow.getByRole("textbox", { name: "Edit bullet" });
    await inline.fill(extraText + " updated");
    let release;
    const held = new Promise((resolve) => {
      release = resolve;
    });
    await page.route(`**/api/nodes/${extra.id}`, async (route) => {
      if (route.request().method() !== "PATCH") return route.continue();
      const response = await route.fetch();
      await held;
      await route.fulfill({ response });
    });
    const changed = page.waitForResponse(
      (response) =>
        response.url().endsWith(`/api/nodes/${extra.id}`) &&
        response.request().method() === "PATCH",
    );
    await inline.press("Enter");
    const followup = page.getByRole("textbox", { name: "Your notes" });
    await followup.waitFor();
    await followup.fill("Keep typing while the previous bullet saves");
    release();
    assert.equal((await changed).status(), 200);
    assert.equal(
      await followup.inputValue(),
      "Keep typing while the previous bullet saves",
    );
    const continued = page.waitForResponse(
      (response) =>
        response.url().endsWith("/api/outline") &&
        response.request().method() === "POST",
    );
    await page.getByRole("button", { name: "Save bullets" }).click();
    assert.equal((await continued).status(), 201);
    await page
      .getByRole("group", { name: "New outline" })
      .waitFor({ state: "hidden" });
    await row.getByRole("button", { name: project, exact: true }).click();
    await page
      .locator(".search-result")
      .filter({ hasText: decision })
      .waitFor();
    await page
      .getByRole("combobox", { name: "Filter by tag" })
      .selectOption("decision");
    await page.waitForResponse(
      (response) =>
        response.url().includes("/api/search?") &&
        response.url().includes("tag=decision"),
    );
    await page
      .locator(".search-result")
      .filter({ hasText: decision })
      .getByRole("button", { name: `Open ${decision}` })
      .click();
    await page.waitForURL("**/#/node/*");
    const permalink = page.url();
    await page.reload({ waitUntil: "networkidle" });
    assert.equal(page.url(), permalink);
    await page.getByRole("heading", { name: decision, exact: true }).waitFor();
    await page.getByRole("button", { name: "Edit this note" }).click();
    await page
      .getByRole("textbox", { name: "Edit bullet" })
      .fill(`${decision} — clarified`);
    const updated = page.waitForResponse(
      (response) =>
        response.url().includes("/api/nodes/") &&
        response.request().method() === "PATCH",
    );
    await page.getByRole("button", { name: "Save", exact: true }).click();
    assert.equal((await updated).status(), 200);
    await page
      .getByRole("heading", { name: `${decision} — clarified`, exact: true })
      .waitFor();
    await page.getByRole("button", { name: "History", exact: true }).click();
    const firstRevision = page
      .locator(".revision-card")
      .filter({ has: page.locator("strong", { hasText: /^Revision 1$/ }) });
    await firstRevision.waitFor();
    page.once("dialog", (dialog) => dialog.accept());
    const restored = page.waitForResponse((response) =>
      response.url().endsWith("/restore"),
    );
    await firstRevision
      .getByRole("button", { name: "Restore", exact: true })
      .click();
    assert.equal((await restored).status(), 200);
    await page.getByRole("heading", { name: decision, exact: true }).waitFor();
    await page.getByRole("dialog").waitFor({ state: "hidden" });
    const overflow = await page.evaluate(() => ({
      scroll: document.documentElement.scrollWidth,
      width: window.innerWidth,
    }));
    assert.ok(
      overflow.scroll <= overflow.width + 1,
      `${name}: horizontal overflow ${JSON.stringify(overflow)}`,
    );
    if (name === "phone") {
      await page.getByRole("button", { name: "Open navigation" }).click();
      await page
        .getByRole("dialog")
        .getByRole("button", { name: /Journal/ })
        .click();
      await page.getByRole("dialog").waitFor({ state: "hidden" });
      await page.getByRole("heading", { level: 1 }).waitFor();
    }
    assert.deepEqual(errors, [], `${name}: browser runtime errors`);
    results.push({
      viewport: name,
      capture: true,
      inlineChildren: true,
      immediateContinuation: true,
      nestedTags: true,
      subjectFilter: true,
      permalink: true,
      edit: true,
      restore: true,
      noHorizontalOverflow: true,
    });
    await context.close();
  }
  console.log(JSON.stringify({ ok: true, results }, null, 2));
} finally {
  await browser.close();
}
