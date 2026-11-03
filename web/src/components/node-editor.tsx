import { useEffect, useRef, useState } from "react";
import { Check, Loader2, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import {
  api,
  ApiError,
  errorMessage,
  json,
  requestID,
  type Node,
  type NodeView,
} from "@/lib/api";
import { splitTags } from "@/lib/outline";

type Draft = {
  text: string;
  tags: string;
  revision: number;
  summary: string;
  request_id: string;
};
function draftFor(node: Node): Draft {
  try {
    const draft = JSON.parse(
      localStorage.getItem(`vrac:draft:${node.id}`) || "null",
    ) as Draft | null;
    if (
      draft &&
      typeof draft.text === "string" &&
      typeof draft.tags === "string" &&
      typeof draft.revision === "number"
    )
      return {
        ...draft,
        summary: typeof draft.summary === "string" ? draft.summary : "",
        request_id:
          typeof draft.request_id === "string" ? draft.request_id : requestID(),
      };
  } catch {
    /* A damaged or unavailable browser draft does not affect canonical data. */
  }
  return {
    text: node.text,
    tags: node.tags.join(" "),
    revision: node.revision,
    summary: "",
    request_id: requestID(),
  };
}
export function NodeEditor({
  node,
  onSaved,
  onClose,
  compact = false,
  onNext,
}: {
  node: Node;
  onSaved: (node: Node, keepEditing?: boolean) => Promise<void>;
  onClose: () => void;
  compact?: boolean;
  onNext?: (node: Node, action: "below" | "indent" | "outdent") => void;
}) {
  const [draft, setDraft] = useState(() => draftFor(node));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [conflict, setConflict] = useState(false);
  const [current, setCurrent] = useState<Node>();
  const [localDraft, setLocalDraft] = useState(true);
  const latest = useRef(draft);
  latest.current = draft;
  const pending = useRef(false);
  const alive = useRef(true);
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);
  const field = useRef<HTMLTextAreaElement>(null);
  useEffect(() => {
    if (compact && field.current) {
      field.current.style.height = "auto";
      field.current.style.height = `${Math.max(28, field.current.scrollHeight)}px`;
    }
  }, [compact, draft.text]);
  useEffect(() => {
    try {
      localStorage.setItem(`vrac:draft:${node.id}`, JSON.stringify(draft));
      setLocalDraft(true);
    } catch {
      setLocalDraft(false);
    }
  }, [draft, node.id]);
  async function save() {
    if (!draft.text.trim() || pending.current) return;
    if (
      compact &&
      draft.text === node.text &&
      draft.tags === node.tags.join(" ") &&
      draft.revision === node.revision &&
      !draft.summary
    ) {
      try {
        localStorage.removeItem(`vrac:draft:${node.id}`);
      } catch {
        /* The editor matches canonical state. */
      }
      try {
        await onSaved(node);
        return node;
      } catch (caught) {
        setError(errorMessage(caught));
        return;
      }
    }
    pending.current = true;
    setBusy(true);
    setError("");
    try {
      const result = await api<{ node: Node }>(
        `/nodes/${node.id}`,
        json("PATCH", {
          text: draft.text,
          tags: splitTags(draft.tags),
          expected_revision: draft.revision,
          summary: draft.summary,
          request_id: draft.request_id,
        }),
      );
      let storedRequest: string | undefined;
      try {
        storedRequest = JSON.parse(
          localStorage.getItem(`vrac:draft:${node.id}`) || "null",
        )?.request_id;
      } catch {
        /* Use the current in-memory draft. */
      }
      const newer =
        latest.current.request_id !== draft.request_id ||
        Boolean(storedRequest && storedRequest !== draft.request_id);
      if (!alive.current) {
        if (!newer) {
          try {
            localStorage.removeItem(`vrac:draft:${node.id}`);
          } catch {
            /* The write is confirmed. */
          }
        }
        await onSaved(result.node, true);
        return;
      }
      if (newer) {
        setDraft((value) => ({ ...value, revision: result.node.revision }));
        await onSaved(result.node, true);
        return;
      }
      try {
        localStorage.removeItem(`vrac:draft:${node.id}`);
      } catch {
        /* Saving to the server succeeded. */
      }
      try {
        await onSaved(result.node);
        return result.node;
      } catch (caught) {
        setDraft((value) => ({
          ...value,
          revision: result.node.revision,
          request_id: requestID(),
        }));
        setError(
          `Saved successfully, but refreshing failed: ${errorMessage(caught)}`,
        );
      }
    } catch (caught) {
      setError(errorMessage(caught));
      setConflict(caught instanceof ApiError && caught.status === 409);
    } finally {
      pending.current = false;
      setBusy(false);
    }
  }
  async function review() {
    try {
      const result = await api<NodeView>(`/nodes/${node.id}`);
      setCurrent(result.node);
      setDraft((value) => ({
        ...value,
        revision: result.node.revision,
        request_id: requestID(),
      }));
      setConflict(false);
      setError(
        "Review the current text below before saving your draft over this version.",
      );
    } catch (caught) {
      setError(errorMessage(caught));
    }
  }
  return (
    <div
      className={`node-editor${compact ? " inline-node-editor" : ""}`}
      onKeyDown={(event) => {
        if (event.nativeEvent.isComposing) return;
        if (
          compact &&
          event.target instanceof HTMLTextAreaElement &&
          ((event.key === "Enter" &&
            !event.shiftKey &&
            !event.metaKey &&
            !event.ctrlKey) ||
            event.key === "Tab")
        ) {
          event.preventDefault();
          const action =
            event.key === "Enter"
              ? "below"
              : event.shiftKey
                ? "outdent"
                : "indent";
          if (action === "below") {
            if (!draft.text.trim()) {
              setError("A saved bullet needs text. Use Archive to remove it.");
              return;
            }
            void save();
            onNext?.(node, action);
            return;
          }
          void save().then((node) => {
            if (node) onNext?.(node, action);
          });
          return;
        }
        if (
          (event.ctrlKey || event.metaKey) &&
          !event.shiftKey &&
          event.key === "Enter"
        ) {
          event.preventDefault();
          void save();
        }
        if (event.key === "Escape") {
          event.stopPropagation();
          onClose();
        }
      }}
    >
      <Textarea
        ref={field}
        autoFocus
        aria-label="Edit bullet"
        value={draft.text}
        onChange={(event) =>
          setDraft({
            ...draft,
            text: event.target.value,
            request_id: requestID(),
          })
        }
        className="editor-text"
        rows={
          compact ? 1 : Math.min(12, Math.max(3, draft.text.split("\n").length))
        }
      />
      <details open={!compact} className="editor-details">
        {compact && <summary>Tags and change summary</summary>}
        <div className="editor-properties">
          <label className="sr-only" htmlFor={`tags-${node.id}`}>
            Tags
          </label>
          <Input
            id={`tags-${node.id}`}
            placeholder="Tags: decision project…"
            value={draft.tags}
            onChange={(event) =>
              setDraft({
                ...draft,
                tags: event.target.value,
                request_id: requestID(),
              })
            }
          />
          <label className="sr-only" htmlFor={`summary-${node.id}`}>
            Change summary
          </label>
          <Input
            id={`summary-${node.id}`}
            placeholder="What changed? (optional)"
            value={draft.summary}
            onChange={(event) =>
              setDraft({
                ...draft,
                summary: event.target.value,
                request_id: requestID(),
              })
            }
          />
        </div>
      </details>
      {current && (
        <div className="current-version">
          <strong>Current saved text · revision {current.revision}</strong>
          <p>{current.text}</p>
          <small>{current.tags.map((tag) => `#${tag}`).join(" ")}</small>
        </div>
      )}
      {error && (
        <div role="alert" className="inline-error">
          {error}
          {conflict && (
            <Button size="sm" variant="outline" onClick={() => void review()}>
              Review current version
            </Button>
          )}
        </div>
      )}
      <div className="editor-footer">
        <small>
          {localDraft
            ? "Draft kept in this browser"
            : "Browser draft storage unavailable — keep this editor open"}
        </small>
        <div>
          <Button size="sm" variant="ghost" onClick={onClose} disabled={busy}>
            <X />
            Close
          </Button>
          <Button
            size="sm"
            onClick={() => void save()}
            disabled={busy || !draft.text.trim()}
          >
            {busy ? <Loader2 className="animate-spin" /> : <Check />}Save
          </Button>
        </div>
      </div>
    </div>
  );
}
