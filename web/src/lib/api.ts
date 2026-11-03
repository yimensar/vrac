export type Attachment = {
  id: string;
  filename: string;
  type: string;
  size: number;
  url: string;
};
export type Relation = { targetId: string; type: string };
export type Note = {
  id: string;
  content: string;
  title: string;
  tags: string[];
  createdAt: string;
  updatedAt: string;
  state: "active" | "archived" | "deleted";
  pinned: boolean;
  version: number;
  source: string;
  sourceId?: string;
  attachments: Attachment[];
  relations: Relation[];
  snippet?: string;
  matchType?: string;
};
export type Page = {
  notes: Note[];
  total: number;
  nextCursor?: string;
  tookMs: number;
  mode: string;
};
export type Tag = { name: string; count: number };
export type Stats = {
  active: number;
  archived: number;
  tags: number;
  attachments: number;
};
export type Session = {
  authenticated: boolean;
  needsSetup: boolean;
  user?: { name: string };
  mcp?: { url: string; oauth: boolean };
};
export class APIError extends Error {
  status: number;
  constructor(message: string, status: number) {
    super(message);
    this.status = status;
  }
}
export async function api<T>(
  path: string,
  options: RequestInit = {},
): Promise<T> {
  const response = await fetch(`/api${path}`, {
    credentials: "same-origin",
    ...options,
    headers: { "Content-Type": "application/json", ...options.headers },
  });
  if (!response.ok) {
    let message = "Une erreur est survenue.";
    try {
      const data = await response.json();
      message = data.error || message;
    } catch {
      /* Non-JSON server errors remain readable. */
    }
    throw new APIError(message, response.status);
  }
  if (response.status === 204) return undefined as T;
  return response.json();
}
export function formatDate(value: string, short = false) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return date.toLocaleDateString(
    "fr-FR",
    short
      ? { day: "numeric", month: "short" }
      : { day: "numeric", month: "long", year: "numeric" },
  );
}
export function formatTime(value: string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime())
    ? ""
    : date.toLocaleTimeString("fr-FR", { hour: "2-digit", minute: "2-digit" });
}
export function downloadText(
  filename: string,
  content: string,
  type = "text/markdown;charset=utf-8",
) {
  const url = URL.createObjectURL(new Blob([content], { type }));
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  a.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
export function safeFilename(title: string) {
  return (
    (title || "note")
      .replace(/[^\p{L}\p{N}\s_-]/gu, "")
      .trim()
      .slice(0, 80) || "note"
  );
}
export function plainPreview(content: string) {
  return content
    .replace(/```[\s\S]*?```/g, " ")
    .replace(/!?(?:\[([^\]]*)\])\([^)]*\)/g, "$1")
    .replace(/[#*_>`~]/g, "")
    .replace(/\s+/g, " ")
    .trim();
}
