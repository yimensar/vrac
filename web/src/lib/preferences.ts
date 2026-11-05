import { useEffect } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { api, current, type Preferences } from "./api"
import { reportError } from "./hooks"

/** Loads the preferences, keeps them in effect and applies the theme. */
export function usePreferences(): Preferences {
  const { data } = useQuery({ queryKey: ["preferences"], queryFn: api.preferences, staleTime: 60_000 })
  const p = data ?? current
  Object.assign(current, p)
  useEffect(() => {
    const root = document.documentElement
    root.classList.toggle("dark", p.theme === "dark")
    root.classList.toggle("light", p.theme === "light")
  }, [p.theme])
  return p
}

export function useSetPreferences() {
  const client = useQueryClient()
  return async (change: Partial<Preferences>) => {
    try {
      const next = await api.setPreferences({ ...current, ...change })
      Object.assign(current, next)
      client.setQueryData(["preferences"], next)
    } catch (err) {
      reportError(err)
    }
  }
}
