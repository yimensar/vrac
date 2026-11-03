import {
  Archive,
  ArrowUpRight,
  ChevronRight,
  Download,
  History,
  Maximize2,
  MoreHorizontal,
  Paperclip,
  PenLine,
  Pin,
  Sparkles,
  Trash2,
  Undo2,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Markdown } from "@/components/Markdown";
import { cn } from "@/lib/utils";
import {
  downloadText,
  formatDate,
  formatTime,
  plainPreview,
  safeFilename,
  type Note,
} from "@/lib/api";
export function NoteMenu({
  note,
  onEdit,
  onUpdate,
  onHistory,
  onFocus,
}: {
  note: Note;
  onEdit: () => void;
  onUpdate: (patch: Partial<Note>) => void;
  onHistory: () => void;
  onFocus: () => void;
}) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label={`Actions pour ${note.title || "la note"}`}
          className="note-menu-button"
        >
          <MoreHorizontal size={17} />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="note-menu">
        <DropdownMenuItem onClick={onFocus}>
          <Maximize2 />
          Ouvrir en focus
        </DropdownMenuItem>
        <DropdownMenuItem onClick={onEdit}>
          <PenLine />
          Modifier
        </DropdownMenuItem>
        <DropdownMenuItem onClick={() => onUpdate({ pinned: !note.pinned })}>
          <Pin />
          {note.pinned ? "Désépingler" : "Épingler"}
        </DropdownMenuItem>
        <DropdownMenuItem onClick={onHistory}>
          <History />
          Voir l’historique
        </DropdownMenuItem>
        <DropdownMenuItem
          onClick={() =>
            downloadText(`${safeFilename(note.title)}.md`, note.content)
          }
        >
          <Download />
          Exporter en Markdown
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        {note.state === "active" ? (
          <DropdownMenuItem onClick={() => onUpdate({ state: "archived" })}>
            <Archive />
            Archiver
          </DropdownMenuItem>
        ) : (
          <DropdownMenuItem onClick={() => onUpdate({ state: "active" })}>
            <Undo2 />
            Restaurer
          </DropdownMenuItem>
        )}
        {note.state !== "deleted" && (
          <DropdownMenuItem
            className="text-destructive"
            onClick={() => onUpdate({ state: "deleted" })}
          >
            <Trash2 />
            Mettre à la corbeille
          </DropdownMenuItem>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

export function NoteCard({
  note,
  outline,
  collapsed,
  onCollapse,
  onFocus,
  onEdit,
  onUpdate,
  onHistory,
  onTag,
  onLinkedNote,
  selected,
}: {
  note: Note;
  outline: boolean;
  collapsed: boolean;
  onCollapse: () => void;
  onFocus: () => void;
  onEdit: () => void;
  onUpdate: (patch: Partial<Note>) => void;
  onHistory: () => void;
  onTag: (tag: string) => void;
  onLinkedNote: (id: string) => void;
  selected: boolean;
}) {
  const content = note.content || "";
  return (
    <article
      className={cn(
        "note-card",
        outline && "outline-card",
        selected && "keyboard-selected",
      )}
      data-note-id={note.id}
    >
      {outline ? (
        <div className="outline-heading">
          <button
            className="outline-toggle"
            aria-label={collapsed ? "Déplier la note" : "Replier la note"}
            aria-expanded={!collapsed}
            onClick={onCollapse}
          >
            <ChevronRight size={15} className={collapsed ? "" : "rotate-90"} />
          </button>
          <button
            className="outline-bullet"
            aria-label={`Focus sur ${note.title || "la note"}`}
            onClick={onFocus}
          >
            <span />
          </button>
          <button className="outline-title" onClick={onFocus}>
            {note.title || plainPreview(content).slice(0, 100) || "Sans titre"}
          </button>
          {note.pinned && <Pin className="pinned-icon" size={13} />}
          <span className="outline-date">
            {formatDate(note.createdAt, true)}
          </span>
          <NoteMenu
            note={note}
            onEdit={onEdit}
            onUpdate={onUpdate}
            onHistory={onHistory}
            onFocus={onFocus}
          />
        </div>
      ) : (
        <div className="note-meta">
          <span className="note-avatar">
            {note.source === "mcp" ? <Sparkles size={13} /> : <span>Y</span>}
          </span>
          <span className="note-author">
            {note.source === "mcp" ? "Assistant IA" : "Vous"}
          </span>
          <span className="meta-dot">·</span>
          <time dateTime={note.createdAt}>
            {formatDate(note.createdAt, true)}
            <span className="note-time"> à {formatTime(note.createdAt)}</span>
          </time>
          {note.source === "memos" && (
            <span className="source-badge">Memos</span>
          )}
          {note.pinned && (
            <span className="pin-label">
              <Pin size={12} />
              Épinglée
            </span>
          )}
          <span className="flex-spacer" />
          <NoteMenu
            note={note}
            onEdit={onEdit}
            onUpdate={onUpdate}
            onHistory={onHistory}
            onFocus={onFocus}
          />
        </div>
      )}
      {(!outline || !collapsed) && (
        <div className="note-body">
          <div
            className={cn("note-content", !outline && "note-content-clamped")}
          >
            <Markdown value={content} onTag={onTag} onNote={onLinkedNote} />
          </div>
          {note.attachments?.length > 0 && (
            <div className="note-attachments">
              {note.attachments.map((a) => (
                <a
                  key={a.id}
                  href={`/api/attachments/${encodeURIComponent(a.id)}`}
                  target="_blank"
                  rel="noreferrer"
                >
                  <Paperclip size={12} />
                  <span>{a.filename}</span>
                  <ArrowUpRight size={11} />
                </a>
              ))}
            </div>
          )}
          <div className="note-footer">
            <div className="note-tags">
              {(note.tags || []).slice(0, 5).map((tag) => (
                <button key={tag} onClick={() => onTag(tag)}>
                  <span>#</span>
                  {tag}
                </button>
              ))}
              {(note.tags || []).length > 5 && (
                <span className="more-tags">+{note.tags.length - 5}</span>
              )}
            </div>
            <button
              className="focus-link"
              onClick={onFocus}
              aria-label={`Ouvrir ${note.title || "la note"} en focus`}
            >
              <Maximize2 size={13} />
              <span>Focus</span>
            </button>
          </div>
        </div>
      )}
    </article>
  );
}
