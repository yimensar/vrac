import type { Node, Refs } from "./api"

// The Vrac syntax as the interface uses it.
//
// Canonical text, stored by the server, writes references as [[ID]].
// Input, written by the editor and sent back, writes them [[Name|ID]], or
// [[Name]] for a node to resolve or create by name. #type tokens are the
// same in both. Rows display input, so a saved row and a row being edited
// look the same.

const canonicalRef = /\[\[([a-z2-7]{26})\]\]/g

/** Turns canonical text into input, naming references from refs. */
export function toInput(text: string, refs: Refs): string {
  return text.replace(canonicalRef, (_, id: string) => `[[${refs[id]?.name ?? id}|${id}]]`)
}

export function nodeInput(node: Pick<Node, "text">, refs: Refs): string {
  return toInput(node.text, refs)
}

export type Token =
  | { kind: "text"; start: number; text: string }
  | { kind: "ref"; start: number; length: number; name: string; id?: string }
  | { kind: "type"; start: number; length: number; name: string }

const inputTokens = /\[\[([^[\]|]+?)(?:\|([a-z2-7]{26}))?\]\]|(^|\s)#([\p{L}\p{N}_-]+)/gu

/** Splits input into text, references and types, with their offsets. */
export function tokens(input: string): Token[] {
  const out: Token[] = []
  let last = 0
  const text = (end: number) => {
    if (end > last) out.push({ kind: "text", start: last, text: input.slice(last, end) })
  }
  for (const m of input.matchAll(inputTokens)) {
    if (m[1] !== undefined) {
      text(m.index)
      out.push({ kind: "ref", start: m.index, length: m[0].length, name: m[1].trim(), id: m[2] })
    } else {
      const start = m.index + m[3].length
      text(start)
      out.push({ kind: "type", start, length: m[0].length - m[3].length, name: m[4].toLowerCase() })
    }
    last = m.index + m[0].length
  }
  text(input.length)
  return out
}

/** What the caret follows: [[ or @ for a reference, # for a tag, / for a
 * command. length counts the trigger and its query. */
export type Trigger = { kind: "ref" | "type" | "command"; query: string; length: number }

/** Finds a completion trigger at the end of the text before the caret. */
export function trigger(before: string): Trigger | null {
  const ref = /\[\[([^[\]]*)$/.exec(before)
  if (ref) return { kind: "ref", query: ref[1], length: ref[0].length }
  // @, # and / open a menu only at the start of a word, so emails, C# and
  // paths stay text. A command takes words after its name: "/due friday
  // 14h"; the menu closes by itself once nothing matches.
  const word = /(^|\s)([@#])([\p{L}\p{N}_-]*)$/u.exec(before)
  if (word) return { kind: word[2] === "@" ? "ref" : "type", query: word[3], length: word[3].length + 1 }
  const command = /(^|\s)\/([\p{L}\p{N}_-]*(?: [^/]*)?)$/u.exec(before)
  if (command) return { kind: "command", query: command[2], length: command[2].length + 1 }
  return null
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
  const d = day === localDate() ? "Today" : formatDay(day)
  return time ? `${d} ${time}` : d
}

/** The type tokens of input, in order, without duplicates. */
export function typesOf(input: string): string[] {
  const out: string[] = []
  for (const t of tokens(input)) if (t.kind === "type" && !out.includes(t.name)) out.push(t.name)
  return out
}

/** Formats an RFC 3339 timestamp in the browser's local time. */
export function formatStamp(at: string): string {
  return new Date(at).toLocaleString(undefined, { day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" })
}

export const isDay = (name: string) => /^\d{4}-\d{2}-\d{2}$/.test(name)
