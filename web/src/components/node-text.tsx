import { Fragment } from "react";
import type { Node } from "@/lib/api";

export function NodeText({
  node,
  onSubject,
}: {
  node: Node;
  onSubject: (id: string, label: string) => void;
}) {
  const parts = node.text.split(/(\[\[[^\]\n]+\]\])/g);
  return (
    <span className="node-text">
      {parts.map((part, i) => {
        const label =
          part.startsWith("[[") && part.endsWith("]]")
            ? part.slice(2, -2)
            : undefined;
        const ref =
          label === undefined
            ? undefined
            : node.references?.find(
                (item) =>
                  (item.source_label || item.label) === label ||
                  item.label === label,
              );
        return ref ? (
          <button
            key={i}
            className="reference-link"
            onClick={(event) => {
              event.stopPropagation();
              onSubject(ref.target_id, ref.label);
            }}
            title={`Explore ${ref.label}`}
          >
            {ref.label}
          </button>
        ) : (
          <Fragment key={i}>{part}</Fragment>
        );
      })}
    </span>
  );
}
