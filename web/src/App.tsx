import { QueryClient, QueryClientProvider, useQuery } from "@tanstack/react-query"
import { BrowserRouter, Route, Routes } from "react-router"
import { api } from "@/lib/api"
import { Toaster } from "@/components/ui/sonner"
import { TooltipProvider } from "@/components/ui/tooltip"
import { Layout } from "@/components/layout"
import { ActivityPage } from "@/pages/activity"
import { AgendaPage } from "@/pages/agenda"
import { AuthorizePage, SignIn } from "@/pages/auth"
import { DayPage, JournalPage } from "@/pages/journal"
import { NodePage } from "@/pages/node"
import { SearchPage } from "@/pages/search"
import { SettingsPage } from "@/pages/settings"
import { SharePage } from "@/pages/share"
import { TasksPage } from "@/pages/tasks"
import { TodayPage } from "@/pages/today"
import { TypePage, TypesPage } from "@/pages/types"

export function makeClient() {
  return new QueryClient({ defaultOptions: { queries: { refetchOnWindowFocus: true, retry: 1, staleTime: 2_000 } } })
}

function Gate() {
  const { data: session, error } = useQuery({ queryKey: ["session"], queryFn: api.session })
  if (error) return <p className="p-8 text-muted-foreground">Vrac is not reachable.</p>
  if (!session) return null
  if (!session.authenticated) return <SignIn owner={session.owner} />
  return (
    <Routes>
      <Route element={<Layout />}>
        <Route index element={<TodayPage />} />
        <Route path="journal" element={<JournalPage />} />
        <Route path="day/:day" element={<DayPage />} />
        <Route path="node/:id" element={<NodePage />} />
        <Route path="tasks" element={<TasksPage />} />
        <Route path="agenda" element={<AgendaPage />} />
        <Route path="types" element={<TypesPage />} />
        <Route path="type/:name" element={<TypePage />} />
        <Route path="search" element={<SearchPage />} />
        <Route path="activity" element={<ActivityPage />} />
        <Route path="settings" element={<SettingsPage />} />
        <Route path="authorize" element={<AuthorizePage />} />
        <Route path="share" element={<SharePage />} />
        <Route path="*" element={<p className="text-muted-foreground">Nothing here.</p>} />
      </Route>
    </Routes>
  )
}

export function App({ client = makeClient() }: { client?: QueryClient }) {
  return (
    <QueryClientProvider client={client}>
      <TooltipProvider>
        <BrowserRouter>
          <Gate />
        </BrowserRouter>
        <Toaster position="bottom-right" />
      </TooltipProvider>
    </QueryClientProvider>
  )
}
