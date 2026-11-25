import type { Fragment, Node as PMNode } from "prosemirror-model"
import type { Refs } from "@/lib/api"
import { tokens } from "@/lib/syntax"
import { schema } from "./schema"

// Conversions between input strings and editor documents. Offsets count
// characters of the input string; positions count editor steps, where an
// atom is one step.

export function fromInput(input: string, refs: Refs): PMNode {
  const nodes: PMNode[] = []
  for (const t of tokens(input)) {
    if (t.kind === "text") nodes.push(schema.text(t.text))
    else if (t.kind === "ref") nodes.push(schema.nodes.ref.create({ id: t.id ?? null, name: t.name, label: (t.id && refs[t.id]?.label) || t.name }))
    else nodes.push(schema.nodes.type.create({ name: t.name }))
  }
  return schema.node("doc", null, nodes)
}

export function atomInput(n: PMNode): string {
  if (n.type === schema.nodes.ref) return n.attrs.id ? `[[${n.attrs.name}|${n.attrs.id}]]` : `[[${n.attrs.name}]]`
  if (n.type === schema.nodes.type) return "#" + n.attrs.name
  return n.text ?? ""
}

export function toInput(content: Fragment): string {
  let out = ""
  content.forEach((n) => (out += atomInput(n)))
  return out
}

/** Editor position of an input offset; inside an atom means after it. */
export function offsetToPos(doc: PMNode, offset: number): number {
  let pos = 0
  let at = 0
  let found = -1
  doc.forEach((n) => {
    if (found >= 0) return
    const len = atomInput(n).length
    if (offset <= at + len) {
      found = n.isText ? pos + (offset - at) : offset === at ? pos : pos + 1
      return
    }
    at += len
    pos += n.nodeSize
  })
  return found >= 0 ? found : doc.content.size
}

/** Input offset of an editor position. */
export function posToOffset(doc: PMNode, pos: number): number {
  return toInput(doc.content.cut(0, pos)).length
}
