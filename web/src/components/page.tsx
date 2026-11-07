import type { ReactNode } from "react"
import { Skeleton } from "@/components/ui/skeleton"

export function PageTitle({ children, actions }: { children: ReactNode; actions?: ReactNode }) {
  return (
    <div className="mb-6 flex flex-wrap items-center justify-between gap-2">
      <h1 className="text-xl font-semibold tracking-tight">{children}</h1>
      {actions && <div className="flex items-center gap-2">{actions}</div>}
    </div>
  )
}

export function Section({ title, children, count }: { title: string; children: ReactNode; count?: number }) {
  return (
    <section className="mb-8">
      <h2 className="mb-1.5 flex items-center gap-2 text-[11px] font-semibold uppercase tracking-[0.08em] text-muted-foreground/80">
        {title}
        {count !== undefined && <span className="tabular-nums">{count}</span>}
      </h2>
      {children}
    </section>
  )
}

export function Loading() {
  return (
    <div className="space-y-2">
      <Skeleton className="h-8 w-48" />
      <Skeleton className="h-5 w-full" />
      <Skeleton className="h-5 w-2/3" />
    </div>
  )
}
