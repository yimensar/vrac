import { useEffect, useRef } from "react"
import { useNavigate } from "react-router"
import { ArrowDownIcon, ArrowUpIcon, CheckIcon, ChevronDownIcon, IndentDecreaseIcon, IndentIncreaseIcon, Maximize2Icon } from "lucide-react"
import { cn } from "@/lib/utils"
import { END, GHOST, type OutlineStore } from "./store"

/** On touch screens there is no Tab, Alt or Shift+arrow: this bar sits
 * above the keyboard while a row is edited and offers those keys. */
export function TouchBar({ store }: { store: OutlineStore }) {
  const navigate = useNavigate()
  const bar = useRef<HTMLDivElement>(null)
  const editing = store.editing
  // The bar follows the visual viewport, which shrinks when the keyboard
  // opens; a fixed bottom would hide behind it on iOS.
  useEffect(() => {
    const vv = window.visualViewport
    const el = bar.current
    if (!vv || !el) return
    const place = () => {
      el.style.top = `${vv.offsetTop + vv.height}px`
    }
    place()
    vv.addEventListener("resize", place)
    vv.addEventListener("scroll", place)
    return () => {
      vv.removeEventListener("resize", place)
      vv.removeEventListener("scroll", place)
    }
  }, [editing])
  if (!editing) return null
  const k = editing.key
  const e = k === GHOST ? undefined : store.entry(k)
  const task = e?.node?.types.includes("task")
  const button = "flex size-10 shrink-0 items-center justify-center rounded-md text-foreground/80 active:bg-accent disabled:opacity-30"
  // pointerdown is prevented so the editor keeps its focus and the keyboard
  // stays open while a key is tapped.
  const key = (label: string, icon: React.ReactNode, run: () => void, disabled = false) => (
    <button type="button" aria-label={label} className={button} disabled={disabled} onPointerDown={(ev) => ev.preventDefault()} onClick={run}>
      {icon}
    </button>
  )
  return (
    <div
      ref={bar}
      className={cn(
        "fixed inset-x-0 z-40 hidden -translate-y-full items-center gap-1 border-t bg-background/95 px-2 py-1 backdrop-blur pointer-coarse:flex",
      )}
      style={{ top: "100%" }}
    >
      {key("Outdent", <IndentDecreaseIcon className="size-5" />, () => store.outdent(k, END), !e)}
      {key("Indent", <IndentIncreaseIcon className="size-5" />, () => store.indent(k, END), !e)}
      {key("Move up", <ArrowUpIcon className="size-5" />, () => store.shift(k, -1, END), !e)}
      {key("Move down", <ArrowDownIcon className="size-5" />, () => store.shift(k, 1, END), !e)}
      {task && key("Done", <CheckIcon className="size-5" />, () => store.toggleDone(k))}
      {e?.id && key("Zoom in", <Maximize2Icon className="size-5" />, () => navigate(`/node/${e.id}`))}
      <span className="flex-1" />
      {key("Hide keyboard", <ChevronDownIcon className="size-5" />, () => (document.activeElement as HTMLElement | null)?.blur())}
    </div>
  )
}
