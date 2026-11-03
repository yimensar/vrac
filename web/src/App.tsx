import { useCallback, useEffect, useRef, useState } from "react";
import {
  ArrowRight,
  ArrowUpRight,
  BookOpen,
  CalendarDays,
  Check,
  ChevronLeft,
  ChevronRight,
  CircleHelp,
  Download,
  Hash,
  Link2,
  Loader2,
  LogOut,
  Menu,
  Plus,
  Search,
  ShieldCheck,
  Sprout,
  X,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Separator } from "@/components/ui/separator";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from "@/components/ui/sheet";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { TooltipProvider } from "@/components/ui/tooltip";
import { Composer, type ComposerTarget } from "@/components/composer";
import { HistoryDialog } from "@/components/history-dialog";
import {
  OutlineRow,
  type RowAction,
  type RowContext,
} from "@/components/outline-row";
import { NodeText } from "@/components/node-text";
import { NodeEditor } from "@/components/node-editor";
import { SubjectPicker } from "@/components/subject-picker";
import {
  api,
  ApiError,
  errorMessage,
  json,
  requestID,
  type Node,
  type NodeView,
  type Page,
  type SearchPage,
  type TagPage,
} from "@/lib/api";
import { dateLabel, shiftDate } from "@/lib/outline";

type Bootstrap = {
  today: Node;
  journal: Node;
  date: string;
  timezone: string;
  tags: string[];
  tags_next_cursor?: string;
};
type Mode = "journal" | "node" | "search" | "backlinks";
type Subject = { id: string; label: string };
const emptySearch: SearchPage = { items: [] };

function Login({ onConnected }: { onConnected: () => Promise<void> }) {
  const [token, setToken] = useState("");
  const [error, setError] = useState(() =>
    new URLSearchParams(window.location.search).has("auth_error")
      ? "Sign-in failed or expired. Please try again."
      : "",
  );
  const [busy, setBusy] = useState(false);
  const [provider, setProvider] = useState<{
    enabled: boolean;
    label: string;
  } | null>(null);
  useEffect(() => {
    const controller = new AbortController();
    fetch("/auth/config", {
      credentials: "same-origin",
      signal: controller.signal,
    })
      .then(async (response) => {
        if (!response.ok)
          throw new Error("Sign-in configuration is unavailable");
        const data = await response.json();
        if (!controller.signal.aborted) {
          setProvider({
            enabled: data.enabled === true,
            label: data.label || "Single sign-on",
          });
        }
      })
      .catch(() => {
        if (!controller.signal.aborted)
          setProvider({ enabled: false, label: "" });
      });
    return () => controller.abort();
  }, []);
  async function submit() {
    setBusy(true);
    setError("");
    try {
      await api("/session", json("POST", { token }));
      setToken("");
      await onConnected();
    } catch (caught) {
      setError(errorMessage(caught));
    } finally {
      setBusy(false);
    }
  }
  return (
    <main className="login-screen">
      <div className="login-card">
        <div className="brand-mark large">v</div>
        <p className="eyebrow">YOUR WORKING KNOWLEDGE</p>
        <h1>
          A place for everything
          <br />
          worth keeping.
        </h1>
        <p className="login-intro">
          Open your journal. Follow a thought.
          <br />
          Pick up where you left off.
        </p>
        {error && (
          <p className="inline-error" role="alert">
            {error}
          </p>
        )}
        {provider?.enabled && (
          <Button asChild className="w-full">
            <a href="/auth/login">
              <ArrowRight />
              Continue with {provider.label}
            </a>
          </Button>
        )}
        {provider === null ? (
          <Loader2
            className="animate-spin"
            aria-label="Loading sign-in options"
          />
        ) : (
          <details open={!provider.enabled}>
            {provider.enabled && (
              <summary className="field-label cursor-pointer py-4">
                Use an access token
              </summary>
            )}
            <form
              onSubmit={(event) => {
                event.preventDefault();
                void submit();
              }}
            >
              <label className="field-label" htmlFor="access-token">
                Access token
              </label>
              <Input
                id="access-token"
                type="password"
                autoComplete="current-password"
                autoFocus={!provider.enabled}
                value={token}
                onChange={(event) => setToken(event.target.value)}
                placeholder="Enter your instance token"
                required
              />
              <Button
                type="submit"
                disabled={busy || !token.trim()}
                className="w-full"
              >
                {busy ? <Loader2 className="animate-spin" /> : <ArrowRight />}
                Open Vrac
              </Button>
            </form>
          </details>
        )}
        <p className="login-foot">
          <ShieldCheck className="size-3.5" />
          {provider?.enabled
            ? "Your working knowledge, privately."
            : "Your token stays out of browser storage."}
        </p>
      </div>
      <span className="login-wordmark">vrac.</span>
    </main>
  );
}

export default function App() {
  const [auth, setAuth] = useState<"loading" | "connected" | "login" | "error">(
    "loading",
  );
  const [bootstrap, setBootstrap] = useState<Bootstrap>();
  const [date, setDate] = useState("");
  const [mode, setMode] = useState<Mode>("journal");
  const [view, setView] = useState<NodeView>();
  const [branches, setBranches] = useState<Record<string, Page>>({});
  const [expanded, setExpanded] = useState(new Set<string>());
  const [loadingBranches, setLoadingBranches] = useState(new Set<string>());
  const [loadingPage, setLoadingPage] = useState(false);
  const [editing, setEditing] = useState<string>();
  const [error, setError] = useState("");
  const [tags, setTags] = useState<string[]>([]);
  const [tagsCursor, setTagsCursor] = useState<string>();
  const [loadingTags, setLoadingTags] = useState(false);
  const [query, setQuery] = useState("");
  const [tag, setTag] = useState("");
  const [subject, setSubject] = useState<Subject>();
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const [results, setResults] = useState<SearchPage>(emptySearch);
  const [searching, setSearching] = useState(false);
  const [backlinkTarget, setBacklinkTarget] = useState<Node>();
  const [composer, setComposer] = useState<ComposerTarget>();
  const [loadingTodayCapture, setLoadingTodayCapture] = useState(false);
  const [history, setHistory] = useState<Node>();
  const [help, setHelp] = useState(false);
  const [subjectPicker, setSubjectPicker] = useState(false);
  const [mobileNav, setMobileNav] = useState(false);
  const pageSequence = useRef(0);
  const searchSequence = useRef(0);
  const tagsSequence = useRef(0);
  const archiveRequests = useRef(new Map<string, string>());
  const todayCapturePending = useRef(false);
  const searchInput = useRef<HTMLInputElement>(null);
  const queryParameters = useCallback(() => {
    const parameters = new URLSearchParams({ limit: "50" });
    if (query.trim()) parameters.set("q", query.trim());
    if (tag) parameters.set("tag", tag);
    if (subject) parameters.set("subject", subject.id);
    if (from) parameters.set("from", from);
    if (to) parameters.set("to", to);
    return parameters;
  }, [query, tag, subject, from, to]);

  function report(caught: unknown) {
    if (caught instanceof ApiError && caught.status === 401) setAuth("login");
    else setError(errorMessage(caught));
  }
  function resetTags(values: string[], cursor?: string) {
    ++tagsSequence.current;
    setTags(values);
    setTagsCursor(cursor);
    setLoadingTags(false);
  }
  async function loadNode(
    id: string,
    nextMode: Mode = "node",
    updateURL = true,
  ) {
    const sequence = ++pageSequence.current;
    ++searchSequence.current;
    setLoadingPage(true);
    setError("");
    setEditing(undefined);
    setMobileNav(false);
    try {
      const next = await api<NodeView>(`/nodes/${encodeURIComponent(id)}`);
      if (sequence !== pageSequence.current) return;
      setView(next);
      setBranches({ [id]: next.children });
      setExpanded(new Set());
      setMode(nextMode);
      setComposer(
        next.children.nodes.length === 0
          ? nextMode === "journal" && next.node.journal_date
            ? { kind: "capture", date: next.node.journal_date }
            : { kind: "node", parent: next.node.id, label: "New bullets" }
          : undefined,
      );
      if (updateURL)
        window.history.pushState(
          null,
          "",
          nextMode === "journal"
            ? `#/journal/${next.node.journal_date || date}`
            : `#/node/${id}`,
        );
    } catch (caught) {
      if (sequence === pageSequence.current) report(caught);
    } finally {
      if (sequence === pageSequence.current) setLoadingPage(false);
    }
  }
  async function openDate(nextDate?: string, updateURL = true) {
    const sequence = ++pageSequence.current;
    ++searchSequence.current;
    setLoadingPage(true);
    setError("");
    setEditing(undefined);
    setMobileNav(false);
    try {
      const data = await api<Bootstrap>(
        nextDate
          ? `/bootstrap?date=${encodeURIComponent(nextDate)}`
          : "/bootstrap",
      );
      const next = await api<NodeView>(`/nodes/${data.today.id}`);
      if (sequence !== pageSequence.current) return;
      if (!nextDate) setBootstrap(data);
      setDate(data.date);
      setView(next);
      setBranches({ [data.today.id]: next.children });
      setExpanded(new Set());
      setMode("journal");
      setComposer(
        next.children.nodes.length === 0
          ? { kind: "capture", date: data.date }
          : undefined,
      );
      resetTags(data.tags || [], data.tags_next_cursor);
      if (updateURL)
        window.history.pushState(null, "", `#/journal/${data.date}`);
    } catch (caught) {
      if (sequence === pageSequence.current) report(caught);
    } finally {
      if (sequence === pageSequence.current) setLoadingPage(false);
    }
  }
  async function captureToday() {
    if (todayCapturePending.current) return;
    todayCapturePending.current = true;
    setLoadingTodayCapture(true);
    setError("");
    try {
      const data = await api<Bootstrap>("/bootstrap");
      setBootstrap(data);
      resetTags(data.tags || [], data.tags_next_cursor);
      setComposer({ kind: "capture", date: data.date });
    } catch (caught) {
      report(caught);
    } finally {
      todayCapturePending.current = false;
      setLoadingTodayCapture(false);
    }
  }
  async function connect() {
    try {
      const data = await api<Bootstrap>("/bootstrap");
      setBootstrap(data);
      setDate(data.date);
      resetTags(data.tags || [], data.tags_next_cursor);
      setAuth("connected");
      const route = window.location.hash.match(
        /^#\/(node|journal|subject)\/([^/?]+)/,
      );
      if (route?.[1] === "node")
        await loadNode(decodeURIComponent(route[2]), "node", false);
      else if (route?.[1] === "journal")
        await openDate(decodeURIComponent(route[2]), false);
      else if (route?.[1] === "subject") {
        const next = await api<NodeView>(
          `/nodes/${encodeURIComponent(route[2])}`,
        );
        setSubject({ id: next.node.id, label: next.node.text });
        setMode("search");
      } else await loadNode(data.today.id, "journal", false);
    } catch (caught) {
      if (caught instanceof ApiError && caught.status === 401) setAuth("login");
      else {
        setError(errorMessage(caught));
        setAuth("error");
      }
    }
  }
  useEffect(() => {
    void connect();
  }, []); // The server chooses today's date in its configured timezone.
  useEffect(() => {
    function shortcuts(event: KeyboardEvent) {
      if (auth !== "connected") return;
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        openSearch();
        setTimeout(() => searchInput.current?.focus(), 0);
      }
      if (
        (event.ctrlKey || event.metaKey) &&
        event.shiftKey &&
        event.key === "Enter" &&
        bootstrap
      ) {
        event.preventDefault();
        void captureToday();
      }
    }
    window.addEventListener("keydown", shortcuts);
    return () => window.removeEventListener("keydown", shortcuts);
  }, [auth, bootstrap]);
  useEffect(() => {
    const route = () => {
      const value = window.location.hash.match(
        /^#\/(node|journal|subject)\/([^/?]+)/,
      );
      if (value?.[1] === "journal")
        void openDate(decodeURIComponent(value[2]), false);
      else if (value?.[1] === "node")
        void loadNode(decodeURIComponent(value[2]), "node", false);
      else if (value?.[1] === "subject")
        void api<NodeView>(`/nodes/${value[2]}`)
          .then((next) => openSubject(next.node.id, next.node.text))
          .catch(report);
    };
    window.addEventListener("popstate", route);
    return () => window.removeEventListener("popstate", route);
  }, []);
  useEffect(() => {
    if (auth !== "connected" || mode !== "search") return;
    const sequence = ++searchSequence.current;
    setSearching(true);
    const timer = setTimeout(() => {
      api<SearchPage>(`/search?${queryParameters()}`)
        .then((data) => {
          if (sequence === searchSequence.current) setResults(data);
        })
        .catch((caught) => {
          if (sequence === searchSequence.current) report(caught);
        })
        .finally(() => {
          if (sequence === searchSequence.current) setSearching(false);
        });
    }, 200);
    return () => {
      clearTimeout(timer);
      ++searchSequence.current;
    };
  }, [auth, mode, queryParameters]);
  useEffect(() => {
    document.title =
      mode === "search"
        ? `${subject?.label || "Search"} · Vrac`
        : `${view?.node.system ? "Journal" : view?.node.text || "Journal"} · Vrac`;
  }, [mode, subject, view]);

  async function reloadBranch(id: string, append = false) {
    const epoch = pageSequence.current;
    setLoadingBranches((value) => new Set(value).add(id));
    try {
      const cursor = append ? branches[id]?.next_cursor : undefined;
      const page = await api<Page>(
        `/nodes/${id}/children?limit=100${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`,
      );
      if (epoch === pageSequence.current)
        setBranches((value) => ({
          ...value,
          [id]: {
            ...page,
            nodes: append
              ? [...(value[id]?.nodes || []), ...page.nodes]
              : page.nodes,
          },
        }));
    } catch (caught) {
      report(caught);
    } finally {
      setLoadingBranches((value) => {
        const next = new Set(value);
        next.delete(id);
        return next;
      });
    }
  }
  function toggle(node: Node) {
    if (expanded.has(node.id))
      setExpanded((value) => {
        const next = new Set(value);
        next.delete(node.id);
        return next;
      });
    else {
      setExpanded((value) => new Set(value).add(node.id));
      if (!branches[node.id]) void reloadBranch(node.id);
    }
  }
  function replaceNode(node: Node) {
    setBranches((value) =>
      Object.fromEntries(
        Object.entries(value).map(([id, page]) => [
          id,
          {
            ...page,
            nodes: page.nodes.map((item) =>
              item.id === node.id ? node : item,
            ),
          },
        ]),
      ),
    );
    setView((value) =>
      value?.node.id === node.id ? { ...value, node } : value,
    );
  }
  async function saved(node: Node, keepEditing = false) {
    replaceNode(node);
    if (!keepEditing)
      setEditing((current) => (current === node.id ? undefined : current));
    if (view) await reloadBranch(view.node.id);
    void refreshTags();
  }
  async function refreshTags() {
    const sequence = ++tagsSequence.current;
    setLoadingTags(true);
    try {
      const data = await api<TagPage>("/tags");
      if (sequence === tagsSequence.current) {
        setTags(data.tags);
        setTagsCursor(data.next_cursor);
      }
    } catch (caught) {
      if (sequence === tagsSequence.current) report(caught);
    } finally {
      if (sequence === tagsSequence.current) setLoadingTags(false);
    }
  }
  async function loadMoreTags() {
    if (!tagsCursor || loadingTags) return;
    const sequence = tagsSequence.current;
    setLoadingTags(true);
    try {
      const data = await api<TagPage>(
        `/tags?cursor=${encodeURIComponent(tagsCursor)}`,
      );
      if (sequence === tagsSequence.current) {
        setTags((current) => [...new Set([...current, ...data.tags])]);
        setTagsCursor(data.next_cursor);
      }
    } catch (caught) {
      if (sequence === tagsSequence.current) report(caught);
    } finally {
      if (sequence === tagsSequence.current) setLoadingTags(false);
    }
  }
  async function created(_nodes: Node[], parent?: string) {
    if (parent && parent !== view?.node.id) {
      setExpanded((value) => new Set(value).add(parent));
      await reloadBranch(parent);
    }
    if (view) await reloadBranch(view.node.id);
    if (mode === "search") {
      try {
        const data = await api<SearchPage>(`/search?${queryParameters()}`);
        setResults(data);
      } catch (caught) {
        report(caught);
      }
    }
    await refreshTags();
  }
  function openSearch(nextTag = "") {
    ++pageSequence.current;
    ++searchSequence.current;
    setLoadingPage(false);
    setMode("search");
    setSubject(undefined);
    setQuery("");
    setTag(nextTag);
    setFrom("");
    setTo("");
    setResults(emptySearch);
    setEditing(undefined);
    setMobileNav(false);
  }
  function openSubject(id: string, label: string) {
    ++pageSequence.current;
    ++searchSequence.current;
    setLoadingPage(false);
    setMode("search");
    setSubject({ id, label });
    setQuery("");
    setTag("");
    setFrom("");
    setTo("");
    setResults(emptySearch);
    setEditing(undefined);
    setMobileNav(false);
    window.history.pushState(null, "", `#/subject/${id}`);
  }
  async function moreResults() {
    if (!results.next_cursor || searching) return;
    const sequence = searchSequence.current;
    setSearching(true);
    try {
      let path: string;
      if (mode === "backlinks" && backlinkTarget)
        path = `/nodes/${backlinkTarget.id}/backlinks?cursor=${encodeURIComponent(results.next_cursor)}&limit=50`;
      else {
        const parameters = queryParameters();
        parameters.set("cursor", results.next_cursor);
        path = `/search?${parameters}`;
      }
      const data = await api<SearchPage>(path);
      if (sequence === searchSequence.current)
        setResults((value) => ({
          ...data,
          items: [...value.items, ...data.items],
        }));
    } catch (caught) {
      report(caught);
    } finally {
      if (sequence === searchSequence.current) setSearching(false);
    }
  }
  async function action(action: RowAction, node: Node) {
    setError("");
    try {
      if (action === "focus") return await loadNode(node.id);
      if (action === "child") {
        setExpanded((value) => new Set(value).add(node.id));
        if (!branches[node.id]) await reloadBranch(node.id);
        return setComposer({
          kind: "node",
          parent: node.id,
          label: "Add a child bullet",
        });
      }
      if (action === "below" && node.parent_id)
        return setComposer({
          kind: "node",
          parent: node.parent_id,
          after: node.id,
          label: "Add a bullet below",
        });
      if (action === "history") return setHistory(node);
      if (action === "archive") {
        if (
          !window.confirm(
            "Archive this bullet? Its text and revisions will remain recoverable in history.",
          )
        )
          return;
        const key = `${node.id}:${node.revision}`;
        if (!archiveRequests.current.has(key))
          archiveRequests.current.set(key, requestID());
        await api(
          `/nodes/${node.id}`,
          json("DELETE", {
            expected_revision: node.revision,
            request_id: archiveRequests.current.get(key),
          }),
        );
        setEditing(undefined);
        if (node.parent_id) await reloadBranch(node.parent_id);
        if (view && view.node.id !== node.parent_id)
          await reloadBranch(view.node.id);
        setHistory({ ...node, deleted: true });
        void refreshTags();
        return;
      }
      if (action === "backlinks") {
        ++pageSequence.current;
        const sequence = ++searchSequence.current;
        setLoadingPage(false);
        setMode("backlinks");
        setBacklinkTarget(node);
        setResults(emptySearch);
        setSearching(true);
        try {
          const data = await api<SearchPage>(
            `/nodes/${node.id}/backlinks?limit=50`,
          );
          if (sequence === searchSequence.current) setResults(data);
        } finally {
          if (sequence === searchSequence.current) setSearching(false);
        }
        return;
      }
      const siblings = branches[node.parent_id || ""]?.nodes || [];
      const index = siblings.findIndex((item) => item.id === node.id);
      let parent = node.parent_id;
      let placement: string | { before: string } | { after: string } = "last";
      if (action === "indent") {
        if (index < 1) return;
        parent = siblings[index - 1].id;
      }
      if (action === "outdent") {
        if (!parent) return;
        const data = await api<NodeView>(`/nodes/${parent}`);
        if (data.node.system)
          throw new Error("Journal headings stay in the journal.");
        parent = data.node.parent_id;
        placement = { after: data.node.id };
      }
      if (action === "up") {
        if (index < 1) return;
        placement = { before: siblings[index - 1].id };
      }
      if (action === "down") {
        if (index < 0 || index >= siblings.length - 1) return;
        placement = { after: siblings[index + 1].id };
      }
      const data = await api<{ node: Node }>(
        `/nodes/${node.id}/move`,
        json("POST", {
          parent_id: parent || null,
          placement,
          expected_revision: node.revision,
          request_id: requestID(),
        }),
      );
      replaceNode(data.node);
      if (node.parent_id) await reloadBranch(node.parent_id);
      if (parent && parent !== node.parent_id) {
        setExpanded((value) => new Set(value).add(parent!));
        await reloadBranch(parent);
      }
      if (view && view.node.id !== node.parent_id && view.node.id !== parent)
        await reloadBranch(view.node.id);
    } catch (caught) {
      report(caught);
    }
  }
  async function logout() {
    try {
      await api("/session", { method: "DELETE" });
      setView(undefined);
      setBranches({});
      setAuth("login");
    } catch (caught) {
      report(caught);
    }
  }
  function composerSlot(parent: string, after?: string) {
    if (!composer) return null;
    const targetParent =
      composer.kind === "node"
        ? composer.parent
        : mode === "journal" && composer.date === date
          ? view?.node.id
          : undefined;
    const targetAfter = composer.kind === "node" ? composer.after : undefined;
    if (targetParent !== parent || targetAfter !== after) return null;
    return (
      <li className="outline-composer-item">
        <Composer
          key={JSON.stringify(composer)}
          target={composer}
          onClose={() =>
            setComposer((current) =>
              current === composer ? undefined : current,
            )
          }
          onCreated={created}
        />
      </li>
    );
  }
  const rowContext: RowContext = {
    expanded,
    branches,
    editing,
    loading: loadingBranches,
    onToggle: toggle,
    onAction: (kind, node) => {
      void action(kind, node);
    },
    onEdit: setEditing,
    onSaved: saved,
    onTag: openSearch,
    onSubject: openSubject,
    onMore: (id) => {
      void reloadBranch(id, true);
    },
    renderComposer: composerSlot,
    onInlineCommand: (node, kind) => {
      if (kind === "outdent" && node.parent_id === view?.node.id) {
        setEditing(node.id);
        return;
      }
      void action(kind, node).then(() => {
        if (kind !== "below") setEditing(node.id);
      });
    },
  };
  const rootPage = view ? branches[view.node.id] : undefined;
  const sidebar = (
    <div className="sidebar-inner">
      <a
        className="brand"
        href="#/journal"
        onClick={(event) => {
          event.preventDefault();
          void openDate();
        }}
      >
        <span className="brand-mark">v</span>
        <span>
          vrac<span className="brand-period">.</span>
        </span>
      </a>
      <div className="workspace-label">
        <span className="workspace-dot" />
        Personal knowledge<span className="version-label">V0</span>
      </div>
      <nav aria-label="Main navigation">
        <button
          className={`nav-item${mode === "journal" ? " active" : ""}`}
          onClick={() => {
            void openDate();
          }}
        >
          <CalendarDays />
          Journal<span className="nav-detail">Today</span>
        </button>
        <button
          className={`nav-item${mode === "search" && !tag && !subject ? " active" : ""}`}
          onClick={() => openSearch()}
        >
          <Search />
          Search<span className="keycap">⌘ K</span>
        </button>
      </nav>
      <div className="sidebar-section-title">
        TAGS
        <Button
          variant="ghost"
          size="icon-xs"
          aria-label="Refresh tags"
          title="Refresh tags"
          disabled={loadingTags}
          onClick={() => void refreshTags()}
        >
          <Hash />
        </Button>
      </div>
      <nav className="tag-navigation" aria-label="Tags">
        {tags.length === 0 ? (
          <p className="sidebar-empty">Tags appear as you write.</p>
        ) : (
          tags.map((name) => (
            <button
              key={name}
              className={`nav-item tag-nav${mode === "search" && tag === name ? " active" : ""}`}
              onClick={() => openSearch(name)}
            >
              <Hash />
              <span>{name}</span>
            </button>
          ))
        )}
        {tagsCursor && (
          <Button
            variant="ghost"
            size="sm"
            className="more-tags"
            disabled={loadingTags}
            onClick={() => void loadMoreTags()}
          >
            {loadingTags && <Loader2 className="animate-spin" />}Load more tags
          </Button>
        )}
      </nav>
      <div className="sidebar-bottom">
        <div className="knowledge-note">
          <Sprout />
          <p>
            Capture a thought.
            <br />
            <span>Let the connections grow.</span>
          </p>
        </div>
        <Separator />
        <div className="sidebar-footer">
          <button onClick={() => setHelp(true)}>
            <CircleHelp />
            Guide
          </button>
          <button
            onClick={() => void logout()}
            title="Sign out"
            aria-label="Sign out"
          >
            <LogOut />
          </button>
        </div>
      </div>
    </div>
  );

  if (auth === "login") return <Login onConnected={connect} />;
  if (auth === "loading" || auth === "error")
    return (
      <main className="loading-screen">
        <div className="brand-mark large">v</div>
        {auth === "loading" ? (
          <p className="loading-line">
            <Loader2 className="size-4 animate-spin" />
            Opening your journal…
          </p>
        ) : (
          <>
            <p role="alert" className="inline-error">
              {error}
            </p>
            <Button
              onClick={() => {
                setAuth("loading");
                void connect();
              }}
            >
              Try again
            </Button>
          </>
        )}
      </main>
    );
  return (
    <TooltipProvider>
      <div className="app-shell">
        <aside className="desktop-sidebar">{sidebar}</aside>
        <div className="main-shell">
          <header className="topbar">
            <div className="topbar-left">
              <Sheet open={mobileNav} onOpenChange={setMobileNav}>
                <SheetTrigger asChild>
                  <Button
                    variant="ghost"
                    size="icon"
                    className="mobile-menu"
                    aria-label="Open navigation"
                  >
                    <Menu />
                  </Button>
                </SheetTrigger>
                <SheetContent side="left" className="mobile-sidebar">
                  <SheetHeader className="sr-only">
                    <SheetTitle>Navigation</SheetTitle>
                    <SheetDescription>
                      Journal, search, and tags
                    </SheetDescription>
                  </SheetHeader>
                  {sidebar}
                </SheetContent>
              </Sheet>
              <span className="topbar-section">
                <BookOpen className="size-4" />
                {mode === "journal"
                  ? "Journal"
                  : mode === "search"
                    ? subject
                      ? "Subject"
                      : "Search"
                    : mode === "backlinks"
                      ? "References"
                      : "Note"}
              </span>
              {mode === "journal" && bootstrap && date === bootstrap.date && (
                <span className="today-chip">Today</span>
              )}
            </div>
            <div className="topbar-actions">
              <a
                href="/api/export"
                className="export-link"
                title="Export your knowledge"
                aria-label="Export your knowledge"
              >
                <Download className="size-4" />
              </a>
              <Button
                size="sm"
                disabled={loadingTodayCapture}
                onClick={() => {
                  if (mode === "journal")
                    setComposer({ kind: "capture", date });
                  else void captureToday();
                }}
              >
                <Plus />
                Capture<span className="capture-shortcut">⌘ ⇧ ↵</span>
              </Button>
            </div>
          </header>
          <main className="main-content" id="main-content">
            {composer?.kind === "capture" &&
              (mode !== "journal" || composer.date !== date) && (
                <section
                  className="inline-today-capture"
                  aria-label="Capture today"
                >
                  <Composer
                    key={JSON.stringify(composer)}
                    target={composer}
                    onClose={() =>
                      setComposer((current) =>
                        current === composer ? undefined : current,
                      )
                    }
                    onCreated={created}
                  />
                </section>
              )}
            {error && (
              <div className="error-banner" role="alert">
                <span>{error}</span>
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => setError("")}
                  aria-label="Dismiss error"
                >
                  <X />
                </Button>
              </div>
            )}
            {(mode === "journal" || mode === "node") && (
              <>
                <div className="page-heading">
                  {mode === "journal" ? (
                    <>
                      <div className="journal-date-line">
                        <span className="eyebrow">THE DAILY THREAD</span>
                        <div className="date-controls">
                          <Button
                            variant="ghost"
                            size="icon-sm"
                            aria-label="Previous day"
                            onClick={() => void openDate(shiftDate(date, -1))}
                          >
                            <ChevronLeft />
                          </Button>
                          <label className="sr-only" htmlFor="journal-date">
                            Journal date
                          </label>
                          <input
                            id="journal-date"
                            type="date"
                            value={date}
                            onChange={(event) => {
                              if (event.target.value)
                                void openDate(event.target.value);
                            }}
                          />
                          <Button
                            variant="ghost"
                            size="icon-sm"
                            aria-label="Next day"
                            onClick={() => void openDate(shiftDate(date, 1))}
                          >
                            <ChevronRight />
                          </Button>
                        </div>
                      </div>
                      <h1>
                        {date &&
                          dateLabel(date, {
                            weekday: "long",
                            month: "long",
                            day: "numeric",
                          })}
                        <span className="heading-period">.</span>
                      </h1>
                      <p className="page-subtitle">
                        A place for today’s thoughts, decisions, and loose ends.
                      </p>
                    </>
                  ) : (
                    <>
                      <div className="breadcrumbs">
                        <button
                          onClick={() => {
                            void openDate();
                          }}
                        >
                          Journal
                        </button>
                        {view?.path
                          .filter(
                            (node) =>
                              node.id !== view.node.id &&
                              node.system !== "journal",
                          )
                          .map((node) => (
                            <span key={node.id}>
                              <ChevronRight />
                              <button
                                onClick={() =>
                                  void loadNode(
                                    node.id,
                                    node.journal_date ? "journal" : "node",
                                  )
                                }
                              >
                                {node.text}
                              </button>
                            </span>
                          ))}
                      </div>
                      <div className="note-heading">
                        <h1>{view?.node.text}</h1>
                        <Button
                          variant="ghost"
                          size="icon"
                          title="Edit this note"
                          aria-label="Edit this note"
                          onClick={() => setEditing(view?.node.id)}
                        >
                          <ArrowUpRight />
                        </Button>
                      </div>
                      <div className="heading-meta">
                        {view?.node.tags.map((name) => (
                          <button
                            key={name}
                            className="tag-link"
                            onClick={() => openSearch(name)}
                          >
                            #{name}
                          </button>
                        ))}
                        <button
                          onClick={() => {
                            if (view) void action("backlinks", view.node);
                          }}
                        >
                          <Link2 className="size-3.5" />
                          References
                        </button>
                        <button
                          onClick={() => {
                            if (view) setHistory(view.node);
                          }}
                        >
                          History
                        </button>
                      </div>
                    </>
                  )}
                </div>
                {loadingPage ? (
                  <p className="loading-line page-loading">
                    <Loader2 className="size-4 animate-spin" />
                    Loading notes…
                  </p>
                ) : (
                  <section
                    className="outline-section"
                    aria-label="Journal outline"
                  >
                    {mode === "node" && view && editing === view.node.id && (
                      <div className="focused-editor">
                        <NodeEditor
                          node={view.node}
                          onSaved={saved}
                          onClose={() => setEditing(undefined)}
                        />
                      </div>
                    )}
                    {rootPage?.nodes.length || composer ? (
                      <ul className="outline-list root-outline" role="list">
                        {rootPage?.nodes.map((node, index) => (
                          <OutlineRow
                            key={node.id}
                            node={node}
                            context={rowContext}
                            siblingIndex={index}
                            siblingCount={rootPage.nodes.length}
                          />
                        ))}
                        {view && composerSlot(view.node.id)}
                      </ul>
                    ) : (
                      <div className="empty-journal">
                        <span className="empty-symbol">
                          <Sprout />
                        </span>
                        <h2>Start with one thought.</h2>
                        <p>
                          A meeting, a decision, something to do.
                          <br />
                          It all has a place here.
                        </p>
                        <Button
                          variant="outline"
                          onClick={() =>
                            setComposer(
                              mode === "journal"
                                ? { kind: "capture", date }
                                : {
                                    kind: "node",
                                    parent: view!.node.id,
                                    label: "Add a child bullet",
                                  },
                            )
                          }
                        >
                          <Plus />
                          Write your first bullet
                        </Button>
                      </div>
                    )}
                    {rootPage?.next_cursor && (
                      <Button
                        variant="outline"
                        className="load-more"
                        disabled={view && loadingBranches.has(view.node.id)}
                        onClick={() => {
                          if (view) void reloadBranch(view.node.id, true);
                        }}
                      >
                        Load more bullets
                      </Button>
                    )}
                    {rootPage?.nodes.length && !composer ? (
                      <button
                        className="add-bullet"
                        onClick={() =>
                          setComposer(
                            mode === "journal"
                              ? { kind: "capture", date }
                              : {
                                  kind: "node",
                                  parent: view!.node.id,
                                  label: "Add a child bullet",
                                },
                          )
                        }
                      >
                        <Plus />
                        Add a thought…
                      </button>
                    ) : null}
                    <div className="journal-end">
                      <span />
                      <span>
                        {mode === "journal"
                          ? "ROOM FOR MORE"
                          : "FOLLOW THE THREAD"}
                      </span>
                      <span />
                    </div>
                  </section>
                )}
              </>
            )}
            {(mode === "search" || mode === "backlinks") && (
              <>
                <div className="page-heading search-heading">
                  <span className="eyebrow">
                    {mode === "backlinks"
                      ? "FOLLOW THE CONNECTIONS"
                      : subject
                        ? "CONNECTED KNOWLEDGE"
                        : "FIND YOUR THREAD"}
                  </span>
                  <h1>
                    {mode === "backlinks"
                      ? "References"
                      : subject?.label || (tag ? `#${tag}` : "Search")}
                    <span className="heading-period">.</span>
                  </h1>
                  <p className="page-subtitle">
                    {mode === "backlinks"
                      ? `Notes that reference “${backlinkTarget?.text || ""}”.`
                      : subject
                        ? "Notes connected to this subject, with their original context."
                        : "Explore by words, tags, subjects, or a moment in time."}
                  </p>
                  {subject && (
                    <Button
                      variant="outline"
                      size="sm"
                      className="subject-open"
                      onClick={() => void loadNode(subject.id)}
                    >
                      <ArrowUpRight />
                      Open subject note
                    </Button>
                  )}
                </div>
                {mode === "search" && (
                  <form
                    className="search-form"
                    onSubmit={(event) => event.preventDefault()}
                  >
                    <div className="search-box">
                      <Search className="size-4" />
                      <Input
                        ref={searchInput}
                        aria-label="Search notes"
                        placeholder={
                          subject
                            ? "Search within this subject…"
                            : "Search your knowledge…"
                        }
                        value={query}
                        onChange={(event) => setQuery(event.target.value)}
                      />
                      {query && (
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          aria-label="Clear search"
                          onClick={() => setQuery("")}
                        >
                          <X />
                        </Button>
                      )}
                    </div>
                    <div className="search-filters">
                      <div className="subject-filter">
                        <span>Subject</span>
                        <div>
                          <Button
                            variant="outline"
                            size="sm"
                            onClick={() => setSubjectPicker(true)}
                          >
                            <Link2 />
                            {subject ? subject.label : "Any subject"}
                          </Button>
                          {subject && (
                            <Button
                              variant="ghost"
                              size="icon-xs"
                              aria-label="Clear subject"
                              onClick={() => setSubject(undefined)}
                            >
                              <X />
                            </Button>
                          )}
                        </div>
                      </div>
                      <label>
                        <span>Tag</span>
                        <select
                          aria-label="Filter by tag"
                          value={tag}
                          onChange={(event) => setTag(event.target.value)}
                        >
                          <option value="">All tags</option>
                          {tag && !tags.includes(tag) && (
                            <option value={tag}>{tag}</option>
                          )}
                          {tags.map((value) => (
                            <option key={value} value={value}>
                              {value}
                            </option>
                          ))}
                        </select>
                      </label>
                      <label>
                        <span>From</span>
                        <input
                          type="date"
                          aria-label="From date"
                          value={from}
                          onChange={(event) => setFrom(event.target.value)}
                        />
                      </label>
                      <label>
                        <span>To</span>
                        <input
                          type="date"
                          aria-label="To date"
                          min={from || undefined}
                          value={to}
                          onChange={(event) => setTo(event.target.value)}
                        />
                      </label>
                      {(tag || from || to) && (
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => {
                            setTag("");
                            setFrom("");
                            setTo("");
                          }}
                        >
                          Clear filters
                        </Button>
                      )}
                    </div>
                  </form>
                )}
                <div className="search-results" aria-busy={searching}>
                  {searching && results.items.length === 0 ? (
                    <p className="loading-line">
                      <Loader2 className="size-4 animate-spin" />
                      Searching…
                    </p>
                  ) : results.items.length === 0 ? (
                    <div className="empty-search">
                      <Search />
                      <h2>No matching notes yet.</h2>
                      <p>
                        {mode === "backlinks"
                          ? "A [[reference]] to this note will appear here."
                          : "Try another word or a wider date range."}
                      </p>
                    </div>
                  ) : (
                    <>
                      <p className="results-label">
                        {results.items.length}
                        {results.next_cursor ? "+" : ""}{" "}
                        {results.items.length === 1 ? "note" : "notes"}
                        {searching && (
                          <Loader2 className="size-3 animate-spin" />
                        )}
                      </p>
                      {results.items.map((item) => (
                        <article className="search-result" key={item.node.id}>
                          <div className="result-breadcrumb">
                            {item.path
                              .filter((node) => node.id !== item.node.id)
                              .map((node) => node.text)
                              .join(" / ") || "Subject note"}
                          </div>
                          <div className="result-content">
                            <NodeText
                              node={item.node}
                              onSubject={openSubject}
                            />
                            <Button
                              variant="ghost"
                              size="icon-sm"
                              title="Open note"
                              aria-label={`Open ${item.node.text.slice(0, 60)}`}
                              onClick={() => void loadNode(item.node.id)}
                            >
                              <ArrowUpRight />
                            </Button>
                          </div>
                          <div className="result-meta">
                            {item.node.tags.map((value) => (
                              <button
                                key={value}
                                className="tag-link"
                                onClick={() => setTag(value)}
                              >
                                #{value}
                              </button>
                            ))}
                            {item.matched_via && (
                              <span>{item.matched_via}</span>
                            )}
                          </div>
                        </article>
                      ))}
                      {results.next_cursor && (
                        <Button
                          variant="outline"
                          className="load-more"
                          disabled={searching}
                          onClick={() => void moreResults()}
                        >
                          Load more results
                        </Button>
                      )}
                    </>
                  )}
                </div>
              </>
            )}
          </main>
          <footer className="main-footer">
            <span>
              <span className="connection-dot" />
              Connected to your knowledge
            </span>
            <button onClick={() => setHelp(true)}>
              Write freely. Find it later.
              <ArrowUpRight className="size-3" />
            </button>
          </footer>
        </div>
      </div>
      {subjectPicker && (
        <SubjectPicker
          onClose={() => setSubjectPicker(false)}
          onSelect={(id, label) => setSubject({ id, label })}
        />
      )}
      {history && (
        <HistoryDialog
          node={history}
          onClose={() => setHistory(undefined)}
          onRestored={async (node) => {
            await saved(node);
            if (node.parent_id && node.parent_id !== view?.node.id)
              await reloadBranch(node.parent_id);
          }}
        />
      )}
      <Dialog open={help} onOpenChange={setHelp}>
        <DialogContent className="guide-dialog">
          <DialogHeader>
            <DialogTitle>A little room for your thoughts.</DialogTitle>
            <DialogDescription>
              Vrac keeps your notes in one ordered journal. Tags and references
              give you different ways back to the same information.
            </DialogDescription>
          </DialogHeader>
          <div className="guide-items">
            <p>
              <CalendarDays />
              <span>
                <strong>Start in the journal</strong>Capture a thought or paste
                an indented outline. Each bullet can have children.
              </span>
            </p>
            <p>
              <Hash />
              <span>
                <strong>Give it a tag</strong>Write #meeting, #decision, or
                #task. Click a tag to find notes of that kind.
              </span>
            </p>
            <p>
              <Link2 />
              <span>
                <strong>Connect a subject</strong>Write [[Project Atlas]].
                References are stable links, even when a subject is renamed.
              </span>
            </p>
            <p>
              <Check />
              <span>
                <strong>Keep one current note</strong>Edit a note as it evolves.
                Earlier revisions remain in its history.
              </span>
            </p>
          </div>
          <div className="guide-shortcuts">
            <span>
              <kbd>Enter</kbd>Next bullet
            </span>
            <span>
              <kbd>Tab / ⇧ Tab</kbd>Nest / outdent
            </span>
            <span>
              <kbd>⇧ Enter</kbd>New line in a bullet
            </span>
            <span>
              <kbd>⌘ / Ctrl K</kbd>Search
            </span>
            <span>
              <kbd>⌘ / Ctrl ⇧ Enter</kbd>Capture today
            </span>
            <span>
              <kbd>⌘ / Ctrl Enter</kbd>Save an editor
            </span>
          </div>
        </DialogContent>
      </Dialog>
    </TooltipProvider>
  );
}
