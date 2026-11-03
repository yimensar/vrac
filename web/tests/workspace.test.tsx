import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Note } from "@/lib/api";
import App from "@/App";
import userEvent from "@testing-library/user-event";

vi.mock("@/components/Markdown", () => ({
  Markdown: ({
    value,
    onChange,
    editable,
  }: {
    value: string;
    onChange?: (v: string) => void;
    editable?: boolean;
  }) =>
    editable ? (
      <textarea
        aria-label="Contenu de la note"
        value={value}
        onChange={(e) => onChange?.(e.target.value)}
      />
    ) : (
      <div>{value}</div>
    ),
}));

const note: Note = {
  id: "test-note",
  content: "# Une idée durable\n\nLe contexte est utile. #projet",
  title: "Une idée durable",
  tags: ["projet"],
  createdAt: "2026-11-03T01:00:00Z",
  updatedAt: "2026-11-03T01:00:00Z",
  state: "active",
  pinned: false,
  version: 1,
  source: "web",
  attachments: [],
  relations: [],
};
let calls: { url: string; options?: RequestInit }[] = [];
let failCreate = false;
let conflict = false;
let pendingCreate: Promise<Response> | null = null;
let pendingPatch: Promise<Response> | null = null;
let pendingHistory: Promise<Response> | null = null;
let listNotes = [note];
const json = (data: unknown, status = 200) =>
  new Response(JSON.stringify(data), {
    status,
    headers: { "Content-Type": "application/json" },
  });
beforeEach(() => {
  calls = [];
  failCreate = false;
  conflict = false;
  pendingCreate = null;
  pendingPatch = null;
  pendingHistory = null;
  listNotes = [note];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: string | URL | Request, options?: RequestInit) => {
      const url = String(input);
      calls.push({ url, options });
      if (url === "/api/session")
        return json({
          authenticated: true,
          needsSetup: false,
          user: { name: "Yan" },
          mcp: { url: "https://example.test/mcp", oauth: true },
        });
      if (url === "/api/tags") return json([{ name: "projet", count: 1 }]);
      if (url === "/api/stats")
        return json({ active: 1, archived: 0, tags: 1, attachments: 0 });
      if (url.startsWith("/api/notes?")) {
        const params = new URL(url, "http://localhost").searchParams;
        return json({
          notes: params.get("pinned") === "true" ? [] : listNotes,
          total: params.get("pinned") === "true" ? 0 : 1,
          tookMs: 0.8,
          mode: "fts5",
        });
      }
      if (url === "/api/notes" && options?.method === "POST")
        return (
          pendingCreate ||
          (failCreate
            ? json({ error: "Storage temporarily unavailable" }, 503)
            : json(
                {
                  ...note,
                  id: "new-note",
                  content: JSON.parse(String(options.body)).content,
                },
                201,
              ))
        );
      if (url === "/api/notes/test-note" && options?.method === "PATCH")
        return (
          pendingPatch ||
          (conflict
            ? json({ error: "conflict" }, 409)
            : json({
                ...note,
                ...JSON.parse(String(options.body)),
                version: 2,
              }))
        );
      if (url.endsWith("/history")) return pendingHistory || json([]);
      const requested = listNotes.find((n) => url === `/api/notes/${n.id}`);
      if (requested) return json(requested);
      throw new Error(`Unexpected request ${url}`);
    }),
  );
});

describe("workspace behavior", () => {
  it("uses the server pinned filter rather than hiding unseen paginated notes", async () => {
    render(<App />);
    await screen.findByText(/Le contexte est utile/);
    fireEvent.click(screen.getByRole("button", { name: "Épinglées" }));
    await screen.findByText("Gardez l’essentiel à portée.");
    expect(calls.some((call) => call.url.includes("pinned=true"))).toBe(true);
  });
  it("keeps a failed capture available and retries with the same idempotency key", async () => {
    render(<App />);
    await screen.findByText(/Le contexte est utile/);
    const draft = screen.getByRole("textbox", { name: "Contenu de la note" });
    fireEvent.change(draft, {
      target: { value: "Une idée à conserver malgré une panne." },
    });
    failCreate = true;
    fireEvent.click(screen.getByRole("button", { name: "Enregistrer" }));
    await screen.findByText(/Storage temporarily unavailable/);
    expect(
      screen.getByRole("textbox", { name: "Contenu de la note" }),
    ).toHaveValue("Une idée à conserver malgré une panne.");
    failCreate = false;
    fireEvent.click(screen.getByRole("button", { name: "Enregistrer" }));
    await screen.findByText("Une idée de plus, bien gardée.");
    const writes = calls.filter(
      (call) => call.url === "/api/notes" && call.options?.method === "POST",
    );
    expect(writes).toHaveLength(2);
    const first = JSON.parse(String(writes[0].options?.body)),
      second = JSON.parse(String(writes[1].options?.body));
    expect(second.idempotencyKey).toBe(first.idempotencyKey);
    expect(
      screen.getByRole("textbox", { name: "Contenu de la note" }),
    ).toHaveValue("");
  });
  it("keeps edits visible after an optimistic concurrency conflict", async () => {
    render(<App />);
    await screen.findByText(/Le contexte est utile/);
    fireEvent.click(
      screen.getByRole("button", { name: "Ouvrir Une idée durable en focus" }),
    );
    const dialog = screen.getByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Modifier" }));
    const editor = within(dialog).getByRole("textbox", {
      name: "Contenu de la note",
    });
    fireEvent.change(editor, {
      target: { value: "A changed draft that must survive a conflict." },
    });
    conflict = true;
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Enregistrer" }),
    );
    await screen.findByText(/Cette note a changé/);
    expect(editor).toHaveValue("A changed draft that must survive a conflict.");
    const write = calls.find((call) => call.options?.method === "PATCH");
    expect(JSON.parse(String(write?.options?.body)).version).toBe(1);
  });
  it("debounces search and passes the literal user query to the API", async () => {
    render(<App />);
    await screen.findByText(/Le contexte est utile/);
    const search = screen.getByRole("textbox", {
      name: "Rechercher dans les notes",
    });
    fireEvent.change(search, { target: { value: "JWT & OAuth" } });
    await waitFor(() =>
      expect(
        calls.some(
          (call) =>
            new URL(call.url, "http://localhost").searchParams.get("q") ===
            "JWT & OAuth",
        ),
      ).toBe(true),
    );
    expect(screen.getByText(/Recherche par pertinence/)).toBeInTheDocument();
  });
  it("opens a note deep link after authentication", async () => {
    history.replaceState({}, "", "/?note=test-note");
    render(<App />);
    await waitFor(() => expect(screen.getByRole("dialog")).toBeInTheDocument());
    expect(
      within(screen.getByRole("dialog")).getByText("Une idée durable"),
    ).toBeInTheDocument();
  });
  it("locks the composer until a delayed capture finishes", async () => {
    let resolve!: (value: Response) => void;
    pendingCreate = new Promise<Response>((done) => {
      resolve = done;
    });
    render(<App />);
    await screen.findByText(/Le contexte est utile/);
    fireEvent.change(
      screen.getByRole("textbox", { name: "Contenu de la note" }),
      { target: { value: "Le texte envoyé doit rester intact." } },
    );
    fireEvent.click(screen.getByRole("button", { name: "Enregistrer" }));
    expect(
      screen.queryByRole("textbox", { name: "Contenu de la note" }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Insérer un tag" }),
    ).toBeDisabled();
    expect(
      screen.getByRole("button", { name: "Mode Markdown" }),
    ).toBeDisabled();
    await act(async () => {
      resolve(
        json({ ...note, content: "Le texte envoyé doit rester intact." }, 201),
      );
    });
    expect(
      screen.getByRole("textbox", { name: "Contenu de la note" }),
    ).toHaveValue("");
  });
  it("keeps the edited note open and locked while a save is pending", async () => {
    let resolve!: (value: Response) => void;
    pendingPatch = new Promise<Response>((done) => {
      resolve = done;
    });
    render(<App />);
    await screen.findByText(/Le contexte est utile/);
    fireEvent.click(
      screen.getByRole("button", { name: "Ouvrir Une idée durable en focus" }),
    );
    const dialog = screen.getByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Modifier" }));
    fireEvent.change(
      within(dialog).getByRole("textbox", { name: "Contenu de la note" }),
      { target: { value: "Une modification atomique." } },
    );
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Enregistrer" }),
    );
    expect(
      within(dialog).queryByRole("textbox", { name: "Contenu de la note" }),
    ).not.toBeInTheDocument();
    expect(
      within(dialog).getByRole("button", { name: "Revenir au fil" }),
    ).toBeDisabled();
    fireEvent.click(within(dialog).getByRole("button", { name: "Fermer" }));
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    await act(async () => {
      resolve(
        json({ ...note, content: "Une modification atomique.", version: 2 }),
      );
    });
    expect(
      within(screen.getByRole("dialog")).getByText(
        "Une modification atomique.",
      ),
    ).toBeInTheDocument();
  });
  it("ignores a history response after the selection has changed", async () => {
    const user = userEvent.setup();
    const other = {
      ...note,
      id: "second-note",
      title: "Deuxième idée",
      content: "# Deuxième idée",
    };
    listNotes = [note, other];
    let resolve!: (value: Response) => void;
    pendingHistory = new Promise<Response>((done) => {
      resolve = done;
    });
    render(<App />);
    await screen.findByText(/Le contexte est utile/);
    await user.click(
      screen.getByRole("button", { name: "Actions pour Une idée durable" }),
    );
    await user.click(
      await screen.findByRole("menuitem", { name: "Voir l’historique" }),
    );
    fireEvent.click(
      within(screen.getByRole("dialog")).getByRole("button", {
        name: "Revenir au fil",
      }),
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Ouvrir Deuxième idée en focus" }),
    );
    await act(async () => {
      resolve(json([{ ...note, content: "Ancienne version note A" }]));
    });
    expect(
      within(screen.getByRole("dialog")).getByRole("heading", {
        name: "Deuxième idée",
      }),
    ).toBeInTheDocument();
    expect(
      screen.queryByText("Ancienne version note A"),
    ).not.toBeInTheDocument();
  });
  it("uses a new idempotency key when a changed draft follows a lost response", async () => {
    render(<App />);
    await screen.findByText(/Le contexte est utile/);
    const originalFetch = vi.mocked(fetch).getMockImplementation()!;
    let loseNext = true;
    vi.mocked(fetch).mockImplementation(async (input, options) => {
      if (
        String(input) === "/api/notes" &&
        options?.method === "POST" &&
        loseNext
      ) {
        loseNext = false;
        calls.push({ url: String(input), options });
        throw new TypeError("Response lost");
      }
      return originalFetch(input, options);
    });
    fireEvent.change(
      screen.getByRole("textbox", { name: "Contenu de la note" }),
      { target: { value: "First uncertain intent." } },
    );
    fireEvent.click(screen.getByRole("button", { name: "Enregistrer" }));
    await screen.findByText(/Response lost/);
    fireEvent.change(
      screen.getByRole("textbox", { name: "Contenu de la note" }),
      { target: { value: "A different intent after the failed response." } },
    );
    fireEvent.click(screen.getByRole("button", { name: "Enregistrer" }));
    await screen.findByText(/Nouvelle note enregistrée/);
    const writes = calls.filter(
      (call) => call.url === "/api/notes" && call.options?.method === "POST",
    );
    expect(writes).toHaveLength(2);
    const first = JSON.parse(String(writes[0].options?.body)),
      second = JSON.parse(String(writes[1].options?.body));
    expect(second.idempotencyKey).not.toBe(first.idempotencyKey);
    expect(second.content).toBe(
      "A different intent after the failed response.",
    );
  });
});
