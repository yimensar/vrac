import { useEffect, useState } from "react";
import { History, Loader2, RotateCcw } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  api,
  errorMessage,
  json,
  requestID,
  type Node,
  type Revision,
} from "@/lib/api";
export function HistoryDialog({
  node,
  onClose,
  onRestored,
}: {
  node: Node;
  onClose: () => void;
  onRestored: (node: Node) => Promise<void>;
}) {
  const [revisions, setRevisions] = useState<Revision[]>([]);
  const [loading, setLoading] = useState(true);
  const [nextCursor, setNextCursor] = useState<string>();
  const [loadingMore, setLoadingMore] = useState(false);
  const [restoreKeys] = useState(() => new Map<number, string>());
  const [busy, setBusy] = useState<number>();
  const [error, setError] = useState("");
  useEffect(() => {
    let alive = true;
    api<{ revisions: Revision[]; next_cursor?: string }>(
      `/nodes/${node.id}/revisions`,
    )
      .then((value) => {
        if (alive) {
          setRevisions(value.revisions);
          setNextCursor(value.next_cursor);
        }
      })
      .catch((caught) => {
        if (alive) setError(errorMessage(caught));
      })
      .finally(() => {
        if (alive) setLoading(false);
      });
    return () => {
      alive = false;
    };
  }, [node.id]);
  async function loadMore() {
    if (!nextCursor || loadingMore) return;
    setLoadingMore(true);
    setError("");
    try {
      const value = await api<{ revisions: Revision[]; next_cursor?: string }>(
        `/nodes/${node.id}/revisions?cursor=${encodeURIComponent(nextCursor)}`,
      );
      setRevisions((current) => [...current, ...value.revisions]);
      setNextCursor(value.next_cursor);
    } catch (caught) {
      setError(errorMessage(caught));
    } finally {
      setLoadingMore(false);
    }
  }
  const latest = revisions[0];
  const currentRevision = latest?.revision ?? node.revision;
  const archived = latest?.node
    ? Boolean(latest.node.deleted)
    : Boolean(node.deleted);
  async function restore(revision: Revision) {
    if (
      !window.confirm(
        `Restore revision ${revision.revision}? The current text will remain in history.`,
      )
    )
      return;
    if (!restoreKeys.has(revision.revision))
      restoreKeys.set(revision.revision, requestID());
    setBusy(revision.revision);
    setError("");
    try {
      const value = await api<{ node: Node }>(
        `/nodes/${node.id}/restore`,
        json("POST", {
          revision: revision.revision,
          expected_revision: currentRevision,
          request_id: restoreKeys.get(revision.revision),
        }),
      );
      await onRestored(value.node);
      onClose();
    } catch (caught) {
      setError(errorMessage(caught));
    } finally {
      setBusy(undefined);
    }
  }
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent className="history-dialog">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <History className="size-4" />
            {archived ? "Archived note" : "Revision history"}
          </DialogTitle>
          <DialogDescription>
            {archived
              ? "This bullet is archived. Restore an earlier revision to bring it back to its original place."
              : "One note, with its earlier states preserved. Restoring creates a new revision."}
          </DialogDescription>
        </DialogHeader>
        {error && (
          <p role="alert" className="inline-error">
            {error}
          </p>
        )}
        {loading ? (
          <p className="loading-line">
            <Loader2 className="size-4 animate-spin" />
            Loading revisions…
          </p>
        ) : (
          <div className="revision-list">
            {revisions.length === 0 && (
              <p className="muted">No earlier revisions yet.</p>
            )}
            {revisions.map((revision) => (
              <article className="revision-card" key={revision.revision}>
                <div className="revision-heading">
                  <strong>Revision {revision.revision}</strong>
                  <span>
                    {revision.created_at || revision.at
                      ? new Date(
                          revision.created_at || revision.at || "",
                        ).toLocaleString()
                      : ""}
                  </span>
                  {revision.revision === currentRevision ? (
                    <Badge variant="secondary">
                      {archived ? "Archived" : "Current"}
                    </Badge>
                  ) : revision.node?.deleted ? (
                    <Badge variant="secondary">Archived state</Badge>
                  ) : (
                    <Button
                      size="sm"
                      variant="ghost"
                      disabled={busy !== undefined}
                      onClick={() => void restore(revision)}
                    >
                      {busy === revision.revision ? (
                        <Loader2 className="animate-spin" />
                      ) : (
                        <RotateCcw />
                      )}
                      Restore
                    </Button>
                  )}
                </div>
                {revision.summary && (
                  <p className="revision-summary">{revision.summary}</p>
                )}
                <p className="revision-text">{revision.text}</p>
                <div className="tag-row">
                  {(revision.tags || []).map((tag) => (
                    <Badge variant="secondary" key={tag}>
                      #{tag}
                    </Badge>
                  ))}
                </div>
              </article>
            ))}
            {nextCursor && (
              <Button
                variant="outline"
                className="load-more"
                disabled={loadingMore}
                onClick={() => void loadMore()}
              >
                {loadingMore && <Loader2 className="animate-spin" />}Load older
                revisions
              </Button>
            )}
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
