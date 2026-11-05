import { useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent } from "react"
import { useQuery } from "@tanstack/react-query"
import { api, type Creation } from "@/lib/api"
import { typeClass } from "@/lib/colors"
import { useTypes } from "@/lib/hooks"
import { replaceTrigger, trigger, type Known, type Trigger } from "@/lib/syntax"
import { cn } from "@/lib/utils"
import { Label } from "./node-text"

/** What the editor collected besides its text. */
export type Intent = { known: Known; create: Creation[]; createTypes: string[] }

export function newIntent(known: Known = new Map()): Intent {
  return { known, create: [], createTypes: [] }
}

/** A completion. Node names stay plain; their types and types themselves
 * are shown in the type's color. */
type Option = { key: string; label: string; types?: string[]; hint?: string; color?: string; rich?: boolean; apply: () => void }

type Props = {
  value: string
  onChange: (value: string) => void
  intent: Intent
  /** Return true when the key was handled. Called when no menu is open. */
  onKey?: (e: KeyboardEvent<HTMLTextAreaElement>, el: HTMLTextAreaElement) => boolean
  onBlur?: () => void
  placeholder?: string
  autoFocus?: boolean
  /** Caret position when focused on mount; the end by default. */
  initialCaret?: number
  className?: string
}

/** A single-paragraph text area with #type and [[reference]] completion. */
export function Editor({ value, onChange, intent, onKey, onBlur, placeholder, autoFocus, initialCaret, className }: Props) {
  const ref = useRef<HTMLTextAreaElement>(null)
  const [caret, setCaret] = useState(0)
  const [active, setActive] = useState(0)
  const [dismissed, setDismissed] = useState<number | null>(null)
  const pendingCaret = useRef<number | null>(null)
  const t = trigger(value, caret)
  const open = t !== null && dismissed !== t.start
  const types = useTypes()

  const { data: found } = useQuery({
    queryKey: ["resolve", t?.kind === "ref" ? t.query : ""],
    queryFn: () => api.resolve(t!.query),
    enabled: open && t?.kind === "ref" && t.query.trim().length > 0,
    staleTime: 5_000,
  })

  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    el.style.height = "auto"
    el.style.height = el.scrollHeight + "px"
    if (pendingCaret.current !== null) {
      el.setSelectionRange(pendingCaret.current, pendingCaret.current)
      pendingCaret.current = null
    }
  }, [value])

  useEffect(() => {
    if (autoFocus && ref.current) {
      ref.current.focus()
      const at = Math.min(initialCaret ?? ref.current.value.length, ref.current.value.length)
      ref.current.setSelectionRange(at, at)
      setCaret(at)
    }
    // Only on mount or when asked to focus again.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [autoFocus])

  const insert = (tr: Trigger, text: string) => {
    const next = replaceTrigger(value, caret, tr, text)
    pendingCaret.current = next.caret
    setCaret(next.caret)
    onChange(next.value)
  }

  const options: Option[] = []
  if (open && t) {
    const q = t.query.trim()
    if (t.kind === "ref") {
      for (const c of found?.candidates ?? []) {
        options.push({
          key: c.id,
          label: c.label || c.name,
          types: c.types,
          rich: true,
          hint: c.path?.filter((p) => p !== "Journal").slice(-2).join(" / "),
          apply: () => {
            intent.known.set(c.name.toLowerCase(), c.id)
            insert(t, `[[${c.name}]] `)
          },
        })
      }
      const exact = (found?.candidates ?? []).some((c) => c.name.toLowerCase() === q.toLowerCase())
      if (q && !exact) {
        for (const as of ["project", "person", ""]) {
          options.push({
            key: "create-" + as,
            label: as ? `Create “${q}” as #${as}` : `Create “${q}”`,
            apply: () => {
              intent.known.delete(q.toLowerCase())
              intent.create = intent.create.filter((c) => c.name.toLowerCase() !== q.toLowerCase())
              intent.create.push({ name: q, types: as ? [as] : [] })
              insert(t, `[[${q}]] `)
            },
          })
        }
      }
    } else {
      const lower = q.toLowerCase()
      for (const type of types.values()) {
        if (type.name.startsWith(lower)) {
          options.push({ key: type.name, label: "#" + type.name, color: type.color, apply: () => insert(t, `#${type.name} `) })
        }
      }
      if (lower && !types.has(lower) && /^[\p{L}\p{N}_-]+$/u.test(lower)) {
        options.push({
          key: "create-type",
          label: `Create type #${lower}`,
          apply: () => {
            intent.createTypes.push(lower)
            insert(t, `#${lower} `)
          },
        })
      }
    }
  }
  const index = Math.min(active, Math.max(options.length - 1, 0))

  const keyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (open && options.length > 0) {
      if (e.key === "ArrowDown" || e.key === "ArrowUp") {
        e.preventDefault()
        setActive((index + (e.key === "ArrowDown" ? 1 : options.length - 1)) % options.length)
        return
      }
      if (e.key === "Enter" || e.key === "Tab") {
        e.preventDefault()
        options[index].apply()
        setActive(0)
        return
      }
      if (e.key === "Escape") {
        e.preventDefault()
        setDismissed(t!.start)
        return
      }
    }
    if (onKey?.(e, e.currentTarget)) e.preventDefault()
  }

  return (
    <div className="relative w-full">
      <textarea
        ref={ref}
        rows={1}
        value={value}
        placeholder={placeholder}
        aria-label={placeholder ?? "Edit"}
        className={cn("m-0 block w-full resize-none overflow-hidden border-0 bg-transparent p-0 outline-none placeholder:text-muted-foreground", className)}
        onChange={(e) => {
          setCaret(e.target.selectionStart)
          setDismissed(null)
          onChange(e.target.value.replace(/\n/g, " "))
        }}
        onSelect={(e) => setCaret(e.currentTarget.selectionStart)}
        onKeyDown={keyDown}
        onBlur={() => onBlur?.()}
      />
      {open && options.length > 0 && (
        <div role="listbox" className="absolute left-0 top-full z-50 mt-1 w-80 max-w-[90vw] overflow-hidden rounded-md border bg-popover p-1 text-popover-foreground shadow-md">
          {options.map((o, i) => (
            <button
              key={o.key}
              role="option"
              aria-selected={i === index}
              type="button"
              onMouseDown={(e) => {
                e.preventDefault()
                o.apply()
                setActive(0)
                ref.current?.focus()
              }}
              onMouseEnter={() => setActive(i)}
              className={cn("flex w-full items-center justify-between gap-2 rounded-sm px-2 py-1.5 text-left text-sm", i === index && "bg-accent")}
            >
              <span className={cn("min-w-0 truncate", o.color && typeClass(o.color))}>{o.rich ? <Label text={o.label} types={o.types} /> : o.label}</span>
              {o.hint && <span className="truncate text-xs text-muted-foreground">{o.hint}</span>}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}
