export type Reference = {
  target_id: string;
  label: string;
  source_label?: string;
};
export type Node = {
  id: string;
  parent_id?: string | null;
  text: string;
  tags: string[];
  references: Reference[];
  revision: number;
  created_at: string;
  updated_at: string;
  has_children: boolean;
  system?: string;
  journal_date?: string;
  deleted?: boolean;
};
export type Page = { nodes: Node[]; next_cursor?: string };
export type TagPage = { tags: string[]; next_cursor?: string };
export type NodeView = { node: Node; path: Node[]; children: Page };
export type SearchItem = { node: Node; path: Node[]; matched_via?: string };
export type SearchPage = { items: SearchItem[]; next_cursor?: string };
export type Revision = {
  revision: number;
  text: string;
  tags: string[];
  created_at?: string;
  at?: string;
  summary?: string;
  node?: Node;
};
export type CreateNode = {
  text: string;
  tags?: string[];
  children?: CreateNode[];
};
export class ApiError extends Error {
  status: number;
  code: string;
  constructor(status: number, code: string, message: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}
export async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`/api${path}`, {
    ...init,
    credentials: "same-origin",
    headers: {
      ...(init?.body ? { "Content-Type": "application/json" } : {}),
      ...init?.headers,
    },
  });
  if (!response.ok) {
    let message = `Request failed (${response.status})`;
    let code = "request_failed";
    try {
      const data = await response.json();
      message = data.error?.message || message;
      code = data.error?.code || code;
    } catch {
      /* Preserve an HTTP error from a non-JSON proxy response. */
    }
    throw new ApiError(response.status, code, message);
  }
  if (response.status === 204) return undefined as T;
  return response.json() as Promise<T>;
}
export function json(method: string, body: unknown): RequestInit {
  return { method, body: JSON.stringify(body) };
}
export function requestID(): string {
  return crypto.randomUUID();
}
export function errorMessage(error: unknown): string {
  return error instanceof Error
    ? error.message
    : "Something went wrong. Please try again.";
}
