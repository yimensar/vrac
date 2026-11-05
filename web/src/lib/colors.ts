// Static class lists so Tailwind keeps them in the build.
const colors: Record<string, string> = {
  blue: "bg-blue-500/10 text-blue-700 dark:text-blue-300",
  violet: "bg-violet-500/10 text-violet-700 dark:text-violet-300",
  amber: "bg-amber-500/10 text-amber-700 dark:text-amber-300",
  emerald: "bg-emerald-500/10 text-emerald-700 dark:text-emerald-300",
  rose: "bg-rose-500/10 text-rose-700 dark:text-rose-300",
  cyan: "bg-cyan-500/10 text-cyan-700 dark:text-cyan-300",
  lime: "bg-lime-500/10 text-lime-700 dark:text-lime-300",
  orange: "bg-orange-500/10 text-orange-700 dark:text-orange-300",
  fuchsia: "bg-fuchsia-500/10 text-fuchsia-700 dark:text-fuchsia-300",
  teal: "bg-teal-500/10 text-teal-700 dark:text-teal-300",
  sky: "bg-sky-500/10 text-sky-700 dark:text-sky-300",
  pink: "bg-pink-500/10 text-pink-700 dark:text-pink-300",
}

export function typeClass(color: string | undefined): string {
  return colors[color ?? ""] ?? "bg-muted/60 text-muted-foreground"
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
  return dots[color ?? ""] ?? "bg-foreground/70"
}
