import { useEffect, useMemo, useState } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { toast } from "sonner"
import { ApiError, api, type Type } from "./api"

/** The value once it has stopped changing for a moment: what typing
 * sends to the server, so results do not flicker at every key. */
export function useDebounced<T>(value: T, delay = 150): T {
  const [settled, setSettled] = useState(value)
  useEffect(() => {
    // Leaving or reaching empty happens at once: a menu opens full and
    // the first letter is asked without waiting for a pause.
    if (settled === "" || value === "") {
      setSettled(value)
      return
    }
    const t = setTimeout(() => setSettled(value), delay)
    return () => clearTimeout(t)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [value, delay])
  return settled
}

/** The user's tags by name. */
export function useTypes(): Map<string, Type> {
  const { data } = useQuery({ queryKey: ["types"], queryFn: api.types, staleTime: 60_000 })
  return useMemo(() => new Map((data?.types ?? []).map((t) => [t.name, t])), [data])
}

/** Refreshes every view after a mutation. Personal workspaces are small. */
export function useRefresh() {
  const client = useQueryClient()
  return () => client.invalidateQueries()
}

export function reportError(err: unknown) {
  if (err instanceof ApiError) {
    if (err.code === "unresolved") {
      const names = err.unresolved.map((u) => (u.kind === "type" ? "#" : "") + u.name).join(", ")
      toast.error(`Unknown ${names}`, { description: "Pick it from the suggestions or choose Create." })
      return
    }
    if (err.code === "conflict") {
      toast.error("This changed elsewhere", { description: "The latest version is now shown." })
      return
    }
    toast.error(err.message)
    return
  }
  toast.error(String(err))
}
