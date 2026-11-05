import { Link } from "react-router"
import { CalendarIcon, ClockIcon } from "lucide-react"
import type { Node, Refs } from "@/lib/api"
import { typeClass } from "@/lib/colors"
import { useTypes } from "@/lib/hooks"
import { formatDay, formatMoment, inlineTypes, localDate, segments } from "@/lib/syntax"
import { cn } from "@/lib/utils"

export function TypeBadge({ name }: { name: string }) {
  const types = useTypes()
  return (
    <Link
      to={`/type/${name}`}
      onClick={(e) => e.stopPropagation()}
      className={cn("inline-flex items-center rounded px-1.5 py-px text-xs font-medium leading-5", typeClass(types.get(name)?.color))}
    >
      #{name}
    </Link>
  )
}

export function RefChip({ id, refs }: { id: string; refs: Refs }) {
  const r = refs[id]
  const types = useTypes()
  const color = r?.types[0] ? types.get(r.types[0])?.color : undefined
  return (
    <Link
      to={`/node/${id}`}
      onClick={(e) => e.stopPropagation()}
      className={cn(
        "rounded px-1 font-medium underline-offset-2 hover:underline",
        color ? typeClass(color) : "text-primary underline decoration-muted-foreground/40",
        r?.trashed && "line-through opacity-60",
      )}
    >
      {r?.name ?? "missing"}
    </Link>
  )
}

export function DateChip({ node }: { node: Node }) {
  const today = localDate()
  if (node.due) {
    const late = node.status === "open" && node.due < today
    return (
      <span className={cn("inline-flex items-center gap-1 text-xs text-muted-foreground", late && "text-destructive", node.due === today && "text-foreground")}>
        <CalendarIcon className="size-3" />
        {node.due === today ? "Today" : formatDay(node.due)}
      </span>
    )
  }
  if (node.start) {
    return (
      <span className="inline-flex items-center gap-1 text-xs text-muted-foreground">
        <ClockIcon className="size-3" />
        {formatMoment(node.start)}
        {node.end && ` → ${formatMoment(node.end)}`}
      </span>
    )
  }
  return null
}

/** Renders canonical text with reference chips, type badges where they are
 * written (other types after the text), and dates. */
export function NodeText({ node, refs, className, hide }: { node: Node; refs: Refs; className?: string; hide?: string }) {
  const inline = inlineTypes(node.text)
  return (
    <span className={cn("break-words", node.status === "done" && "text-muted-foreground line-through", className)}>
      {segments(node.text).map((s, i) =>
        "ref" in s ? <RefChip key={i} id={s.ref} refs={refs} /> : "type" in s ? (s.type === hide ? null : <TypeBadge key={i} name={s.type} />) : <span key={i}>{s.text}</span>,
      )}
      {node.text === "" && node.types.length === 0 && <span className="text-muted-foreground">&nbsp;</span>}
      {node.types.filter((t) => !inline.has(t) && t !== hide).map((t) => (
        <span key={t} className="ml-1.5 inline-block align-baseline">
          <TypeBadge name={t} />
        </span>
      ))}
      <span className="ml-2 inline-block">
        <DateChip node={node} />
      </span>
    </span>
  )
}
