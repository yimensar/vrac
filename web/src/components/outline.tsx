import { createContext, useContext, useEffect, useState, type KeyboardEvent, type ReactNode } from "react"
import { Link } from "react-router"
import { useQuery } from "@tanstack/react-query"
import { ChevronRightIcon } from "lucide-react"
import { api, type Node, type Placement, type Refs } from "@/lib/api"
import { reportError, useRefresh } from "@/lib/hooks"
import { toEditable, toInput } from "@/lib/syntax"
import { cn } from "@/lib/utils"
import { Checkbox } from "@/components/ui/checkbox"
import { Editor, newIntent, type Intent } from "./editor"
import { NodeText } from "./node-text"

// Which node should open in edit mode once it renders.
const FocusContext = createContext<{ focus: string | null; setFocus: (id: string | null) => void }>({ focus: null, setFocus: () => {} })

export function FocusProvider({ children }: { children: ReactNode }) {
  const [focus, setFocus] = useState<string | null>(null)
  return <FocusContext.Provider value={{ focus, setFocus }}>{children}</FocusContext.Provider>
}

// Entities are collapsed by default: their content is shown on their page.
const collapsedTypes = new Set(["project", "person"])

type OutlineProps = {
  nodes: Node[]
  refs: Refs
  /** Where the composer appends new nodes. */
  placement: Placement
  /** The node whose children these are, when nested. */
  parent?: Node
  composer?: boolean
  placeholder?: string
}

export function Outline({ nodes, refs, placement, parent, composer, placeholder }: OutlineProps) {
  return (
    <div>
      {nodes.map((n, i) => (
        <NodeRow key={n.id} node={n} refs={refs} prev={nodes[i - 1]} parent={parent} />
      ))}
      {composer && <Composer placement={placement} placeholder={placeholder} />}
    </div>
  )
}

export function Composer({ placement, placeholder, autoFocus }: { placement: Placement; placeholder?: string; autoFocus?: boolean }) {
  const [value, setValue] = useState("")
  const [intent, setIntent] = useState<Intent>(() => newIntent())
  const refresh = useRefresh()
  const submit = async () => {
    if (!value.trim()) return
    try {
      await api.create({ placement, nodes: [{ input: toInput(value.trim(), intent.known) }], create: intent.create, create_types: intent.createTypes })
      setValue("")
      setIntent(newIntent())
      refresh()
    } catch (err) {
      reportError(err)
    }
  }
  return (
    <div className="flex items-start gap-1.5 py-1 pl-5">
      <span className="mt-2.5 size-1.5 shrink-0 rounded-full bg-muted-foreground/30" />
      <Editor
        value={value}
        onChange={setValue}
        intent={intent}
        autoFocus={autoFocus}
        placeholder={placeholder ?? "Write… #task #meeting [[reference]]"}
        className="py-0.5 leading-6"
        onKey={(e) => {
          if (e.key === "Enter" && !e.shiftKey) {
            submit()
            return true
          }
          return false
        }}
      />
    </div>
  )
}

function Children({ node }: { node: Node }) {
  const { data } = useQuery({ queryKey: ["children", node.id], queryFn: () => api.children(node.id) })
  if (!data) return null
  return (
    <div className="ml-[0.6rem] border-l pl-2">
      <Outline nodes={data.nodes} refs={data.refs} placement={{ parent_id: node.id }} parent={node} />
    </div>
  )
}

function NodeRow({ node, refs, prev, parent }: { node: Node; refs: Refs; prev?: Node; parent?: Node }) {
  const { focus, setFocus } = useContext(FocusContext)
  const refresh = useRefresh()
  const [editing, setEditing] = useState(false)
  const [value, setValue] = useState("")
  const [original, setOriginal] = useState("")
  const [intent, setIntent] = useState<Intent>(() => newIntent())
  const [expanded, setExpanded] = useState(!node.types.some((t) => collapsedTypes.has(t)))

  const startEdit = () => {
    const known = new Map<string, string>()
    const text = toEditable(node, refs, known)
    setIntent(newIntent(known))
    setValue(text)
    setOriginal(text)
    setEditing(true)
  }

  useEffect(() => {
    if (focus === node.id) {
      setFocus(null)
      startEdit()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [focus, node.id])

  /** Saves the edit and returns the node at its latest revision. */
  const commit = async (): Promise<Node | null> => {
    if (value === original) return node
    try {
      const res = await api.update(node, { input: toInput(value.trim(), intent.known), create: intent.create, create_types: intent.createTypes })
      setOriginal(value)
      return res.nodes[0]
    } catch (err) {
      reportError(err)
      return null
    }
  }

  const finish = async () => {
    const saved = await commit()
    if (saved) {
      setEditing(false)
      refresh()
    }
  }

  const onKey = (e: KeyboardEvent<HTMLTextAreaElement>, el: HTMLTextAreaElement): boolean => {
    if (e.key === "Enter" && !e.shiftKey) {
      ;(async () => {
        const saved = await commit()
        if (!saved) return
        try {
          const placement: Placement = saved.parent_id ? { parent_id: saved.parent_id, after_id: saved.id } : { root: true, after_id: saved.id }
          const res = await api.create({ placement, nodes: [{ input: "" }] })
          setEditing(false)
          setFocus(res.nodes[0].id)
          refresh()
        } catch (err) {
          reportError(err)
        }
      })()
      return true
    }
    if (e.key === "Tab") {
      const target: Placement | null = e.shiftKey
        ? parent
          ? parent.parent_id
            ? { parent_id: parent.parent_id, after_id: parent.id }
            : { root: true, after_id: parent.id }
          : null
        : prev
          ? { parent_id: prev.id }
          : null
      if (!target) return true
      ;(async () => {
        const saved = await commit()
        if (!saved) return
        try {
          await api.move(saved, target)
          setFocus(saved.id)
          refresh()
        } catch (err) {
          reportError(err)
        }
      })()
      return true
    }
    if (e.key === "Backspace" && value === "" && el.selectionStart === 0) {
      ;(async () => {
        try {
          await api.trash(node)
          if (prev) setFocus(prev.id)
          refresh()
        } catch (err) {
          reportError(err)
        }
      })()
      return true
    }
    if (e.key === "Escape") {
      setEditing(false)
      return true
    }
    return false
  }

  const toggle = async () => {
    try {
      await api.update(node, { status: node.status === "done" ? "open" : "done" })
      refresh()
    } catch (err) {
      reportError(err)
    }
  }

  const isTask = node.types.includes("task")
  return (
    <div>
      <div className="group flex items-start gap-1.5 rounded-sm py-0.5">
        <button
          type="button"
          aria-label={expanded ? "Collapse" : "Expand"}
          onClick={() => setExpanded(!expanded)}
          className={cn("mt-1.5 size-4 shrink-0 text-muted-foreground transition-transform", expanded && "rotate-90", node.child_count === 0 && "invisible")}
        >
          <ChevronRightIcon className="size-4" />
        </button>
        <Link to={`/node/${node.id}`} aria-label="Zoom in" className="mt-1 flex size-4 shrink-0 items-center justify-center">
          <span className={cn("size-1.5 rounded-full bg-foreground/70 transition-transform group-hover:scale-150", node.child_count > 0 && !expanded && "ring-4 ring-muted-foreground/25")} />
        </Link>
        {isTask && <Checkbox checked={node.status === "done"} onCheckedChange={toggle} aria-label="Done" className="mt-1" />}
        <div className="min-w-0 flex-1 py-0.5 leading-6">
          {editing ? (
            <Editor value={value} onChange={setValue} intent={intent} onKey={onKey} onBlur={finish} autoFocus className="leading-6" />
          ) : (
            <div role="button" tabIndex={0} onClick={startEdit} onKeyDown={(e) => e.key === "Enter" && startEdit()} className="cursor-text">
              <NodeText node={node} refs={refs} />
            </div>
          )}
        </div>
      </div>
      {expanded && node.child_count > 0 && <Children node={node} />}
    </div>
  )
}

/** A read-only list item for searches and sections. */
export function Item({ node, refs, showParent = true }: { node: Node; refs: Refs; showParent?: boolean }) {
  const refresh = useRefresh()
  const toggle = async () => {
    try {
      await api.update(node, { status: node.status === "done" ? "open" : "done" })
      refresh()
    } catch (err) {
      reportError(err)
    }
  }
  const parent = node.parent_id ? refs[node.parent_id] : undefined
  return (
    <div className="flex items-start gap-2 py-1">
      {node.types.includes("task") ? (
        <Checkbox checked={node.status === "done"} onCheckedChange={toggle} aria-label="Done" className="mt-1" />
      ) : (
        <span className="mt-2.5 size-1.5 shrink-0 rounded-full bg-foreground/60" />
      )}
      <div className="min-w-0 flex-1 leading-6">
        <Link to={`/node/${node.id}`} className="hover:underline">
          <NodeText node={node} refs={refs} />
        </Link>
        {showParent && parent && (
          <div className="truncate text-xs text-muted-foreground">
            in{" "}
            <Link to={`/node/${parent.id}`} className="hover:underline">
              {parent.name}
            </Link>
          </div>
        )}
      </div>
    </div>
  )
}
