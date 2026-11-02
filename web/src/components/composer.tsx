import { useEffect, useState } from "react";
import { ArrowUpRight, Loader2 } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { Input } from "@/components/ui/input";
import { api, errorMessage, json, requestID, type Node } from "@/lib/api";
import { parseOutline, splitTags } from "@/lib/outline";
export type ComposerTarget =
  | { kind: "capture"; date: string }
  | { kind: "node"; parent: string; after?: string; label: string };
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
    try {
      const raw = localStorage.getItem(key);
      if (raw) {
        try {
          const data = JSON.parse(raw);
          if (
            typeof data.text === "string" &&
            typeof data.request_id === "string"
          )
            return data as { text: string; tags: string; request_id: string };
        } catch {
          return { text: raw, tags: "", request_id: requestID() };
        }
      }
    } catch {
      /* Browser storage may be unavailable. */
    }
    return { text: "", tags: "", request_id: requestID() };
  });
  const [text, setText] = useState(initial.text);
  const [mutationID, setMutationID] = useState(initial.request_id);
  const [tags, setTags] = useState(initial.tags || "");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [draftStored, setDraftStored] = useState(true);
  useEffect(() => {
    try {
      if (text)
        localStorage.setItem(
          key,
          JSON.stringify({ text, tags, request_id: mutationID }),
        );
      else localStorage.removeItem(key);
      setDraftStored(true);
    } catch {
      setDraftStored(false);
    }
  }, [key, text, tags, mutationID]);
  async function submit() {
    if (!text.trim() || busy) return;
    setBusy(true);
    setError("");
    try {
      let nodes: Node[];
      if (target.kind === "capture") {
        const input = parseOutline(text).map((node) => ({
          ...node,
          tags: splitTags(tags),
        }));
        const result = await api<{ nodes: Node[] }>(
          "/capture",
          json("POST", {
            date: target.date,
            nodes: input,
            request_id: mutationID,
          }),
        );
        nodes = result.nodes;
      } else {
        const result = await api<{ node: Node }>(
          "/nodes",
          json("POST", {
            parent_id: target.parent,
            text,
            tags: splitTags(tags),
            ...(target.after ? { placement: { after: target.after } } : {}),
            request_id: mutationID,
          }),
        );
        nodes = [result.node];
      }
      try {
        localStorage.removeItem(key);
      } catch {
        /* The canonical write has succeeded. */
      }
      onClose();
      await onCreated(
        nodes,
        target.kind === "node" ? target.parent : undefined,
      );
    } catch (caught) {
      setError(errorMessage(caught));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !busy) onClose();
      }}
    >
      <DialogContent className="capture-dialog">
        <DialogHeader>
          <DialogTitle>
            {target.kind === "capture"
              ? "Capture in the journal"
              : target.label}
          </DialogTitle>
          <DialogDescription>
            {target.kind === "capture"
              ? `Add to ${target.date}. Paste indented bullets to keep their structure.`
              : "Add a bullet here. Use line breaks for longer text."}
          </DialogDescription>
        </DialogHeader>
        <form
          onSubmit={(event) => {
            event.preventDefault();
            void submit();
          }}
        >
          <label className="sr-only" htmlFor="capture-text">
            Your notes
          </label>
          <Textarea
            id="capture-text"
            autoFocus
            placeholder="What is on your mind?\n\nMeeting [[Project Atlas]] #meeting\n  A decision worth keeping #decision"
            rows={10}
            value={text}
            onChange={(event) => {
              setText(event.target.value);
              setMutationID(requestID());
            }}
            onKeyDown={(event) => {
              if ((event.ctrlKey || event.metaKey) && event.key === "Enter") {
                event.preventDefault();
                void submit();
              }
            }}
          />
          <label className="field-label" htmlFor="capture-tags">
            Additional tags
          </label>
          <Input
            id="capture-tags"
            placeholder="meeting, decision, idea…"
            value={tags}
            onChange={(event) => {
              setTags(event.target.value);
              setMutationID(requestID());
            }}
          />
          <p className="field-hint">
            Use #tags and [[references]] in your text.{" "}
            {target.kind === "capture" && "All bullets are saved together."}
          </p>
          {error && (
            <p className="inline-error" role="alert">
              {error}
            </p>
          )}
          <DialogFooter>
            <span className="draft-note">
              {draftStored
                ? "Your draft stays here until saved."
                : "Browser draft storage is unavailable."}
            </span>
            <Button type="submit" disabled={busy || !text.trim()}>
              {busy ? <Loader2 className="animate-spin" /> : <ArrowUpRight />}
              Save {target.kind === "capture" ? "to journal" : "bullet"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
