import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import App from "./App";
import type { Node } from "./lib/api";
const day: Node = {
  id: "day",
  text: "2026-11-02",
  tags: [],
  references: [],
  revision: 1,
  created_at: "2026-11-02T00:00:00Z",
  updated_at: "2026-11-02T00:00:00Z",
  has_children: true,
  system: "journal_day",
  journal_date: "2026-11-02",
};
const note: Node = {
  ...day,
  id: "meeting",
  parent_id: "day",
  text: "Meeting [[Atlas]]",
  tags: ["meeting"],
  references: [{ target_id: "atlas", label: "Atlas" }],
  system: undefined,
  journal_date: undefined,
};
const decision: Node = {
  ...note,
  id: "decision",
  parent_id: "meeting",
  text: "A useful decision",
  tags: ["decision"],
  has_children: false,
};
const response = (data: unknown, status = 200) =>
  new Response(JSON.stringify(data), {
    status,
    headers: { "Content-Type": "application/json" },
  });
function backend(
  extra?: (
    url: string,
    init?: RequestInit,
  ) => Response | Promise<Response> | undefined,
) {
  const fetcher = vi.fn(
    async (input: string | URL | Request, init?: RequestInit) => {
      const url = String(input);
      const custom = extra?.(url, init);
      if (custom) return custom;
      if (url.startsWith("/api/bootstrap"))
        return response({
          today: day,
          journal: {
            ...day,
            id: "journal",
            text: "Journal",
            system: "journal",
          },
          date: "2026-11-02",
          timezone: "Europe/Paris",
          tags: ["decision", "meeting"],
        });
      if (url === "/api/nodes/day")
        return response({
          node: day,
          path: [day],
          children: { nodes: [note], next_cursor: "second-page" },
        });
      if (url.startsWith("/api/nodes/day/children"))
        return response({
          nodes: [
            {
              ...decision,
              id: "later",
              parent_id: "day",
              text: "A later root bullet",
            },
          ],
        });
      if (url.startsWith("/api/nodes/meeting/children"))
        return response({ nodes: [decision] });
      if (url.startsWith("/api/search"))
        return response({
          items: [{ node: decision, path: [day, note, decision] }],
        });
      return response({ tags: ["decision", "meeting"] });
    },
  );
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}
beforeEach(() => {
  window.history.replaceState(null, "", "/");
});
describe("journal application", () => {
  it("opens the real journal, lazily expands a branch, and exposes the next root page", async () => {
    const fetcher = backend();
    render(<App />);
    await screen.findByText("Meeting");
    expect(
      fetcher.mock.calls.some(([url]) =>
        String(url).includes("/meeting/children"),
      ),
    ).toBe(false);
    await userEvent.click(
      screen.getByRole("button", { name: "Expand bullet" }),
    );
    expect(await screen.findByText("A useful decision")).toBeInTheDocument();
    await userEvent.click(
      screen.getByRole("button", { name: "Load more bullets" }),
    );
    expect(await screen.findByText("A later root bullet")).toBeInTheDocument();
    expect(screen.getByText("Meeting")).toBeInTheDocument();
    expect(
      fetcher.mock.calls.some(([url]) =>
        String(url).includes("cursor=second-page"),
      ),
    ).toBe(true);
  });
  it("combines tag and date filters in server queries", async () => {
    const fetcher = backend();
    render(<App />);
    await screen.findByText("Meeting");
    await userEvent.click(screen.getByRole("button", { name: "decision" }));
    await screen.findByText("A useful decision");
    fireEvent.change(screen.getByLabelText("From date"), {
      target: { value: "2026-10-01" },
    });
    fireEvent.change(screen.getByLabelText("To date"), {
      target: { value: "2026-11-02" },
    });
    await waitFor(() =>
      expect(
        fetcher.mock.calls.some(
          ([url]) =>
            String(url).includes("tag=decision") &&
            String(url).includes("from=2026-10-01") &&
            String(url).includes("to=2026-11-02"),
        ),
      ).toBe(true),
    );
  });
  it("opens references as a project scope and uses stable permalinks", async () => {
    const fetcher = backend();
    render(<App />);
    await userEvent.click(await screen.findByRole("button", { name: "Atlas" }));
    await screen.findByText("A useful decision");
    expect(window.location.hash).toBe("#/subject/atlas");
    expect(
      fetcher.mock.calls.some(([url]) => String(url).includes("subject=atlas")),
    ).toBe(true);
  });
  it("ignores search results from an older query", async () => {
    let finishOld: ((value: Response) => void) | undefined;
    const fetcher = backend((url) => {
      if (url.includes("/search?") && url.includes("q=old"))
        return new Promise<Response>((resolve) => {
          finishOld = resolve;
        });
      if (url.includes("/search?") && url.includes("q=new"))
        return response({
          items: [{ node: { ...decision, text: "Newest answer" }, path: [] }],
        });
    });
    render(<App />);
    await screen.findByText("Meeting");
    await userEvent.click(screen.getByRole("button", { name: /Search.*K/ }));
    fireEvent.change(screen.getByRole("textbox", { name: "Search notes" }), {
      target: { value: "old" },
    });
    await waitFor(() =>
      expect(
        fetcher.mock.calls.some(([url]) => String(url).includes("q=old")),
      ).toBe(true),
    );
    fireEvent.change(screen.getByRole("textbox", { name: "Search notes" }), {
      target: { value: "new" },
    });
    expect(await screen.findByText("Newest answer")).toBeInTheDocument();
    await act(async () => {
      finishOld?.(
        response({
          items: [{ node: { ...decision, text: "Stale answer" }, path: [] }],
        }),
      );
    });
    expect(screen.queryByText("Stale answer")).not.toBeInTheDocument();
    expect(screen.getByText("Newest answer")).toBeInTheDocument();
  });
  it("signs in with a session and never stores the access token", async () => {
    let signedIn = false;
    const fetcher = backend((url, init) => {
      if (url === "/api/session") {
        signedIn = true;
        expect(JSON.parse(String(init?.body)).token).toBe("secret-token");
        return response({ ok: true });
      }
      if (url.startsWith("/api/bootstrap") && !signedIn)
        return response(
          { error: { code: "unauthorized", message: "Sign in required" } },
          401,
        );
    });
    render(<App />);
    await userEvent.type(
      await screen.findByLabelText("Access token"),
      "secret-token",
    );
    await userEvent.click(screen.getByRole("button", { name: "Open Vrac" }));
    expect(await screen.findByText("Meeting")).toBeInTheDocument();
    expect(
      fetcher.mock.calls.some(([url]) => String(url) === "/api/session"),
    ).toBe(true);
    expect(JSON.stringify(localStorage)).not.toContain("secret-token");
  });
});

it("archives a leaf with a stable retry key and restores using the fresh history revision", async () => {
  const leaf: Node = {
    ...decision,
    id: "accidental",
    parent_id: "day",
    text: "Accidental thought",
    revision: 3,
  };
  let archived = false;
  let archiveAttempts = 0;
  let restoreBody: { expected_revision?: number } | undefined;
  vi.spyOn(window, "confirm").mockReturnValue(true);
  const fetcher = backend((url, init) => {
    if (url === "/api/nodes/day")
      return response({ node: day, path: [day], children: { nodes: [leaf] } });
    if (url.startsWith("/api/nodes/day/children"))
      return response({ nodes: archived ? [] : [leaf] });
    if (url === "/api/nodes/accidental" && init?.method === "DELETE") {
      archived = true;
      archiveAttempts += 1;
      if (archiveAttempts === 1)
        throw new TypeError("Connection lost after archive");
      return response({ ok: true });
    }
    if (url === "/api/nodes/accidental/revisions")
      return response({
        revisions: [
          {
            revision: 4,
            text: leaf.text,
            tags: leaf.tags,
            node: { ...leaf, revision: 4, deleted: true },
          },
          { revision: 3, text: leaf.text, tags: leaf.tags, node: leaf },
        ],
      });
    if (url === "/api/nodes/accidental/restore") {
      restoreBody = JSON.parse(String(init?.body));
      archived = false;
      return response({ node: { ...leaf, revision: 5 } });
    }
  });
  render(<App />);
  await screen.findByText("Accidental thought");
  await userEvent.click(screen.getByRole("button", { name: "Bullet actions" }));
  await userEvent.click(
    await screen.findByRole("menuitem", { name: "Archive bullet" }),
  );
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "Connection lost after archive",
  );
  await userEvent.click(screen.getByRole("button", { name: "Bullet actions" }));
  await userEvent.click(
    await screen.findByRole("menuitem", { name: "Archive bullet" }),
  );
  await screen.findByRole("heading", { name: "Archived note" });
  await screen.findByText("Archived", { exact: true });
  expect(document.querySelector('[data-node-id="accidental"]')).toBeNull();
  await userEvent.click(screen.getByRole("button", { name: "Restore" }));
  await waitFor(() =>
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
  );
  expect(
    document.querySelector('[data-node-id="accidental"]'),
  ).toBeInTheDocument();
  expect(restoreBody?.expected_revision).toBe(4);
  const archives = fetcher.mock.calls.filter(
    ([, init]) => init?.method === "DELETE",
  );
  expect(archives).toHaveLength(2);
  expect(JSON.parse(String(archives[0][1]?.body)).request_id).toBe(
    JSON.parse(String(archives[1][1]?.body)).request_id,
  );
});

it("continues the tag dictionary beyond the bootstrap page", async () => {
  const fetcher = backend((url) => {
    if (url.startsWith("/api/bootstrap"))
      return response({
        today: day,
        journal: { ...day, id: "journal", system: "journal" },
        date: "2026-11-02",
        timezone: "Europe/Paris",
        tags: ["decision", "meeting"],
        tags_next_cursor: "tags-page-2",
      });
    if (url === "/api/tags?cursor=tags-page-2")
      return response({ tags: ["zebra"] });
  });
  render(<App />);
  await userEvent.click(
    await screen.findByRole("button", { name: "Load more tags" }),
  );
  expect(
    await screen.findByRole("button", { name: "zebra" }),
  ).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "decision" })).toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "Load more tags" }),
  ).not.toBeInTheDocument();
  expect(
    fetcher.mock.calls.some(
      ([url]) => String(url) === "/api/tags?cursor=tags-page-2",
    ),
  ).toBe(true);
});

it("refreshes explicit today actions after midnight while preserving an editor and the viewed journal day", async () => {
  let serverToday = "2026-11-02";
  const fetcher = backend((url) => {
    if (url.startsWith("/api/bootstrap")) {
      const selected =
        new URL(url, "http://localhost").searchParams.get("date") ||
        serverToday;
      const selectedDay = {
        ...day,
        id: selected === "2026-11-02" ? "day" : "new-day",
        text: selected,
        journal_date: selected,
      };
      return response({
        today: selectedDay,
        journal: { ...day, id: "journal", system: "journal" },
        date: selected,
        timezone: "Europe/Paris",
        tags: ["meeting"],
      });
    }
    if (url === "/api/nodes/new-day")
      return response({
        node: {
          ...day,
          id: "new-day",
          text: serverToday,
          journal_date: serverToday,
        },
        path: [],
        children: { nodes: [] },
      });
  });
  render(<App />);
  await screen.findByText("Meeting");
  await userEvent.click(screen.getByRole("button", { name: "Edit bullet" }));
  fireEvent.change(screen.getByRole("textbox", { name: "Edit bullet" }), {
    target: { value: "An unfinished meeting draft" },
  });
  serverToday = "2026-11-03";
  fireEvent.keyDown(window, { key: "Enter", ctrlKey: true, shiftKey: true });
  await screen.findByText(/Add to 2026-11-03/);
  expect(
    document.querySelector<HTMLTextAreaElement>(
      'textarea[aria-label="Edit bullet"]',
    )?.value,
  ).toBe("An unfinished meeting draft");
  expect(document.querySelector<HTMLInputElement>("#journal-date")?.value).toBe(
    "2026-11-02",
  );
  expect(document.querySelector(".today-chip")).toBeNull();
  await userEvent.keyboard("{Escape}");
  await waitFor(() =>
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
  );
  await userEvent.click(screen.getByRole("button", { name: /^Capture/ }));
  await screen.findByText(/Add to 2026-11-02/);
  await userEvent.keyboard("{Escape}");
  await waitFor(() =>
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
  );
  await userEvent.click(screen.getByRole("button", { name: /Journal.*Today/ }));
  await waitFor(() =>
    expect(screen.getByLabelText("Journal date")).toHaveValue("2026-11-03"),
  );
  expect(document.querySelector(".today-chip")).toHaveTextContent("Today");
  expect(
    JSON.parse(localStorage.getItem("vrac:draft:meeting") || "{}").text,
  ).toBe("An unfinished meeting draft");
  serverToday = "2026-11-04";
  await userEvent.click(screen.getByRole("button", { name: /Search.*K/ }));
  await userEvent.click(screen.getByRole("button", { name: /^Capture/ }));
  await screen.findByText(/Add to 2026-11-04/);
  expect(document.querySelector(".search-heading h1")).toHaveTextContent(
    "Search",
  );
  expect(
    fetcher.mock.calls.filter(([url]) => String(url) === "/api/bootstrap")
      .length,
  ).toBeGreaterThanOrEqual(4);
});
