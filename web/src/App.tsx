import { Auth } from "@/components/Auth";
import { Composer } from "@/components/Composer";
import { NoteCard, NoteMenu } from "@/components/NoteCard";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  ArrowDown,
  ArrowDownWideNarrow,
  ArrowLeft,
  ArrowUpRight,
  Archive,
  Check,
  ChevronDown,
  ChevronRight,
  Code2,
  Command,
  Copy,
  Download,
  FileText,
  Hash,
  History,
  Inbox,
  LayoutList,
  Link2,
  LoaderCircle,
  LogOut,
  Menu,
  Moon,
  Network,
  Paperclip,
  PenLine,
  Pin,
  Plus,
  Search,
  Settings2,
  Sun,
  Trash2,
  Waypoints,
  X,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { TooltipProvider } from "@/components/ui/tooltip";
import { Brand } from "@/components/Brand";
import { Markdown } from "@/components/Markdown";
import {
  api,
  APIError,
  formatDate,
  formatTime,
  type Note,
  type Page,
  type Session,
  type Stats,
  type Tag,
} from "@/lib/api";
import { cn } from "@/lib/utils";

type View = "active" | "archived" | "deleted" | "pinned";
const emptyStats: Stats = { active: 0, archived: 0, tags: 0, attachments: 0 };
const viewLabels: Record<View, string> = {
  active: "Toutes les notes",
  archived: "Archives",
  deleted: "Corbeille",
  pinned: "Épinglées",
};
const initialDraft = () => {
  try {
    return localStorage.getItem("vrac-draft") || "";
  } catch {
    return "";
  }
};

type CaptureAttempt = { key: string; content: string; uncertain: boolean };
function readCaptureAttempt(): CaptureAttempt | null {
  try {
    const value = JSON.parse(
      localStorage.getItem("vrac-capture-attempt") || "null",
    );
    return value &&
      typeof value.key === "string" &&
      typeof value.content === "string"
      ? value
      : null;
  } catch {
    return null;
  }
}
function rememberCaptureAttempt(value: CaptureAttempt | null) {
  try {
    if (value)
      localStorage.setItem("vrac-capture-attempt", JSON.stringify(value));
    else localStorage.removeItem("vrac-capture-attempt");
  } catch {
    /* Capture remains available when browser storage is full. */
  }
}

export default function App() {
  const [session, setSession] = useState<Session | null>(null),
    [bootError, setBootError] = useState("");
  const [notes, setNotes] = useState<Note[]>([]),
    [tags, setTags] = useState<Tag[]>([]),
    [stats, setStats] = useState<Stats>(emptyStats),
    [page, setPage] = useState<Page | null>(null);
  const [query, setQuery] = useState(""),
    [debouncedQuery, setDebouncedQuery] = useState(""),
    [tag, setTag] = useState(""),
    [view, setView] = useState<View>("active"),
    [outline, setOutline] = useState(
      () => localStorage.getItem("vrac-view") === "outline",
    ),
    [collapsed, setCollapsed] = useState<Set<string>>(new Set()),
    [sort, setSort] = useState<"recent" | "oldest">("recent");
  const [loading, setLoading] = useState(false),
    [loadingMore, setLoadingMore] = useState(false),
    [loadError, setLoadError] = useState(""),
    [refresh, setRefresh] = useState(0),
    [toast, setToast] = useState(""),
    [mobileNav, setMobileNav] = useState(false),
    [createOpen, setCreateOpen] = useState(false),
    [connectionOpen, setConnectionOpen] = useState(false),
    [shortcutsOpen, setShortcutsOpen] = useState(false);
  const [draft, setDraft] = useState(initialDraft),
    [source, setSource] = useState(false),
    [saving, setSaving] = useState(false),
    [selected, setSelected] = useState<Note | null>(null),
    [editing, setEditing] = useState(false),
    [editContent, setEditContent] = useState(""),
    [editSource, setEditSource] = useState(false),
    [history, setHistory] = useState<Note[] | null>(null),
    [historyLoading, setHistoryLoading] = useState(false);
  const [relationTitles, setRelationTitles] = useState<Record<string, string>>(
    {},
  );
  const [dark, setDark] = useState(
      () => localStorage.getItem("vrac-theme") === "dark",
    ),
    [keyboardIndex, setKeyboardIndex] = useState(-1);
  const searchRef = useRef<HTMLInputElement>(null),
    captureAttempt = useRef<CaptureAttempt | null>(readCaptureAttempt()),
    abortRef = useRef<AbortController | null>(null),
    requestID = useRef(0),
    deepLinkOpened = useRef(false),
    selectedIDRef = useRef<string | null>(null),
    historyRequestID = useRef(0);
  const notify = useCallback((message: string) => setToast(message), []);
  const loadSession = useCallback(() => {
    setBootError("");
    api<Session>("/session")
      .then(setSession)
      .catch((e) => setBootError(e.message));
  }, []);
  useEffect(() => {
    loadSession();
  }, [loadSession]);
  useEffect(() => {
    document.documentElement.classList.toggle("dark", dark);
    document.documentElement.style.colorScheme = dark ? "dark" : "light";
    localStorage.setItem("vrac-theme", dark ? "dark" : "light");
  }, [dark]);
  useEffect(() => {
    localStorage.setItem("vrac-view", outline ? "outline" : "list");
  }, [outline]);
  useEffect(() => {
    const timer = setTimeout(() => setDebouncedQuery(query.trim()), 180);
    return () => clearTimeout(timer);
  }, [query]);
  useEffect(() => {
    const timer = setTimeout(() => {
      try {
        localStorage.setItem("vrac-draft", draft);
      } catch {
        /* Storage quota cannot interrupt writing. */
      }
    }, 300);
    return () => clearTimeout(timer);
  }, [draft]);
  useEffect(() => {
    if (!toast) return;
    const timer = setTimeout(() => setToast(""), 4200);
    return () => clearTimeout(timer);
  }, [toast]);
  useEffect(() => {
    if (!session?.authenticated) return;
    Promise.all([api<Tag[]>("/tags"), api<Stats>("/stats")])
      .then(([nextTags, nextStats]) => {
        setTags(nextTags || []);
        setStats(nextStats);
      })
      .catch((e) => {
        if (e instanceof APIError && e.status === 401) loadSession();
      });
  }, [session?.authenticated, refresh, loadSession]);
  const noteParams = useCallback(
    (cursor?: string) => {
      const params = new URLSearchParams({
        state: view === "pinned" ? "active" : view,
        limit: "30",
        sort,
      });
      if (debouncedQuery) params.set("q", debouncedQuery);
      if (tag) params.set("tag", tag);
      if (cursor) params.set("cursor", cursor);
      if (view === "pinned") params.set("pinned", "true");
      return params;
    },
    [view, sort, debouncedQuery, tag],
  );
  useEffect(() => {
    if (!session?.authenticated) return;
    abortRef.current?.abort();
    const controller = new AbortController();
    abortRef.current = controller;
    const id = ++requestID.current;
    setLoading(true);
    setLoadError("");
    setKeyboardIndex(-1);
    api<Page>(`/notes?${noteParams()}`, { signal: controller.signal })
      .then((result) => {
        if (id !== requestID.current) return;
        setNotes(result.notes || []);
        setPage(result);
      })
      .catch((e) => {
        if (e.name === "AbortError" || id !== requestID.current) return;
        if (e instanceof APIError && e.status === 401) {
          loadSession();
          return;
        }
        setLoadError(e.message);
      })
      .finally(() => {
        if (id === requestID.current) setLoading(false);
      });
    return () => controller.abort();
  }, [session?.authenticated, noteParams, refresh, loadSession]);
  const filteredNotes = useMemo(
    () => (view === "pinned" ? notes.filter((n) => n.pinned) : notes),
    [notes, view],
  );
  const refreshNotes = () => setRefresh((r) => r + 1);
  async function loadMore() {
    if (!page?.nextCursor || loadingMore) return;
    setLoadingMore(true);
    const id = requestID.current;
    try {
      const result = await api<Page>(`/notes?${noteParams(page.nextCursor)}`);
      if (id !== requestID.current) return;
      setNotes((old) => {
        const seen = new Set(old.map((n) => n.id));
        return [...old, ...result.notes.filter((n) => !seen.has(n.id))];
      });
      setPage(result);
    } catch (e) {
      notify((e as Error).message);
    } finally {
      setLoadingMore(false);
    }
  }
  async function saveDraft(content = draft) {
    if (saving || !content.trim()) return;
    const previous = captureAttempt.current;
    const changedAfterUncertain =
      previous?.uncertain && previous.content !== content;
    const attempt: CaptureAttempt = {
      key: previous?.content === content ? previous.key : crypto.randomUUID(),
      content,
      uncertain: false,
    };
    captureAttempt.current = attempt;
    rememberCaptureAttempt(attempt);
    setDraft(content);
    setSaving(true);
    try {
      await api<Note>("/notes", {
        method: "POST",
        body: JSON.stringify({
          content,
          source: "web",
          idempotencyKey: attempt.key,
        }),
      });
      setDraft("");
      localStorage.removeItem("vrac-draft");
      captureAttempt.current = null;
      rememberCaptureAttempt(null);
      setCreateOpen(false);
      refreshNotes();
      notify(
        changedAfterUncertain
          ? "Nouvelle note enregistrée. La tentative précédente a peut-être aussi été enregistrée : vérifiez le fil."
          : "Une idée de plus, bien gardée.",
      );
    } catch (e) {
      attempt.uncertain = !(e instanceof APIError) || e.status >= 500;
      rememberCaptureAttempt(attempt);
      notify(
        (e as Error).message +
          (attempt.uncertain
            ? " La note a peut-être été enregistrée. Réessayez sans modifier le texte pour éviter un doublon."
            : ""),
      );
    } finally {
      setSaving(false);
    }
  }
  async function updateNote(note: Note, patch: Partial<Note>) {
    try {
      const updated = await api<Note>(`/notes/${encodeURIComponent(note.id)}`, {
        method: "PATCH",
        body: JSON.stringify({ ...patch, version: note.version }),
      });
      setSelected((current) => (current?.id === note.id ? updated : current));
      setNotes((old) => old.map((n) => (n.id === note.id ? updated : n)));
      refreshNotes();
      notify(
        patch.state === "archived"
          ? "Note archivée."
          : patch.state === "deleted"
            ? "Note déplacée dans la corbeille."
            : patch.state === "active"
              ? "Note restaurée."
              : patch.pinned !== undefined
                ? patch.pinned
                  ? "Note épinglée."
                  : "Note désépinglée."
                : "Modifications enregistrées.",
      );
      return updated;
    } catch (e) {
      notify(
        e instanceof APIError && e.status === 409
          ? "Cette note a changé depuis son ouverture. Copiez vos modifications, puis rouvrez-la."
          : (e as Error).message,
      );
      return null;
    }
  }
  function focusNote(note: Note, edit = false) {
    if (saving) return false;
    if (
      editing &&
      selected &&
      editContent !== selected.content &&
      !window.confirm("Quitter sans enregistrer vos modifications ?")
    )
      return false;
    selectedIDRef.current = note.id;
    historyRequestID.current++;
    setHistoryLoading(false);
    setSelected(note);
    setEditContent(note.content);
    setEditing(edit);
    setHistory(null);
    setEditSource(false);
    return true;
  }
  async function showHistory(note: Note) {
    if (saving) return;
    if (!focusNote(note)) return;
    const request = ++historyRequestID.current;
    setHistoryLoading(true);
    try {
      const revisions = await api<Note[]>(
        `/notes/${encodeURIComponent(note.id)}/history`,
      );
      if (
        request === historyRequestID.current &&
        selectedIDRef.current === note.id
      )
        setHistory(revisions);
    } catch (e) {
      if (request === historyRequestID.current) notify((e as Error).message);
    } finally {
      if (request === historyRequestID.current) setHistoryLoading(false);
    }
  }
  async function saveEdit(content = editContent) {
    if (!selected || !content.trim() || saving) return;
    setEditContent(content);
    setSaving(true);
    const submittedID = selected.id;
    const result = await updateNote(selected, { content });
    if (result && selectedIDRef.current === submittedID) setEditing(false);
    setSaving(false);
  }
  function closeDetail() {
    if (saving) return false;
    if (
      editing &&
      selected &&
      editContent !== selected.content &&
      !window.confirm("Quitter sans enregistrer vos modifications ?")
    )
      return false;
    selectedIDRef.current = null;
    historyRequestID.current++;
    setSelected(null);
    setEditing(false);
    setHistory(null);
    return true;
  }
  function navigate(nextView: View, nextTag = "") {
    setView(nextView);
    setTag(nextTag);
    setQuery("");
    setMobileNav(false);
    selectedIDRef.current = null;
    historyRequestID.current++;
    setSelected(null);
  }
  function chooseTag(nextTag: string) {
    setTag(nextTag);
    setView("active");
    setMobileNav(false);
  }
  function toggleCollapse(id: string) {
    setCollapsed((old) => {
      const next = new Set(old);
      next.has(id) ? next.delete(id) : next.add(id);
      return next;
    });
  }
  useEffect(() => {
    function keydown(event: KeyboardEvent) {
      const target = event.target as HTMLElement;
      const inInput =
        target.isContentEditable ||
        ["INPUT", "TEXTAREA", "SELECT"].includes(target.tagName);
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        searchRef.current?.focus();
        searchRef.current?.select();
        return;
      }
      if (event.key === "Escape") {
        if (selected) {
          closeDetail();
        } else if (query) {
          setQuery("");
          searchRef.current?.blur();
        }
        return;
      }
      if (
        inInput ||
        event.metaKey ||
        event.ctrlKey ||
        event.altKey ||
        createOpen ||
        selected ||
        connectionOpen ||
        mobileNav ||
        shortcutsOpen
      )
        return;
      if (event.key.toLowerCase() === "n") {
        event.preventDefault();
        setCreateOpen(true);
      } else if (event.key === "?") {
        setShortcutsOpen(true);
      } else if (event.key === "ArrowDown" || event.key === "ArrowUp") {
        event.preventDefault();
        setKeyboardIndex((index) => {
          const next = Math.max(
            0,
            Math.min(
              filteredNotes.length - 1,
              index + (event.key === "ArrowDown" ? 1 : -1),
            ),
          );
          const note = filteredNotes[next];
          if (note)
            document
              .querySelector(`[data-note-id="${CSS.escape(note.id)}"]`)
              ?.scrollIntoView({ block: "nearest", behavior: "smooth" });
          return next;
        });
      } else if (event.key === "Enter" && filteredNotes[keyboardIndex]) {
        event.preventDefault();
        focusNote(filteredNotes[keyboardIndex]);
      }
    }
    window.addEventListener("keydown", keydown);
    return () => window.removeEventListener("keydown", keydown);
  }, [
    filteredNotes,
    keyboardIndex,
    selected,
    editing,
    editContent,
    query,
    createOpen,
    connectionOpen,
    mobileNav,
    shortcutsOpen,
  ]);
  useEffect(() => {
    if (!selected) return;
    let live = true;
    const initial = Object.fromEntries(
      notes.map((note) => [note.id, note.title]),
    );
    setRelationTitles(initial);
    const missing = (selected.relations || [])
      .map((r) => r.targetId)
      .filter((id, index, ids) => !initial[id] && ids.indexOf(id) === index)
      .slice(0, 20);
    Promise.allSettled(
      missing.map(
        async (id) =>
          [
            id,
            (await api<Note>(`/notes/${encodeURIComponent(id)}`)).title,
          ] as const,
      ),
    ).then((results) => {
      if (!live) return;
      setRelationTitles((old) => ({
        ...old,
        ...Object.fromEntries(
          results
            .filter(
              (r): r is PromiseFulfilledResult<readonly [string, string]> =>
                r.status === "fulfilled",
            )
            .map((r) => r.value),
        ),
      }));
    });
    return () => {
      live = false;
    };
  }, [selected?.id]);
  useEffect(() => {
    if (!session?.authenticated || deepLinkOpened.current) return;
    const id =
      new URLSearchParams(location.search).get("note") ||
      (location.pathname.startsWith("/memos/")
        ? decodeURIComponent(location.pathname.slice(7))
        : null);
    if (!id) return;
    deepLinkOpened.current = true;
    api<Note>(`/notes/${encodeURIComponent(id)}`)
      .then((note) => focusNote(note))
      .catch((e) => notify(e.message));
  }, [session?.authenticated, notify]);
  async function openLinkedNote(id: string) {
    try {
      focusNote(await api<Note>(`/notes/${encodeURIComponent(id)}`));
    } catch (e) {
      notify((e as Error).message);
    }
  }
  async function copyMCP() {
    try {
      await navigator.clipboard.writeText(
        session?.mcp?.url || `${location.origin}/mcp`,
      );
      notify("Adresse MCP copiée.");
    } catch {
      notify(
        "La copie automatique est indisponible. Sélectionnez l’adresse affichée.",
      );
    }
  }
  const composerProps = {
    draft,
    onDraft: setDraft,
    source,
    onSource: () => setSource((s) => !s),
    onSave: saveDraft,
    saving,
    tags,
  };
  const sidebar = (
    <>
      <div className="sidebar-brand">
        <Brand />
        <span className="personal-badge">PERSONNEL</span>
      </div>
      <button
        className="sidebar-search"
        onClick={() => {
          setMobileNav(false);
          setTimeout(() => searchRef.current?.focus(), 40);
        }}
      >
        <Search size={16} />
        <span>Retrouver une idée</span>
        <kbd>⌘ K</kbd>
      </button>
      <div className="nav-section">
        <div className="nav-section-label">VOTRE MÉMOIRE</div>
        {(
          [
            { key: "active", icon: Inbox },
            { key: "pinned", icon: Pin },
            { key: "archived", icon: Archive },
          ] as const
        ).map(({ key, icon: Icon }) => (
          <button
            key={key}
            className={cn("nav-item", view === key && !tag && "selected")}
            onClick={() => navigate(key)}
          >
            <Icon size={17} />
            <span>{viewLabels[key]}</span>
            {key === "active" ? (
              <span className="nav-count">{stats.active}</span>
            ) : key === "archived" && stats.archived > 0 ? (
              <span className="nav-count">{stats.archived}</span>
            ) : null}
          </button>
        ))}
      </div>
      <div className="sidebar-tags">
        <div className="nav-section-label">
          <span>VOS REPÈRES</span>
          <span className="tag-total">{tags.length}</span>
        </div>
        {tags.length ? (
          tags.map((t) => (
            <button
              className={cn("tag-nav", tag === t.name && "selected")}
              key={t.name}
              title={t.name}
              onClick={() => navigate("active", t.name)}
            >
              <Hash size={14} />
              <span>{t.name}</span>
              <small>{t.count}</small>
            </button>
          ))
        ) : (
          <p className="sidebar-empty">
            Ajoutez un #tag à une note pour créer un repère.
          </p>
        )}
      </div>
      <div className="sidebar-bottom">
        <button
          className="ai-connect"
          onClick={() => {
            setConnectionOpen(true);
            setMobileNav(false);
          }}
        >
          <span className="ai-connect-icon">
            <Waypoints size={18} />
          </span>
          <span>
            <strong>Prolongez votre mémoire</strong>
            <small>Connecter votre assistant IA</small>
          </span>
          <ArrowUpRight size={14} />
        </button>
        <div className="sidebar-footer">
          <button
            className="profile-button"
            onClick={() => setShortcutsOpen(true)}
          >
            <span className="profile-avatar">
              {(session?.user?.name || "Y").charAt(0).toUpperCase()}
            </span>
            <span>
              <strong>{session?.user?.name || "Votre espace"}</strong>
              <small>Espace personnel</small>
            </span>
          </button>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label="Réglages"
                className="sidebar-settings"
              >
                <Settings2 size={16} />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent side="top" align="end">
              <DropdownMenuItem onClick={() => setDark((v) => !v)}>
                {dark ? <Sun /> : <Moon />}
                {dark ? "Thème clair" : "Thème sombre"}
              </DropdownMenuItem>
              <DropdownMenuItem onClick={() => setShortcutsOpen(true)}>
                <Command />
                Raccourcis clavier
              </DropdownMenuItem>
              <DropdownMenuItem asChild>
                <a href="/api/export" download>
                  <Download />
                  Exporter toutes les notes
                </a>
              </DropdownMenuItem>
              <DropdownMenuItem onClick={() => navigate("deleted")}>
                <Trash2 />
                Corbeille
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem
                onClick={async () => {
                  try {
                    await api("/logout", { method: "POST" });
                    setSession({ authenticated: false, needsSetup: false });
                    setNotes([]);
                  } catch (e) {
                    notify((e as Error).message);
                  }
                }}
              >
                <LogOut />
                Se déconnecter
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </div>
    </>
  );
  if (bootError)
    return (
      <div className="boot-screen">
        <Brand />
        <h1>Retrouvons votre espace.</h1>
        <p>Le serveur ne répond pas pour le moment.</p>
        <p className="muted">{bootError}</p>
        <Button onClick={loadSession}>Réessayer</Button>
      </div>
    );
  if (!session)
    return (
      <div className="boot-screen">
        <Brand />
        <LoaderCircle className="animate-spin" aria-label="Chargement" />
      </div>
    );
  if (!session.authenticated)
    return <Auth session={session} onAuthenticated={loadSession} />;
  return (
    <TooltipProvider>
      <div className="workspace">
        <aside className="sidebar">{sidebar}</aside>
        <div className="workspace-main">
          <header className="topbar">
            <button
              className="mobile-menu tool-button"
              aria-label="Ouvrir la navigation"
              onClick={() => setMobileNav(true)}
            >
              <Menu size={21} />
            </button>
            <div className="breadcrumb">
              <span>Votre espace</span>
              <ChevronRight size={12} />
              <strong>{tag ? `# ${tag}` : viewLabels[view]}</strong>
            </div>
            <div className="topbar-right">
              <span className="private-indicator">
                <span className="status-dot" />À vous, simplement
              </span>
              <button
                className="tool-button theme-toggle"
                title={dark ? "Thème clair" : "Thème sombre"}
                aria-label={
                  dark ? "Activer le thème clair" : "Activer le thème sombre"
                }
                onClick={() => setDark((d) => !d)}
              >
                {dark ? <Sun size={17} /> : <Moon size={17} />}
              </button>
              <button
                className="help-button"
                aria-label="Raccourcis clavier"
                onClick={() => setShortcutsOpen(true)}
              >
                ?
              </button>
            </div>
          </header>
          <main className="main-content">
            <div className="workspace-heading">
              <div>
                <div className="eyebrow heading-eyebrow">
                  <span className="tiny-asterisk">✳</span> UN ESPACE POUR TOUT
                  CE QUI COMPTE
                </div>
                <h1>
                  {tag ? (
                    <>
                      <span className="heading-hash">#</span>
                      {tag}
                    </>
                  ) : debouncedQuery ? (
                    "Retrouver le fil."
                  ) : view === "active" ? (
                    <>
                      De la place pour <span>vos idées.</span>
                    </>
                  ) : view === "pinned" ? (
                    "Toujours à portée."
                  ) : view === "archived" ? (
                    "Les idées restent."
                  ) : (
                    "Une seconde chance."
                  )}
                </h1>
                <p>
                  {tag
                    ? "Toutes les idées reliées à ce repère."
                    : view === "active"
                      ? "Une pensée, une découverte, un petit déclic. Gardez ce qui vous traverse."
                      : view === "pinned"
                        ? "Vos notes essentielles, réunies ici."
                        : view === "archived"
                          ? "Le fil de vos idées, conservé pour plus tard."
                          : "Les notes supprimées restent récupérables ici."}
                </p>
              </div>
              <div className="heading-illustration" aria-hidden="true">
                <span className="illustration-line l1" />
                <span className="illustration-line l2" />
                <span className="illustration-line l3" />
                <span className="illustration-node n1" />
                <span className="illustration-node n2" />
                <span className="illustration-node n3" />
                <span className="illustration-plus">+</span>
              </div>
            </div>
            <div className="content-grid">
              <section className="notes-column" aria-label="Notes">
                {view === "active" && !tag && !debouncedQuery && (
                  <div className="desktop-composer">
                    <Composer {...composerProps} />
                  </div>
                )}
                <div className="search-bar">
                  <Search size={18} />
                  <input
                    ref={searchRef}
                    aria-label="Rechercher dans les notes"
                    value={query}
                    onChange={(e) => setQuery(e.target.value)}
                    placeholder="Un mot, un sujet, un souvenir…"
                    autoComplete="off"
                    spellCheck={false}
                  />
                  {query ? (
                    <button
                      className="tool-button"
                      aria-label="Effacer la recherche"
                      onClick={() => setQuery("")}
                    >
                      <X size={16} />
                    </button>
                  ) : (
                    <kbd>⌘ K</kbd>
                  )}
                </div>
                <div className="notes-toolbar">
                  <div className="notes-toolbar-label">
                    <h2>
                      {debouncedQuery
                        ? "Résultats"
                        : tag
                          ? "Notes associées"
                          : viewLabels[view]}
                    </h2>
                    <span className="result-count">{page?.total ?? 0}</span>
                    {loading && (
                      <LoaderCircle size={13} className="animate-spin" />
                    )}
                  </div>
                  <div className="notes-toolbar-actions">
                    <DropdownMenu>
                      <DropdownMenuTrigger asChild>
                        <button
                          className="sort-button"
                          aria-label={`Trier les notes : ${sort === "recent" ? "plus récentes" : "plus anciennes"}`}
                        >
                          <ArrowDownWideNarrow size={14} />
                          <span>
                            {sort === "recent"
                              ? "Plus récentes"
                              : "Plus anciennes"}
                          </span>
                          <ChevronDown size={12} />
                        </button>
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align="end">
                        <DropdownMenuItem onClick={() => setSort("recent")}>
                          Plus récentes
                          {sort === "recent" && <Check className="ml-auto" />}
                        </DropdownMenuItem>
                        <DropdownMenuItem onClick={() => setSort("oldest")}>
                          Plus anciennes
                          {sort === "oldest" && <Check className="ml-auto" />}
                        </DropdownMenuItem>
                      </DropdownMenuContent>
                    </DropdownMenu>
                    <span className="toolbar-separator" />
                    <div
                      className="view-toggle"
                      aria-label="Affichage des notes"
                    >
                      <button
                        aria-label="Vue fil"
                        aria-pressed={!outline}
                        className={!outline ? "active" : ""}
                        onClick={() => setOutline(false)}
                      >
                        <LayoutList size={16} />
                      </button>
                      <button
                        aria-label="Vue dépliable"
                        aria-pressed={outline}
                        className={outline ? "active" : ""}
                        onClick={() => setOutline(true)}
                      >
                        <Network size={16} />
                      </button>
                    </div>
                  </div>
                </div>
                {(tag || debouncedQuery) && (
                  <div className="active-filters">
                    {tag && (
                      <button onClick={() => setTag("")}>
                        <Hash size={12} />
                        {tag}
                        <X size={12} />
                      </button>
                    )}
                    {debouncedQuery && (
                      <span>
                        {page?.mode === "fulltext-broad"
                          ? "Correspondances partielles"
                          : page?.mode?.includes("expanded")
                            ? "Recherche élargie"
                            : "Recherche par pertinence"}
                        {page &&
                          ` · ${page.tookMs.toLocaleString("fr-FR", { maximumFractionDigits: 1 })} ms`}
                      </span>
                    )}
                    {outline && (
                      <button
                        className="collapse-all"
                        onClick={() =>
                          setCollapsed((old) =>
                            old.size
                              ? new Set()
                              : new Set(filteredNotes.map((n) => n.id)),
                          )
                        }
                      >
                        {collapsed.size ? "Tout déplier" : "Tout replier"}
                      </button>
                    )}
                  </div>
                )}
                {loadError ? (
                  <div className="empty-state">
                    <h3>La recherche a été interrompue.</h3>
                    <p>{loadError}</p>
                    <Button variant="outline" onClick={refreshNotes}>
                      Réessayer
                    </Button>
                  </div>
                ) : loading && notes.length === 0 ? (
                  <div
                    className="loading-notes"
                    aria-label="Chargement des notes"
                  >
                    {[1, 2, 3].map((n) => (
                      <div className="note-skeleton" key={n}>
                        <span />
                        <i />
                        <i />
                      </div>
                    ))}
                  </div>
                ) : filteredNotes.length === 0 && !loading ? (
                  <div className="empty-state">
                    <div className="empty-icon">
                      {debouncedQuery ? (
                        <Search size={25} />
                      ) : view === "pinned" ? (
                        <Pin size={25} />
                      ) : (
                        <PenLine size={25} />
                      )}
                    </div>
                    <h3>
                      {debouncedQuery
                        ? "Le fil est encore à trouver."
                        : view === "pinned"
                          ? "Gardez l’essentiel à portée."
                          : view === "archived"
                            ? "Rien à laisser de côté."
                            : view === "deleted"
                              ? "La corbeille est vide."
                              : "Tout commence par une idée."}
                    </h3>
                    <p>
                      {debouncedQuery
                        ? "Essayez un autre mot ou retirez un filtre."
                        : view === "pinned"
                          ? "Épinglez une note depuis son menu pour la retrouver ici."
                          : view === "active"
                            ? "Une phrase suffit. Votre première note vous attend."
                            : "Vos notes apparaîtront ici."}
                    </p>
                    {view === "active" && !debouncedQuery && (
                      <Button onClick={() => setCreateOpen(true)}>
                        <Plus />
                        Écrire une note
                      </Button>
                    )}
                    {debouncedQuery && (
                      <Button
                        variant="outline"
                        onClick={() => {
                          setQuery("");
                          setTag("");
                        }}
                      >
                        Effacer les filtres
                      </Button>
                    )}
                    {view === "pinned" && page?.nextCursor && (
                      <Button variant="outline" onClick={loadMore}>
                        Charger d’autres notes
                      </Button>
                    )}
                  </div>
                ) : (
                  <div
                    className={cn(
                      "notes-list",
                      outline && "outline-list",
                      loading && "refreshing",
                    )}
                  >
                    {filteredNotes.map((note, index) => (
                      <NoteCard
                        key={note.id}
                        note={note}
                        outline={outline}
                        collapsed={collapsed.has(note.id)}
                        onCollapse={() => toggleCollapse(note.id)}
                        onFocus={() => focusNote(note)}
                        onEdit={() => focusNote(note, true)}
                        onUpdate={(patch) => updateNote(note, patch)}
                        onHistory={() => showHistory(note)}
                        onTag={chooseTag}
                        onLinkedNote={openLinkedNote}
                        selected={keyboardIndex === index}
                      />
                    ))}
                  </div>
                )}
                {page?.nextCursor && filteredNotes.length > 0 && (
                  <button
                    className="load-more"
                    disabled={loadingMore}
                    onClick={loadMore}
                  >
                    {loadingMore ? (
                      <LoaderCircle size={15} className="animate-spin" />
                    ) : (
                      <ArrowDown size={15} />
                    )}
                    Voir la suite
                  </button>
                )}
                {filteredNotes.length > 0 && !page?.nextCursor && (
                  <div className="end-of-notes">
                    <span />
                    <span>Vous avez retrouvé le fil</span>
                    <span />
                  </div>
                )}
              </section>
              <aside className="context-rail">
                <div className="rail-intro">
                  <span className="rail-sparkle">✳</span>
                  <h3>Tout se relie.</h3>
                  <p>
                    Une petite note aujourd’hui.
                    <br />
                    Une idée plus claire demain.
                  </p>
                </div>
                <div className="rail-section">
                  <div className="rail-title">
                    VOTRE COLLECTION
                    <span className="status-dot" />
                  </div>
                  <div className="collection-stats">
                    <div>
                      <strong>{stats.active}</strong>
                      <span>notes vivantes</span>
                    </div>
                    <div>
                      <strong>{stats.tags}</strong>
                      <span>repères</span>
                    </div>
                  </div>
                  <div className="collection-foot">
                    <Archive size={13} />
                    {stats.archived}{" "}
                    {stats.archived === 1 ? "note archivée" : "notes archivées"}
                  </div>
                </div>
                <div className="rail-section rail-tip">
                  <div className="rail-title">
                    <Command size={13} /> LE PETIT RACCOURCI
                  </div>
                  <p>Une idée vient d’arriver ?</p>
                  <button onClick={() => setCreateOpen(true)}>
                    <kbd>N</kbd>
                    <span>et laissez-la ici.</span>
                    <ArrowUpRight size={13} />
                  </button>
                </div>
                <button
                  className="rail-ai"
                  onClick={() => setConnectionOpen(true)}
                >
                  <div className="rail-ai-icon">
                    <Waypoints size={23} />
                  </div>
                  <h3>
                    Vos notes.
                    <br />
                    Votre assistant.
                  </h3>
                  <p>Donnez du contexte à vos conversations avec l’IA.</p>
                  <span>
                    Configurer le MCP
                    <ArrowUpRight size={14} />
                  </span>
                </button>
                <a
                  className="rail-source"
                  href="https://github.com/yimensar/vrac"
                  target="_blank"
                  rel="noreferrer"
                >
                  Libre, et fait pour durer.
                  <ArrowUpRight size={12} />
                </a>
              </aside>
            </div>
          </main>
          <button
            className="mobile-capture"
            onClick={() => setCreateOpen(true)}
          >
            <Plus size={22} />
            <span>Une nouvelle idée</span>
          </button>
        </div>
      </div>
      <Sheet open={mobileNav} onOpenChange={setMobileNav}>
        <SheetContent side="left" className="mobile-sidebar">
          <SheetHeader className="sr-only">
            <SheetTitle>Navigation</SheetTitle>
            <SheetDescription>Vos notes et vos repères</SheetDescription>
          </SheetHeader>
          {sidebar}
        </SheetContent>
      </Sheet>
      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent
          className="capture-dialog"
          onOpenAutoFocus={(event) => {
            event.preventDefault();
            requestAnimationFrame(() =>
              document
                .querySelector<HTMLElement>(
                  '.capture-dialog [contenteditable="true"]',
                )
                ?.focus(),
            );
          }}
        >
          <DialogHeader>
            <DialogTitle>Gardez le fil.</DialogTitle>
            <DialogDescription>
              Une idée mérite une place, même en quelques mots.
            </DialogDescription>
          </DialogHeader>
          <Composer {...composerProps} compact />
        </DialogContent>
      </Dialog>
      <Sheet
        open={!!selected}
        onOpenChange={(open) => {
          if (!open) closeDetail();
        }}
      >
        <SheetContent side="right" className="focus-sheet">
          <SheetHeader>
            <div className="focus-eyebrow">
              <span className="status-dot" />{" "}
              {editing
                ? "EN COURS D’ÉCRITURE"
                : history
                  ? "HISTORIQUE"
                  : "UN PEU DE FOCUS"}
            </div>
            <SheetTitle>{selected?.title || "Votre note"}</SheetTitle>
            <SheetDescription>
              {selected &&
                `${formatDate(selected.createdAt)} · ${formatTime(selected.createdAt)}`}
            </SheetDescription>
          </SheetHeader>
          {selected && (
            <>
              <div className="focus-toolbar">
                <button
                  className="focus-back"
                  disabled={saving}
                  onClick={closeDetail}
                >
                  <ArrowLeft size={15} />
                  Revenir au fil
                </button>
                <div>
                  {editing ? (
                    <>
                      <Button
                        variant="ghost"
                        size="sm"
                        aria-label={
                          editSource ? "Mode visuel" : "Mode Markdown"
                        }
                        disabled={saving}
                        onClick={() => setEditSource((s) => !s)}
                      >
                        <Code2 />
                      </Button>
                      <Button
                        size="sm"
                        onClick={() => saveEdit()}
                        disabled={saving || !editContent.trim()}
                      >
                        {saving ? (
                          <LoaderCircle className="animate-spin" />
                        ) : (
                          <Check />
                        )}
                        Enregistrer
                      </Button>
                    </>
                  ) : (
                    <>
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => {
                          setHistory(null);
                          setEditing(true);
                        }}
                      >
                        <PenLine />
                        Modifier
                      </Button>
                      <NoteMenu
                        note={selected}
                        onEdit={() => {
                          setHistory(null);
                          setEditing(true);
                        }}
                        onUpdate={(p) => updateNote(selected, p)}
                        onHistory={() => showHistory(selected)}
                        onFocus={() => setHistory(null)}
                      />
                    </>
                  )}
                </div>
              </div>
              <div className="focus-body">
                {historyLoading ? (
                  <LoaderCircle className="animate-spin" />
                ) : history ? (
                  <div className="history-list">
                    <p className="history-intro">
                      Chaque version garde sa place. Le texte actuel reste
                      inchangé.
                    </p>
                    {history.length ? (
                      history.map((revision, index) => (
                        <div
                          className="history-version"
                          key={`${revision.version}-${index}`}
                        >
                          <div>
                            <History size={14} />
                            <strong>Version {revision.version}</strong>
                            <span>
                              {formatDate(revision.updatedAt)} à{" "}
                              {formatTime(revision.updatedAt)}
                            </span>
                          </div>
                          <Markdown value={revision.content} />
                        </div>
                      ))
                    ) : (
                      <p className="muted">
                        Aucune version précédente pour cette note.
                      </p>
                    )}
                  </div>
                ) : (
                  <>
                    <Markdown
                      key={selected.id}
                      value={editing ? editContent : selected.content}
                      onChange={setEditContent}
                      knownTags={tags.map((t) => t.name)}
                      editable={editing && !saving}
                      source={editSource}
                      onSubmit={saveEdit}
                      className="focus-markdown"
                      onNote={openLinkedNote}
                      onTag={(tag) => {
                        if (closeDetail()) chooseTag(tag);
                      }}
                    />
                    {!editing && selected.attachments?.length > 0 && (
                      <div className="focus-attachments">
                        <h3>
                          <Paperclip size={14} />
                          Pièces jointes
                        </h3>
                        {selected.attachments.map((a) => (
                          <a
                            key={a.id}
                            href={`/api/attachments/${encodeURIComponent(a.id)}`}
                            target="_blank"
                            rel="noreferrer"
                          >
                            <span className="attachment-icon">
                              <FileText size={18} />
                            </span>
                            <span>
                              <strong>{a.filename}</strong>
                              <small>
                                {a.size > 0
                                  ? `${Math.max(1, Math.round(a.size / 1024))} Ko`
                                  : "Fichier"}{" "}
                                · {a.type}
                              </small>
                            </span>
                            <Download size={15} />
                          </a>
                        ))}
                      </div>
                    )}
                    {!editing && selected.relations?.length > 0 && (
                      <div className="focus-relations">
                        <h3>
                          <Link2 size={14} />
                          Notes reliées
                        </h3>
                        {selected.relations.map((r, i) => (
                          <button
                            key={`${r.targetId}-${i}`}
                            onClick={async () => {
                              try {
                                focusNote(
                                  await api<Note>(
                                    `/notes/${encodeURIComponent(r.targetId)}`,
                                  ),
                                );
                              } catch (e) {
                                notify((e as Error).message);
                              }
                            }}
                          >
                            <FileText size={14} />
                            <span>
                              {relationTitles[r.targetId] ||
                                (r.type === "COMMENT"
                                  ? "Commentaire"
                                  : "Note associée")}
                            </span>
                            <ArrowUpRight size={13} />
                          </button>
                        ))}
                      </div>
                    )}
                  </>
                )}
              </div>
              <div className="focus-foot">
                <span>
                  <span className="status-dot" />
                  {editing
                    ? "Vos modifications attendent votre enregistrement."
                    : "Le contexte reste, l’esprit s’allège."}
                </span>
                <span>v{selected.version}</span>
              </div>
            </>
          )}
        </SheetContent>
      </Sheet>
      <Dialog open={connectionOpen} onOpenChange={setConnectionOpen}>
        <DialogContent className="connection-dialog">
          <DialogHeader>
            <span className="connection-icon">
              <Waypoints size={24} />
            </span>
            <DialogTitle>Votre mémoire, avec votre IA.</DialogTitle>
            <DialogDescription>
              Recherchez, relisez et capturez vos notes depuis un assistant
              compatible MCP.
            </DialogDescription>
          </DialogHeader>
          <div className="connection-status">
            <span className="status-dot" />
            <span>
              {session.mcp?.oauth
                ? "OAuth disponible sur ce serveur"
                : "Configuration du serveur"}
            </span>
            <span>Streamable HTTP</span>
          </div>
          <label className="connection-label" htmlFor="mcp-address">
            Adresse du serveur MCP
          </label>
          <div className="connection-url">
            <Input
              id="mcp-address"
              readOnly
              value={session.mcp?.url || `${location.origin}/mcp`}
            />
            <Button
              variant="ghost"
              size="icon"
              aria-label="Copier l’adresse MCP"
              onClick={copyMCP}
            >
              <Copy size={16} />
            </Button>
          </div>
          <ol className="connection-steps">
            <li>
              <span>1</span>
              <p>Ajoutez ce serveur dans les connexions de votre assistant.</p>
            </li>
            <li>
              <span>2</span>
              <p>Choisissez OAuth, puis connectez-vous à votre espace Vrac.</p>
            </li>
            <li>
              <span>3</span>
              <p>
                Autorisez la lecture, et l’écriture si vous souhaitez capturer
                des notes.
              </p>
            </li>
          </ol>
          <p className="connection-note">
            Pour un assistant distant, le serveur doit être accessible à une
            adresse HTTPS. Cette fenêtre indique la configuration disponible ;
            elle ne confirme pas une connexion à un assistant.
          </p>
        </DialogContent>
      </Dialog>
      <Dialog open={shortcutsOpen} onOpenChange={setShortcutsOpen}>
        <DialogContent className="shortcuts-dialog">
          <DialogHeader>
            <DialogTitle>Gardez les mains sur le clavier.</DialogTitle>
            <DialogDescription>
              Quelques raccourcis pour suivre vos idées.
            </DialogDescription>
          </DialogHeader>
          <div className="shortcut-row">
            <span>Rechercher une note</span>
            <kbd>⌘ / Ctrl K</kbd>
          </div>
          <div className="shortcut-row">
            <span>Capturer une idée</span>
            <kbd>N</kbd>
          </div>
          <div className="shortcut-row">
            <span>Parcourir les notes</span>
            <kbd>↑ ↓</kbd>
          </div>
          <div className="shortcut-row">
            <span>Ouvrir la note sélectionnée</span>
            <kbd>Entrée</kbd>
          </div>
          <div className="shortcut-row">
            <span>Enregistrer dans l’éditeur</span>
            <kbd>⌘ / Ctrl ↵</kbd>
          </div>
          <div className="shortcut-row">
            <span>Revenir au fil</span>
            <kbd>Échap</kbd>
          </div>
          <div className="shortcut-row">
            <span>Afficher ces raccourcis</span>
            <kbd>?</kbd>
          </div>
        </DialogContent>
      </Dialog>
      {toast && (
        <div className="toast" role="status">
          <span className="toast-check">
            <Check size={14} />
          </span>
          {toast}
          <button
            aria-label="Fermer la notification"
            onClick={() => setToast("")}
          >
            <X size={14} />
          </button>
        </div>
      )}
    </TooltipProvider>
  );
}
