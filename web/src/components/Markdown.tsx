import { forwardRef, useMemo, useRef } from "react";
import { MarkdownEditor, type Editor } from "@usememos/editor/react";
import {
  tags,
  footnotes,
  suggestion,
  matchToken,
  scanTags,
} from "@usememos/editor";
import "@usememos/editor/style.css";
import { cn } from "@/lib/utils";

type Props = {
  value: string;
  onChange?: (value: string) => void;
  editable?: boolean;
  source?: boolean;
  placeholder?: string;
  onSubmit?: (value: string) => void;
  autofocus?: boolean;
  className?: string;
  onTag?: (tag: string) => void;
  onNote?: (id: string) => void;
  knownTags?: string[];
};

export const Markdown = forwardRef<Editor | null, Props>(function Markdown(
  {
    value,
    onChange,
    editable = false,
    source = false,
    placeholder,
    onSubmit,
    autofocus = false,
    className,
    onTag,
    onNote,
    knownTags = [],
  },
  ref,
) {
  const onTagRef = useRef(onTag);
  const knownTagsRef = useRef(knownTags);
  onTagRef.current = onTag;
  knownTagsRef.current = knownTags;
  const extensions = useMemo(
    () => [
      tags({
        onClick: ({ match }) => {
          onTagRef.current?.(match.slice(1));
          return Boolean(onTagRef.current);
        },
      }),
      footnotes(),
      suggestion({
        name: "tags",
        trigger: ["#", "＃"],
        match: matchToken(scanTags),
        label: "Repères",
        limit: 8,
        items: ({ query }) =>
          knownTagsRef.current
            .filter((tag) =>
              tag
                .toLocaleLowerCase("fr")
                .includes(query.toLocaleLowerCase("fr")),
            )
            .map((tag) => ({ id: tag, label: `#${tag}`, value: tag })),
      }),
    ],
    [],
  );
  return (
    <MarkdownEditor
      ref={ref}
      value={value}
      onChange={onChange}
      editable={editable}
      mode={source ? "source" : "rich"}
      placeholder={placeholder}
      onSubmit={onSubmit}
      autofocus={autofocus}
      extensions={extensions}
      className={cn("vrac-markdown", className)}
      attributes={{
        "aria-label": editable ? "Contenu de la note" : "Note",
        spellcheck: "true",
      }}
      onLinkClick={({ href }) => {
        if (href.startsWith("/memos/")) {
          const id = decodeURIComponent(
            href.slice("/memos/".length).split(/[?#]/)[0],
          );
          if (onNote) onNote(id);
          else window.location.assign(`/?note=${encodeURIComponent(id)}`);
          return true;
        }
        if (href.startsWith("/file/attachments/")) {
          const id = href.split("/")[3];
          window.open(
            `/api/attachments/${encodeURIComponent(id)}`,
            "_blank",
            "noopener,noreferrer",
          );
          return true;
        }
        return false;
      }}
      theme={{ colorScheme: "inherit" }}
      labels={{ copyCode: "Copier", copied: "Copié" }}
    />
  );
});
