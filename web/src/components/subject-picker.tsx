import { useEffect, useState } from "react";
import { ArrowUpRight, Loader2, Search } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { api, errorMessage, type SearchPage } from "@/lib/api";
export function SubjectPicker({
  onClose,
  onSelect,
}: {
  onClose: () => void;
  onSelect: (id: string, label: string) => void;
}) {
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<SearchPage>({ items: [] });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    let alive = true;
    if (!query.trim()) {
      setResults({ items: [] });
      setBusy(false);
      return;
    }
    setBusy(true);
    setError("");
    const timer = setTimeout(() => {
      api<SearchPage>(`/search?q=${encodeURIComponent(query.trim())}&limit=12`)
        .then((data) => {
          if (alive) setResults(data);
        })
        .catch((caught) => {
          if (alive) setError(errorMessage(caught));
        })
        .finally(() => {
          if (alive) setBusy(false);
        });
    }, 200);
    return () => {
      alive = false;
      clearTimeout(timer);
    };
  }, [query]);
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogContent className="subject-picker">
        <DialogHeader>
          <DialogTitle>Choose a subject</DialogTitle>
          <DialogDescription>
            Find a project, person, or any note that other notes reference.
          </DialogDescription>
        </DialogHeader>
        <div className="search-box">
          <Search className="size-4" />
          <Input
            autoFocus
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="Find a subject…"
            aria-label="Find a subject"
          />
        </div>
        {error && (
          <p role="alert" className="inline-error">
            {error}
          </p>
        )}
        <div className="subject-options">
          {busy ? (
            <p className="loading-line">
              <Loader2 className="size-4 animate-spin" />
              Searching…
            </p>
          ) : (
            results.items.map((item) => (
              <Button
                key={item.node.id}
                variant="ghost"
                onClick={() => {
                  onSelect(item.node.id, item.node.text);
                  onClose();
                }}
              >
                <span>
                  {item.node.text}
                  <small>
                    {item.path
                      .filter((node) => node.id !== item.node.id)
                      .map((node) => node.text)
                      .join(" / ") || "Subject note"}
                  </small>
                </span>
                <ArrowUpRight />
              </Button>
            ))
          )}
          {!busy && query.trim() && results.items.length === 0 && (
            <p className="muted">No matching notes. Try another word.</p>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
