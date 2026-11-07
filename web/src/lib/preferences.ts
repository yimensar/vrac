import { useEffect } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { api, current, type Preferences } from "./api"
import { reportError } from "./hooks"

// The page background of each theme, for the browser and system bars.
const bars = { light: "#ffffff", dark: "#0a0a0a" }

/** Loads the preferences, keeps them in effect and applies the theme. */
export function usePreferences(): Preferences {
  const { data } = useQuery({ queryKey: ["preferences"], queryFn: api.preferences, staleTime: 60_000 })
  const p = data ?? current
  Object.assign(current, p)
  useEffect(() => {
    const root = document.documentElement
    root.classList.toggle("dark", p.theme === "dark")
    root.classList.toggle("light", p.theme === "light")
    for (const meta of document.querySelectorAll<HTMLMetaElement>('meta[name="theme-color"]')) {
      const own = meta.media.includes("dark") ? bars.dark : bars.light
      meta.content = p.theme === "system" ? own : bars[p.theme]
    }
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
