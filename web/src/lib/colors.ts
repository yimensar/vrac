// Static class lists so Tailwind keeps them in the build.
const colors: Record<string, string> = {
  blue: "text-blue-600 dark:text-blue-400",
  violet: "text-violet-600 dark:text-violet-400",
  amber: "text-amber-600 dark:text-amber-400",
  emerald: "text-emerald-600 dark:text-emerald-400",
  rose: "text-rose-600 dark:text-rose-400",
  cyan: "text-cyan-600 dark:text-cyan-400",
  lime: "text-lime-600 dark:text-lime-400",
  orange: "text-orange-600 dark:text-orange-400",
  fuchsia: "text-fuchsia-600 dark:text-fuchsia-400",
  teal: "text-teal-600 dark:text-teal-400",
  sky: "text-sky-600 dark:text-sky-400",
  pink: "text-pink-600 dark:text-pink-400",
}

export function typeClass(color: string | undefined): string {
  return colors[color ?? ""] ?? "text-muted-foreground"
}

const dots: Record<string, string> = {
  blue: "bg-blue-500",
  violet: "bg-violet-500",
  amber: "bg-amber-500",
  emerald: "bg-emerald-500",
  rose: "bg-rose-500",
  cyan: "bg-cyan-500",
  lime: "bg-lime-500",
  orange: "bg-orange-500",
  fuchsia: "bg-fuchsia-500",
  teal: "bg-teal-500",
  sky: "bg-sky-500",
  pink: "bg-pink-500",
}

/** The bullet of a typed node. */
export function dotClass(color: string | undefined): string {
  return dots[color ?? ""] ?? "bg-foreground/40"
}
