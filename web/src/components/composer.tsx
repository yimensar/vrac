import { useEffect, useRef, useState, type KeyboardEvent } from "react";
import {
  ArrowLeftToLine,
  ArrowRightToLine,
  Check,
  Loader2,
  Plus,
  X,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { Input } from "@/components/ui/input";
import { api, errorMessage, json, requestID, type Node } from "@/lib/api";
import { splitTags } from "@/lib/outline";
import {
  blankBullet,
  changeDepth,
  draftFromText,
  draftToNodes,
  subtreeEnd,
  type DraftBullet,
} from "@/lib/draft-outline";

export type ComposerTarget =
  | { kind: "capture"; date: string }
  | { kind: "node"; parent: string; after?: string; label: string };

type Draft = {
  bullets: DraftBullet[];
  tags: string;
  request_id: string;
  legacy?: boolean;
};

export function Composer({
  target,
  onClose,
  onCreated,
}: {
  target: ComposerTarget;
  onClose: () => void;
  onCreated: (nodes: Node[], parent?: string) => Promise<void>;
}) {
  const key = `vrac:capture:${target.kind === "capture" ? target.date : target.parent + (target.after || "")}`;
  const [initial] = useState(() => {
    let original = "";
    try {
      const raw = localStorage.getItem(key);
      if (raw) {
        original = raw;
        let data;
        try {
          data = JSON.parse(raw);
        } catch {
          data = { text: raw };
        }
        original = typeof data?.text === "string" ? data.text : raw;
        if (
          data?.bullets &&
          (!Array.isArray(data.bullets) ||
            !data.bullets.length ||
            !data.bullets.every(
              (row: DraftBullet, i: number) =>
                typeof row.id === "string" &&
                row.id &&
                typeof row.text === "string" &&
                Number.isInteger(row.depth) &&
                row.depth >= 0 &&
                (i === 0
                  ? row.depth === 0
                  : row.depth <= data.bullets[i - 1].depth + 1),
            ) ||
            new Set(data.bullets.map((row: DraftBullet) => row.id)).size !==
              data.bullets.length)
        )
          throw new Error("Invalid draft");
        const legacy =
          target.kind === "node" &&
          (data?.legacy === true ||
            (!data?.bullets && typeof data?.text === "string"));
        const bullets =
          (data?.bullets as DraftBullet[] | undefined) ||
          (legacy ? [blankBullet(0, original)] : draftFromText(original));
        return {
          draft: {
            bullets,
            legacy,
            tags: typeof data?.tags === "string" ? data.tags : "",
            request_id:
              typeof data?.request_id === "string" && data.request_id
                ? data.request_id
                : requestID(),
          } as Draft,
          error: "",
        };
      }
    } catch {
      return {
        draft: {
          bullets: [blankBullet(0, original)],
          tags: "",
          request_id: requestID(),
        },
        error: original
          ? "The previous draft could not be parsed as an outline. Its original source is below; review it before saving."
          : "Browser draft storage is unavailable. Keep this page open until your notes are saved.",
      };
    }
    return {
      draft: { bullets: [blankBullet()], tags: "", request_id: requestID() },
      error: "",
    };
  });
  const [draft, setDraft] = useState<Draft>(initial.draft);
  const [error, setError] = useState(initial.error);
  const [busy, setBusy] = useState(false);
  const [confirmed, setConfirmed] = useState<Node[] | null>(null);
  const [stored, setStored] = useState(true);
  const [active, setActive] = useState(initial.draft.bullets[0].id);
  const fields = useRef(new Map<string, HTMLTextAreaElement>());
  const focusRequest = useRef<{ id: string; cursor: number } | null>({
    id: initial.draft.bullets[0].id,
    cursor: initial.draft.bullets[0].text.length,
  });
  const pending = useRef(false);
  function keep(next: Draft) {
    setDraft(next);
    try {
      if (next.bullets.some((row) => row.text))
        localStorage.setItem(
          key,
          JSON.stringify({
            version: 2,
            ...next,
            text: next.bullets
              .map(
                (row) =>
                  `${"  ".repeat(row.depth)}${row.text.replaceAll("\n", `\n${"  ".repeat(row.depth)}`)}`,
              )
              .join("\n"),
          }),
        );
      else localStorage.removeItem(key);
      setStored(true);
    } catch {
      setStored(false);
    }
  }
  function change(bullets: DraftBullet[]) {
    keep({ ...draft, bullets, legacy: false, request_id: requestID() });
    setError("");
  }
  useEffect(() => {
    for (const field of fields.current.values()) {
      field.style.height = "auto";
      field.style.height = `${Math.max(28, field.scrollHeight)}px`;
    }
    const request = focusRequest.current;
    if (request) {
      const field = fields.current.get(request.id);
      field?.focus();
      field?.setSelectionRange(request.cursor, request.cursor);
      focusRequest.current = null;
    }
  }, [draft]);
  async function submit() {
    if (pending.current) return;
    if (confirmed) {
      try {
        await onCreated(
          confirmed,
          target.kind === "node" ? target.parent : undefined,
        );
        onClose();
      } catch (caught) {
        setError(`Saved, but refreshing failed: ${errorMessage(caught)}`);
      }
      return;
    }
    let nodes;
    try {
      nodes = draftToNodes(draft.bullets).map((node) => ({
        ...node,
        tags: splitTags(draft.tags),
      }));
    } catch (caught) {
      setError(errorMessage(caught));
      return;
    }
    pending.current = true;
    setBusy(true);
    setError("");
    // Persist the exact request before sending it, so an uncertain response can be retried.
    keep(draft);
    try {
      const result =
        draft.legacy && target.kind === "node"
          ? {
              nodes: [
                (
                  await api<{ node: Node }>(
                    "/nodes",
                    json("POST", {
                      parent_id: target.parent,
                      text: nodes[0].text,
                      tags: nodes[0].tags,
                      ...(target.after
                        ? { placement: { after: target.after } }
                        : {}),
                      request_id: draft.request_id,
                    }),
                  )
                ).node,
              ],
            }
          : await api<{ nodes: Node[] }>(
              target.kind === "capture" ? "/capture" : "/outline",
              json(
                "POST",
                target.kind === "capture"
                  ? { date: target.date, nodes, request_id: draft.request_id }
                  : {
                      parent_id: target.parent,
                      placement: target.after ? { after: target.after } : {},
                      nodes,
                      request_id: draft.request_id,
                    },
              ),
            );
      if (
        !Array.isArray(result.nodes) ||
        !result.nodes.length ||
        result.nodes.some((node) => !node?.id)
      )
        throw new Error(
          "Save was not confirmed. Retry to recover the original result.",
        );
      setConfirmed(result.nodes);
      try {
        const current = JSON.parse(localStorage.getItem(key) || "null");
        if (current?.request_id === draft.request_id)
          localStorage.removeItem(key);
      } catch {
        /* The canonical write succeeded. */
      }
      try {
        await onCreated(
          result.nodes,
          target.kind === "node" ? target.parent : undefined,
        );
        onClose();
      } catch (caught) {
        setError(`Saved, but refreshing failed: ${errorMessage(caught)}`);
      }
    } catch (caught) {
      setError(errorMessage(caught));
    } finally {
      pending.current = false;
      setBusy(false);
    }
  }
  function indent(index: number, outdent: boolean) {
    if (index < 0 || index >= draft.bullets.length || confirmed) return;
    focusRequest.current = {
      id: draft.bullets[index].id,
      cursor: fields.current.get(draft.bullets[index].id)?.selectionStart || 0,
    };
    change(changeDepth(draft.bullets, index, outdent));
  }
  function keyboard(event: KeyboardEvent<HTMLTextAreaElement>, index: number) {
    if (event.nativeEvent.isComposing || busy || confirmed) return;
    const row = draft.bullets[index];
    if (
      (event.ctrlKey || event.metaKey) &&
      !event.shiftKey &&
      event.key === "Enter"
    ) {
      event.preventDefault();
      void submit();
      return;
    }
    if (event.key === "Escape") {
      event.preventDefault();
      event.stopPropagation();
      onClose();
      return;
    }
    if (event.key === "Tab") {
      event.preventDefault();
      indent(index, event.shiftKey);
      return;
    }
    if (event.key === "Enter" && !event.shiftKey) {
      event.preventDefault();
      const start = event.currentTarget.selectionStart;
      const end = event.currentTarget.selectionEnd;
      const next = blankBullet(row.depth, row.text.slice(end));
      const rows = draft.bullets.map((item, i) =>
        i === index ? { ...item, text: row.text.slice(0, start) } : item,
      );
      rows.splice(subtreeEnd(rows, index), 0, next);
      focusRequest.current = { id: next.id, cursor: 0 };
      change(rows);
    } else if (
      event.key === "Backspace" &&
      row.text === "" &&
      draft.bullets.length > 1 &&
      subtreeEnd(draft.bullets, index) === index + 1
    ) {
      event.preventDefault();
      const next = draft.bullets[index - 1] || draft.bullets[index + 1];
      focusRequest.current = { id: next.id, cursor: next.text.length };
      change(draft.bullets.filter((item) => item.id !== row.id));
    } else if (
      (event.key === "ArrowUp" && event.currentTarget.selectionStart === 0) ||
      (event.key === "ArrowDown" &&
        event.currentTarget.selectionStart === row.text.length)
    ) {
      const next = draft.bullets[index + (event.key === "ArrowUp" ? -1 : 1)];
      if (next) {
        event.preventDefault();
        const field = fields.current.get(next.id);
        field?.focus();
        field?.setSelectionRange(next.text.length, next.text.length);
      }
    }
  }
  return (
    <div
      className="inline-composer"
      role="group"
      aria-label="New outline"
      aria-busy={busy}
    >
      <p className="draft-location">
        {target.kind === "capture" ? `Add to ${target.date}` : target.label}
      </p>
      <div className="draft-bullets">
        {draft.bullets.map((row, index) => (
          <div
            className="draft-bullet"
            key={row.id}
            style={{ marginInlineStart: `${Math.min(row.depth, 10) * 20}px` }}
            data-depth={row.depth}
          >
            <span className="draft-dot" aria-hidden="true">
              •
            </span>
            <Textarea
              ref={(field) => {
                if (field) fields.current.set(row.id, field);
                else fields.current.delete(row.id);
              }}
              aria-label={
                index === 0 ? "Your notes" : `New bullet ${index + 1}`
              }
              value={row.text}
              rows={1}
              readOnly={busy || confirmed !== null}
              placeholder="Write a thought…"
              className="draft-input"
              onFocus={() => setActive(row.id)}
              onKeyDown={(event) => keyboard(event, index)}
              onChange={(event) =>
                change(
                  draft.bullets.map((item) =>
                    item.id === row.id
                      ? { ...item, text: event.target.value }
                      : item,
                  ),
                )
              }
              onPaste={(event) => {
                const text = event.clipboardData.getData("text/plain");
                if (!text.includes("\n")) return;
                event.preventDefault();
                try {
                  const pasted = draftFromText(text).map((item) => ({
                    ...item,
                    depth: item.depth + row.depth,
                  }));
                  const start = event.currentTarget.selectionStart,
                    end = event.currentTarget.selectionEnd;
                  pasted[0].text = row.text.slice(0, start) + pasted[0].text;
                  pasted[pasted.length - 1].text += row.text.slice(end);
                  focusRequest.current = {
                    id: pasted[pasted.length - 1].id,
                    cursor: pasted[pasted.length - 1].text.length,
                  };
                  change([
                    ...draft.bullets.slice(0, index),
                    ...pasted,
                    ...draft.bullets.slice(index + 1),
                  ]);
                } catch (caught) {
                  setError(errorMessage(caught));
                }
              }}
            />
          </div>
        ))}
      </div>
      {error && (
        <p role="alert" className="inline-error">
          {error}
        </p>
      )}
      <div className="inline-composer-toolbar">
        <div className="outline-touch-actions">
          <Button
            size="icon-xs"
            variant="ghost"
            aria-label="Indent new bullet"
            disabled={busy}
            onMouseDown={(e) => e.preventDefault()}
            onClick={() =>
              indent(
                draft.bullets.findIndex((row) => row.id === active),
                false,
              )
            }
          >
            <ArrowRightToLine />
          </Button>
          <Button
            size="icon-xs"
            variant="ghost"
            aria-label="Outdent new bullet"
            disabled={busy}
            onMouseDown={(e) => e.preventDefault()}
            onClick={() =>
              indent(
                draft.bullets.findIndex((row) => row.id === active),
                true,
              )
            }
          >
            <ArrowLeftToLine />
          </Button>
          <Button
            size="icon-xs"
            variant="ghost"
            aria-label="New bullet"
            disabled={busy}
            onClick={() => {
              const next = blankBullet(
                draft.bullets[draft.bullets.length - 1].depth,
              );
              focusRequest.current = { id: next.id, cursor: 0 };
              change([...draft.bullets, next]);
            }}
          >
            <Plus />
          </Button>
        </div>
        <small>
          {confirmed
            ? "Saved"
            : stored
              ? "Enter to continue · Tab to nest · Ctrl/⌘ Enter to save"
              : "Keep this page open until your notes are saved."}
        </small>
        <Button
          size="sm"
          variant="ghost"
          onClick={onClose}
          disabled={busy}
          aria-label="Close outline draft"
        >
          <X />
        </Button>
        <Button
          size="sm"
          onClick={() => void submit()}
          disabled={busy || !draft.bullets.some((row) => row.text.trim())}
        >
          {busy ? <Loader2 className="animate-spin" /> : <Check />}
          {confirmed
            ? "Show saved bullets"
            : `Save ${target.kind === "capture" ? "to journal" : "bullets"}`}
        </Button>
      </div>
      <details className="draft-options">
        <summary>Additional tags</summary>
        <Input
          aria-label="Additional tags"
          placeholder="Tags for these root bullets"
          value={draft.tags}
          disabled={busy || confirmed !== null}
          onChange={(e) =>
            keep({ ...draft, tags: e.target.value, request_id: requestID() })
          }
        />
      </details>
    </div>
  );
}
