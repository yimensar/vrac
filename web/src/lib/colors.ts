// Static class lists so Tailwind keeps them in the build.
const colors: Record<string, string> = {
  blue: "bg-blue-100 text-blue-800 dark:bg-blue-950 dark:text-blue-300",
  violet: "bg-violet-100 text-violet-800 dark:bg-violet-950 dark:text-violet-300",
  amber: "bg-amber-100 text-amber-800 dark:bg-amber-950 dark:text-amber-300",
  emerald: "bg-emerald-100 text-emerald-800 dark:bg-emerald-950 dark:text-emerald-300",
  rose: "bg-rose-100 text-rose-800 dark:bg-rose-950 dark:text-rose-300",
  cyan: "bg-cyan-100 text-cyan-800 dark:bg-cyan-950 dark:text-cyan-300",
  lime: "bg-lime-100 text-lime-800 dark:bg-lime-950 dark:text-lime-300",
  orange: "bg-orange-100 text-orange-800 dark:bg-orange-950 dark:text-orange-300",
  fuchsia: "bg-fuchsia-100 text-fuchsia-800 dark:bg-fuchsia-950 dark:text-fuchsia-300",
  teal: "bg-teal-100 text-teal-800 dark:bg-teal-950 dark:text-teal-300",
  sky: "bg-sky-100 text-sky-800 dark:bg-sky-950 dark:text-sky-300",
  pink: "bg-pink-100 text-pink-800 dark:bg-pink-950 dark:text-pink-300",
}

export function typeClass(color: string | undefined): string {
  return colors[color ?? ""] ?? "bg-muted text-muted-foreground"
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
