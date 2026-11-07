import { Schema, type Node as PMNode } from "prosemirror-model"
import { typeClass } from "@/lib/colors"

// One line of an outline: text with atomic references and tags. Atoms are
// edited as a whole, so what the editor shows matches what rows display.

/** The known tags, kept current by the app: color and to-do trait. */
const known = new Map<string, { color: string; todo: boolean }>()
export function setTypes(types: Iterable<{ name: string; color: string; todo: boolean }>) {
  known.clear()
  for (const t of types) known.set(t.name, { color: t.color, todo: t.todo })
}
export const hasType = (name: string) => known.has(name)
export const typeColor = (name: string) => known.get(name)?.color
export const isTodo = (name: string) => known.get(name)?.todo ?? false
/** The to-do tags, in creation order. */
export const todoTags = () => [...known.entries()].filter(([, t]) => t.todo).map(([name]) => name)

export const refClass = "font-medium underline decoration-muted-foreground/40 underline-offset-[3px]"

export const schema = new Schema({
  nodes: {
    doc: { content: "inline*" },
    text: { group: "inline" },
    ref: {
      group: "inline",
      inline: true,
      atom: true,
      selectable: false,
      attrs: { id: { default: null }, name: {}, label: { default: "" } },
      toDOM: (n: PMNode) => ["span", { class: refClass + (n.attrs.id ? " cursor-pointer" : ""), "data-ref": n.attrs.id ?? "" }, n.attrs.label || n.attrs.name],
    },
    type: {
      group: "inline",
      inline: true,
      atom: true,
      selectable: false,
      attrs: { name: {} },
      toDOM: (n: PMNode) => ["span", { class: "font-medium cursor-pointer " + typeClass(typeColor(n.attrs.name)), "data-tag": n.attrs.name }, "#" + n.attrs.name],
    },
  },
})
