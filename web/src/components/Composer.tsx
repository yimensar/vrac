import { useState } from "react";
import { Code2, Hash, LoaderCircle, PenLine, Plus } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Markdown } from "@/components/Markdown";
import { cn } from "@/lib/utils";
import type { Tag } from "@/lib/api";
export function Composer({
  draft,
  onDraft,
  source,
  onSource,
  onSave,
  saving,
  tags,
  compact = false,
}: {
  draft: string;
  onDraft: (v: string) => void;
  source: boolean;
  onSource: () => void;
  onSave: (content?: string) => void;
  saving: boolean;
  tags: Tag[];
  compact?: boolean;
}) {
  const [tagSearch, setTagSearch] = useState(false);
  return (
    <div className={cn("composer", compact && "composer-modal")}>
      <div className="composer-label">
        <span className="composer-avatar">
          <PenLine size={14} />
        </span>
        <span>Qu’est-ce qu’on garde aujourd’hui ?</span>
        <span className="composer-live">
          <span /> Markdown en direct
        </span>
      </div>
      <Markdown
        value={draft}
        onChange={onDraft}
        knownTags={tags.map((t) => t.name)}
        editable={!saving}
        source={source}
        placeholder="Une idée, une découverte, quelque chose à ne pas oublier…"
        onSubmit={onSave}
        className="capture-editor"
      />
      <div className="composer-bottom">
        <div className="composer-tools">
          <button
            title="Insérer un tag"
            aria-label="Insérer un tag"
            className={cn("tool-button", tagSearch && "active")}
            disabled={saving}
            onClick={() => setTagSearch(!tagSearch)}
          >
            <Hash size={17} />
          </button>
          <button
            title={source ? "Passer en mode visuel" : "Passer en mode Markdown"}
            aria-label={source ? "Mode visuel" : "Mode Markdown"}
            className={cn("tool-button", source && "active")}
            disabled={saving}
            onClick={onSource}
          >
            <Code2 size={17} />
          </button>
          <span className="composer-hint">Vos idées, sans mise en ordre.</span>
        </div>
        <Button
          onClick={() => onSave()}
          disabled={!draft.trim() || saving}
          className="capture-submit"
        >
          {saving ? (
            <LoaderCircle className="animate-spin" />
          ) : (
            <Plus size={16} />
          )}
          <span>Enregistrer</span>
          <kbd aria-hidden="true">⌘ ↵</kbd>
        </Button>
      </div>
      {tagSearch && (
        <div className="tag-suggestions">
          <span>Ajouter un repère</span>
          {tags.slice(0, 8).map((t) => (
            <button
              key={t.name}
              disabled={saving}
              onClick={() => {
                onDraft(`${draft.trimEnd()} #${t.name} `);
                setTagSearch(false);
              }}
            >
              #{t.name}
            </button>
          ))}
          {tags.length === 0 && (
            <p>Écrivez # suivi du nom de votre premier tag.</p>
          )}
        </div>
      )}
    </div>
  );
}
