import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { NodeText } from "./node-text";
import { NodeEditor } from "./node-editor";
import { Composer } from "./composer";
import { HistoryDialog } from "./history-dialog";
import type { Node } from "@/lib/api";
const node: Node = {
  id: "note-a",
  parent_id: "day",
  text: "Use [[Old project]]",
  tags: ["decision"],
  references: [
    {
      target_id: "project-a",
      label: "Renamed project",
      source_label: "Old project",
    },
  ],
  revision: 3,
  created_at: "2026-10-28T10:00:00Z",
  updated_at: "2026-10-28T10:00:00Z",
  has_children: false,
};
const response = (data: unknown, status = 200) =>
  new Response(JSON.stringify(data), {
    status,
    headers: { "Content-Type": "application/json" },
  });
describe("note content", () => {
  it("does not clear a reopened editor when an old save returns", async () => {
    let finish!: (value: Response) => void;
    vi.stubGlobal(
      "fetch",
      vi.fn().mockImplementation(
        () =>
          new Promise<Response>((resolve) => {
            finish = resolve;
          }),
      ),
    );
    const saved = vi.fn().mockResolvedValue(undefined);
    const first = render(
      <NodeEditor node={node} onSaved={saved} onClose={vi.fn()} />,
    );
    fireEvent.change(screen.getByRole("textbox", { name: "Edit bullet" }), {
      target: { value: "Old pending save" },
    });
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    first.unmount();
    render(<NodeEditor node={node} onSaved={saved} onClose={vi.fn()} />);
    fireEvent.change(screen.getByRole("textbox", { name: "Edit bullet" }), {
      target: { value: "Reopened editor draft" },
    });
    finish(
      response({ node: { ...node, text: "Old pending save", revision: 4 } }),
    );
    await waitFor(() =>
      expect(saved).toHaveBeenCalledWith(
        expect.objectContaining({ revision: 4 }),
        true,
      ),
    );
    expect(screen.getByRole("textbox", { name: "Edit bullet" })).toHaveValue(
      "Reopened editor draft",
    );
    expect(
      JSON.parse(localStorage.getItem("vrac:draft:note-a") || "{}").text,
    ).toBe("Reopened editor draft");
  });
  it("opens the next inline bullet immediately while saving the current edit", async () => {
    let finish!: (value: Response) => void;
    vi.stubGlobal(
      "fetch",
      vi.fn().mockImplementation(
        () =>
          new Promise<Response>((resolve) => {
            finish = resolve;
          }),
      ),
    );
    const next = vi.fn();
    const saved = vi.fn().mockResolvedValue(undefined);
    render(
      <NodeEditor
        node={node}
        compact
        onNext={next}
        onSaved={saved}
        onClose={vi.fn()}
      />,
    );
    fireEvent.change(screen.getByRole("textbox", { name: "Edit bullet" }), {
      target: { value: "Updated inline" },
    });
    await userEvent.keyboard("{Enter}");
    expect(next).toHaveBeenCalledWith(node, "below");
    expect(saved).not.toHaveBeenCalled();
    finish(
      response({ node: { ...node, text: "Updated inline", revision: 4 } }),
    );
    await waitFor(() => expect(saved).toHaveBeenCalled());
  });
  it("retains typing made while an earlier save is in flight", async () => {
    let finish!: (value: Response) => void;
    const fetcher = vi.fn().mockImplementationOnce(
      () =>
        new Promise<Response>((resolve) => {
          finish = resolve;
        }),
    );
    vi.stubGlobal("fetch", fetcher);
    const saved = vi.fn().mockResolvedValue(undefined);
    render(<NodeEditor node={node} onSaved={saved} onClose={vi.fn()} />);
    const field = screen.getByRole("textbox", { name: "Edit bullet" });
    fireEvent.change(field, { target: { value: "First draft" } });
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    fireEvent.change(field, { target: { value: "New typing while saving" } });
    finish(response({ node: { ...node, text: "First draft", revision: 4 } }));
    await waitFor(() =>
      expect(saved).toHaveBeenCalledWith(
        expect.objectContaining({ revision: 4 }),
        true,
      ),
    );
    expect(field).toHaveValue("New typing while saving");
    expect(
      JSON.parse(localStorage.getItem("vrac:draft:note-a") || "{}"),
    ).toMatchObject({ text: "New typing while saving", revision: 4 });
  });
  it("renders the current label of a stable reference and safely displays plain text", async () => {
    const onSubject = vi.fn();
    render(
      <NodeText
        node={{ ...node, text: "[[Old project]] <img src=x onerror=alert(1)>" }}
        onSubject={onSubject}
      />,
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Renamed project" }),
    );
    expect(onSubject).toHaveBeenCalledWith("project-a", "Renamed project");
    expect(document.querySelector("img")).toBeNull();
    expect(screen.getByText(/<img src=x/)).toBeInTheDocument();
  });
  it("keeps a failed edit draft and reuses its mutation key on a network retry", async () => {
    const fetcher = vi
      .fn()
      .mockRejectedValueOnce(new TypeError("Network unavailable"))
      .mockResolvedValueOnce(
        response({ node: { ...node, text: "Updated", revision: 4 } }),
      );
    vi.stubGlobal("fetch", fetcher);
    const saved = vi.fn().mockResolvedValue(undefined);
    render(<NodeEditor node={node} onSaved={saved} onClose={vi.fn()} />);
    fireEvent.change(screen.getByRole("textbox", { name: "Edit bullet" }), {
      target: { value: "Updated" },
    });
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Network unavailable",
    );
    expect(
      JSON.parse(localStorage.getItem("vrac:draft:note-a") || "{}").text,
    ).toBe("Updated");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(saved).toHaveBeenCalledOnce());
    const first = JSON.parse(fetcher.mock.calls[0][1].body);
    const second = JSON.parse(fetcher.mock.calls[1][1].body);
    expect(first.request_id).toBe(second.request_id);
    expect(first.expected_revision).toBe(3);
    expect(localStorage.getItem("vrac:draft:note-a")).toBeNull();
  });
  it("uses the confirmed revision and a new mutation key after a saved edit whose refresh failed", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(
        response({ node: { ...node, text: "First change", revision: 4 } }),
      )
      .mockResolvedValueOnce(
        response({ node: { ...node, text: "Second change", revision: 5 } }),
      );
    vi.stubGlobal("fetch", fetcher);
    const saved = vi
      .fn()
      .mockRejectedValueOnce(new Error("Refresh offline"))
      .mockResolvedValueOnce(undefined);
    render(<NodeEditor node={node} onSaved={saved} onClose={vi.fn()} />);
    fireEvent.change(screen.getByRole("textbox", { name: "Edit bullet" }), {
      target: { value: "First change" },
    });
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Saved successfully",
    );
    fireEvent.change(screen.getByRole("textbox", { name: "Edit bullet" }), {
      target: { value: "Second change" },
    });
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(saved).toHaveBeenCalledTimes(2));
    const first = JSON.parse(fetcher.mock.calls[0][1].body);
    const second = JSON.parse(fetcher.mock.calls[1][1].body);
    expect(second.expected_revision).toBe(4);
    expect(second.request_id).not.toBe(first.request_id);
  });
  it("preserves a conflicting draft and requires reviewing the current saved version", async () => {
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(
        response(
          {
            error: {
              code: "conflict",
              message: "This note changed in another session.",
            },
          },
          409,
        ),
      )
      .mockResolvedValueOnce(
        response({
          node: { ...node, text: "Another writer", revision: 4 },
          path: [],
          children: { nodes: [] },
        }),
      );
    vi.stubGlobal("fetch", fetcher);
    render(<NodeEditor node={node} onSaved={vi.fn()} onClose={vi.fn()} />);
    fireEvent.change(screen.getByRole("textbox", { name: "Edit bullet" }), {
      target: { value: "My draft" },
    });
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await userEvent.click(
      await screen.findByRole("button", { name: "Review current version" }),
    );
    expect(await screen.findByText("Another writer")).toBeInTheDocument();
    expect(screen.getByRole("textbox", { name: "Edit bullet" })).toHaveValue(
      "My draft",
    );
    expect(
      JSON.parse(localStorage.getItem("vrac:draft:note-a") || "{}").revision,
    ).toBe(4);
  });
});
describe("capture", () => {
  it("preserves a reopened draft when an older creation is acknowledged", async () => {
    let acknowledge!: (value: Response) => void;
    vi.stubGlobal(
      "fetch",
      vi.fn(
        () =>
          new Promise<Response>((resolve) => {
            acknowledge = resolve;
          }),
      ),
    );
    const props = {
      target: { kind: "capture" as const, date: "2026-11-03" },
      onClose: vi.fn(),
      onCreated: vi.fn().mockResolvedValue(undefined),
    };
    const first = render(<Composer {...props} />);
    fireEvent.change(screen.getByRole("textbox", { name: "Your notes" }), {
      target: { value: "First thought" },
    });
    await userEvent.click(
      screen.getByRole("button", { name: "Save to journal" }),
    );
    first.unmount();
    render(<Composer {...props} />);
    fireEvent.change(screen.getByRole("textbox", { name: "Your notes" }), {
      target: { value: "Newer thought" },
    });
    acknowledge(response({ nodes: [node] }));
    await waitFor(() => expect(props.onCreated).toHaveBeenCalled());
    const saved = JSON.parse(
      localStorage.getItem("vrac:capture:2026-11-03") || "{}",
    );
    expect(saved.bullets[0].text).toBe("Newer thought");
    expect(saved.text).toBe("Newer thought");
  });
  it("retries a legacy pending child creation with its original transport and payload", async () => {
    localStorage.setItem(
      "vrac:capture:dayanchor",
      JSON.stringify({
        text: "First line\nSecond line",
        tags: "work",
        request_id: "legacy-request",
      }),
    );
    const fetcher = vi.fn().mockResolvedValue(response({ node }));
    vi.stubGlobal("fetch", fetcher);
    const created = vi.fn().mockResolvedValue(undefined);
    render(
      <Composer
        target={{
          kind: "node",
          parent: "day",
          after: "anchor",
          label: "New bullets",
        }}
        onClose={vi.fn()}
        onCreated={created}
      />,
    );
    expect(screen.getByRole("textbox", { name: "Your notes" })).toHaveValue(
      "First line\nSecond line",
    );
    await userEvent.click(screen.getByRole("button", { name: "Save bullets" }));
    await waitFor(() => expect(created).toHaveBeenCalled());
    expect(fetcher.mock.calls[0][0]).toBe("/api/nodes");
    expect(JSON.parse(fetcher.mock.calls[0][1].body)).toEqual({
      parent_id: "day",
      text: "First line\nSecond line",
      tags: ["work"],
      placement: { after: "anchor" },
      request_id: "legacy-request",
    });
  });
  it("refreshes a confirmed outline without submitting it twice", async () => {
    const fetcher = vi.fn().mockResolvedValue(response({ nodes: [node] }));
    vi.stubGlobal("fetch", fetcher);
    const created = vi
      .fn()
      .mockRejectedValueOnce(new Error("Refresh offline"))
      .mockResolvedValueOnce(undefined);
    render(
      <Composer
        target={{ kind: "node", parent: "day", label: "New bullets" }}
        onClose={vi.fn()}
        onCreated={created}
      />,
    );
    fireEvent.change(screen.getByRole("textbox", { name: "Your notes" }), {
      target: { value: "Confirmed thought" },
    });
    await userEvent.click(screen.getByRole("button", { name: "Save bullets" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Saved, but refreshing failed",
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Show saved bullets" }),
    );
    await waitFor(() => expect(created).toHaveBeenCalledTimes(2));
    expect(fetcher).toHaveBeenCalledOnce();
    expect(fetcher.mock.calls[0][0]).toBe("/api/outline");
  });
  it("writes an outline with Enter and Tab directly in the page", async () => {
    const fetcher = vi.fn().mockResolvedValue(response({ nodes: [node] }));
    vi.stubGlobal("fetch", fetcher);
    const created = vi.fn().mockResolvedValue(undefined);
    render(
      <Composer
        target={{ kind: "capture", date: "2026-11-03" }}
        onClose={vi.fn()}
        onCreated={created}
      />,
    );
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    await userEvent.type(
      screen.getByRole("textbox", { name: "Your notes" }),
      "Meeting #meeting",
    );
    await userEvent.keyboard("{Enter}{Tab}");
    await userEvent.type(
      screen.getByRole("textbox", { name: "New bullet 2" }),
      "A decision #decision",
    );
    await userEvent.keyboard("{Enter}{Shift>}{Tab}{/Shift}");
    await userEvent.type(
      screen.getByRole("textbox", { name: "New bullet 3" }),
      "Next task #task",
    );
    await userEvent.keyboard("{Control>}{Enter}{/Control}");
    await waitFor(() => expect(created).toHaveBeenCalledOnce());
    expect(JSON.parse(fetcher.mock.calls[0][1].body).nodes).toEqual([
      {
        text: "Meeting #meeting",
        tags: [],
        children: [{ text: "A decision #decision" }],
      },
      { text: "Next task #task", tags: [] },
    ]);
  });
  it("sends a pasted tree as one atomic request and keeps tags explicit", async () => {
    const fetcher = vi.fn().mockResolvedValue(response({ nodes: [node] }));
    vi.stubGlobal("fetch", fetcher);
    const created = vi.fn().mockResolvedValue(undefined);
    render(
      <Composer
        target={{ kind: "capture", date: "2026-11-02" }}
        onClose={vi.fn()}
        onCreated={created}
      />,
    );
    fireEvent.paste(screen.getByRole("textbox", { name: "Your notes" }), {
      clipboardData: {
        getData: () =>
          "Meeting [[Atlas]] #meeting\n  Decision #decision\nNext action #task",
      },
    });
    fireEvent.change(screen.getByRole("textbox", { name: "Additional tags" }), {
      target: { value: "work" },
    });
    await userEvent.click(
      screen.getByRole("button", { name: "Save to journal" }),
    );
    await waitFor(() => expect(created).toHaveBeenCalledOnce());
    expect(fetcher).toHaveBeenCalledOnce();
    expect(fetcher.mock.calls[0][0]).toBe("/api/capture");
    const body = JSON.parse(fetcher.mock.calls[0][1].body);
    expect(body.date).toBe("2026-11-02");
    expect(body.nodes).toEqual([
      {
        text: "Meeting [[Atlas]] #meeting",
        tags: ["work"],
        children: [{ text: "Decision #decision" }],
      },
      { text: "Next action #task", tags: ["work"] },
    ]);
  });
  it("recovers capture text and the same request identity after reopening an uncertain save", async () => {
    const fetcher = vi.fn().mockRejectedValue(new TypeError("Connection lost"));
    vi.stubGlobal("fetch", fetcher);
    const first = render(
      <Composer
        target={{ kind: "capture", date: "2026-11-02" }}
        onClose={vi.fn()}
        onCreated={vi.fn()}
      />,
    );
    fireEvent.change(screen.getByRole("textbox", { name: "Your notes" }), {
      target: { value: "Keep this thought" },
    });
    await userEvent.click(
      screen.getByRole("button", { name: "Save to journal" }),
    );
    await screen.findByRole("alert");
    const request = JSON.parse(fetcher.mock.calls[0][1].body).request_id;
    first.unmount();
    render(
      <Composer
        target={{ kind: "capture", date: "2026-11-02" }}
        onClose={vi.fn()}
        onCreated={vi.fn()}
      />,
    );
    expect(screen.getByRole("textbox", { name: "Your notes" })).toHaveValue(
      "Keep this thought",
    );
    await userEvent.click(
      screen.getByRole("button", { name: "Save to journal" }),
    );
    await screen.findByRole("alert");
    expect(JSON.parse(fetcher.mock.calls[1][1].body).request_id).toBe(request);
  });
  it("keeps an invalid outline local and sends no partial request", async () => {
    const fetcher = vi.fn();
    vi.stubGlobal("fetch", fetcher);
    render(
      <Composer
        target={{ kind: "capture", date: "2026-11-02" }}
        onClose={vi.fn()}
        onCreated={vi.fn()}
      />,
    );
    fireEvent.paste(screen.getByRole("textbox", { name: "Your notes" }), {
      clipboardData: { getData: () => "Parent\n    Child\n  Broken" },
    });
    await userEvent.click(
      screen.getByRole("button", { name: "Save to journal" }),
    );
    expect(await screen.findByRole("alert")).toHaveTextContent("Line 3");
    expect(fetcher).not.toHaveBeenCalled();
  });
});

it("keeps older history pages reachable", async () => {
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(
      response({
        revisions: [{ revision: 3, text: "Current state", tags: [] }],
        next_cursor: "older",
      }),
    )
    .mockResolvedValueOnce(
      response({
        revisions: [{ revision: 2, text: "Earlier state", tags: [] }],
      }),
    );
  vi.stubGlobal("fetch", fetcher);
  render(<HistoryDialog node={node} onClose={vi.fn()} onRestored={vi.fn()} />);
  await userEvent.click(
    await screen.findByRole("button", { name: "Load older revisions" }),
  );
  expect(await screen.findByText("Earlier state")).toBeInTheDocument();
  expect(screen.getByText("Current state")).toBeInTheDocument();
  expect(fetcher.mock.calls[1][0]).toContain("cursor=older");
});
