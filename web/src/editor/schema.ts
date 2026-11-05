import { Schema, type Node as PMNode } from "prosemirror-model"
import { typeClass } from "@/lib/colors"

// One line of an outline: text with atomic references and types. Atoms are
// edited as a whole, so what the editor shows matches what rows display.

/** Known types and their colors, kept current by the app. */
const known = new Map<string, string>()
export function setTypes(colors: Map<string, string>) {
  known.clear()
  for (const [k, v] of colors) known.set(k, v)
}
export const hasType = (name: string) => known.has(name)
export const typeColor = (name: string) => known.get(name)

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
      toDOM: (n: PMNode) => ["span", { class: refClass, "data-ref": n.attrs.id ?? "" }, n.attrs.label || n.attrs.name],
    },
    type: {
      group: "inline",
      inline: true,
      atom: true,
      selectable: false,
      attrs: { name: {} },
      toDOM: (n: PMNode) => ["span", { class: "font-medium " + typeClass(known.get(n.attrs.name)) }, "#" + n.attrs.name],
    },
  },
})
