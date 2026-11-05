import type { Node, Refs } from "./api"

// Presentation of the Vrac syntax. Canonical text stores references as
// [[ID]]; the editor shows [[Name]] and #type, and sends [[Name|ID]] for
// names it knows, so the engine never has to guess.

export const refToken = /\[\[([a-z2-7]{26})\]\]/g

export type Segment = { text: string } | { ref: string }

export function segments(text: string): Segment[] {
  const out: Segment[] = []
  let last = 0
  for (const m of text.matchAll(refToken)) {
    if (m.index > last) out.push({ text: text.slice(last, m.index) })
    out.push({ ref: m[1] })
    last = m.index + m[0].length
  }
  if (last < text.length) out.push({ text: text.slice(last) })
  return out
}

export function plain(text: string, refs: Refs): string {
  return text.replace(refToken, (_, id: string) => refs[id]?.name ?? "…")
}

/** Names the editor can turn back into IDs, keyed by lower-case name. */
export type Known = Map<string, string>

export function toEditable(node: Pick<Node, "text" | "types">, refs: Refs, known: Known): string {
  const text = node.text.replace(refToken, (_, id: string) => {
    const name = refs[id]?.name ?? id
    known.set(name.toLowerCase(), id)
    return `[[${name}]]`
  })
  return [text, ...node.types.map((t) => "#" + t)].filter(Boolean).join(" ")
}

export function toInput(value: string, known: Known): string {
  return value.replace(/\[\[([^[\]]+)\]\]/g, (whole, name: string) => {
    const id = known.get(name.trim().toLowerCase())
    return id ? `[[${name.trim()}|${id}]]` : whole
  })
}

export type Trigger = { kind: "ref" | "type"; query: string; start: number }

/** Finds an autocomplete trigger ending at the caret. */
export function trigger(value: string, caret: number): Trigger | null {
  const before = value.slice(0, caret)
  const ref = /\[\[([^[\]]*)$/.exec(before)
  if (ref) return { kind: "ref", query: ref[1], start: ref.index }
  const type = /(^|\s)#([\p{L}\p{N}_-]*)$/u.exec(before)
  if (type) return { kind: "type", query: type[2], start: type.index + type[1].length }
  return null
}

export function replaceTrigger(value: string, caret: number, t: Trigger, insert: string): { value: string; caret: number } {
  const next = value.slice(0, t.start) + insert + value.slice(caret)
  return { value: next, caret: t.start + insert.length }
}

// Dates are local calendar values: never convert them through UTC.
export function localDate(d = new Date()): string {
  const p = (n: number) => String(n).padStart(2, "0")
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
}

export function addDays(day: string, n: number): string {
  const [y, m, d] = day.split("-").map(Number)
  return localDate(new Date(y, m - 1, d + n))
}

export function formatDay(day: string, opts: Intl.DateTimeFormatOptions = { weekday: "short", day: "numeric", month: "short" }): string {
  const [y, m, d] = day.split("-").map(Number)
  return new Date(y, m - 1, d).toLocaleDateString(undefined, opts)
}

export function formatMoment(m: string): string {
  const [day, time] = m.split("T")
  return time ? `${formatDay(day)} ${time}` : formatDay(day)
}

/** Formats an RFC 3339 timestamp in the browser's local time. */
export function formatStamp(at: string): string {
  return new Date(at).toLocaleString(undefined, { day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" })
}
