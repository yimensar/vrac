import {
  Archive,
  ArrowDown,
  ArrowRight,
  ArrowUp,
  ChevronDown,
  ChevronRight,
  CornerDownRight,
  CornerUpLeft,
  Focus,
  History,
  Link,
  MoreHorizontal,
  Pencil,
  Plus,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { NodeText } from "@/components/node-text";
import { NodeEditor } from "@/components/node-editor";
import type { Node, Page } from "@/lib/api";
export type RowAction =
  | "focus"
  | "child"
  | "below"
  | "indent"
  | "outdent"
  | "up"
  | "down"
  | "history"
  | "backlinks"
  | "archive";
export type RowContext = {
  expanded: Set<string>;
  branches: Record<string, Page>;
  editing?: string;
  loading: Set<string>;
  onToggle: (node: Node) => void;
  onAction: (action: RowAction, node: Node) => void;
  onEdit: (id?: string) => void;
  onSaved: (node: Node) => Promise<void>;
  onTag: (tag: string) => void;
  onSubject: (id: string, label: string) => void;
  onMore: (id: string) => void;
};
export function OutlineRow({
  node,
  context,
  depth = 0,
  siblingIndex,
  siblingCount,
}: {
  node: Node;
  context: RowContext;
  depth?: number;
  siblingIndex: number;
  siblingCount: number;
}) {
  const {
    expanded,
    branches,
    editing,
    loading,
    onToggle,
    onAction,
    onEdit,
    onSaved,
    onTag,
    onSubject,
    onMore,
  } = context;
  const children = branches[node.id];
  const isExpanded = expanded.has(node.id);
  return (
    <li className="outline-item" data-node-id={node.id}>
      <div className={`outline-row${editing === node.id ? " is-editing" : ""}`}>
        <button
          className={`disclosure${node.has_children || children?.nodes.length ? "" : " is-leaf"}`}
          aria-label={`${isExpanded ? "Collapse" : "Expand"} bullet`}
          aria-expanded={
            node.has_children || children?.nodes.length ? isExpanded : undefined
          }
          onClick={() => onToggle(node)}
          disabled={!node.has_children && !children?.nodes.length}
          tabIndex={node.has_children || children?.nodes.length ? 0 : -1}
        >
          {isExpanded ? <ChevronDown /> : <ChevronRight />}
        </button>
        <button
          className="bullet-dot"
          aria-label={`Focus ${node.text.slice(0, 70)}`}
          onClick={() => onAction("focus", node)}
        >
          <span />
        </button>
        <div className="row-content">
          {editing === node.id ? (
            <NodeEditor
              key={node.id}
              node={node}
              onSaved={onSaved}
              onClose={() => onEdit(undefined)}
            />
          ) : (
            <>
              <div className="row-text" onDoubleClick={() => onEdit(node.id)}>
                <NodeText node={node} onSubject={onSubject} />
                {node.tags?.length > 0 && (
                  <span className="inline-tags">
                    {node.tags.map((tag) => (
                      <button
                        key={tag}
                        onClick={() => onTag(tag)}
                        className="tag-link"
                      >
                        #{tag}
                      </button>
                    ))}
                  </span>
                )}
              </div>
            </>
          )}
        </div>
        {editing !== node.id && (
          <div className="row-actions">
            <Button
              variant="ghost"
              size="icon-xs"
              title="Edit bullet"
              aria-label="Edit bullet"
              onClick={() => onEdit(node.id)}
            >
              <Pencil />
            </Button>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button
                  variant="ghost"
                  size="icon-xs"
                  aria-label="Bullet actions"
                >
                  <MoreHorizontal />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem onSelect={() => onAction("focus", node)}>
                  <Focus />
                  Focus
                </DropdownMenuItem>
                <DropdownMenuItem onSelect={() => onAction("child", node)}>
                  <CornerDownRight />
                  Add child
                </DropdownMenuItem>
                <DropdownMenuItem onSelect={() => onAction("below", node)}>
                  <Plus />
                  Add below
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem
                  disabled={siblingIndex === 0}
                  onSelect={() => onAction("indent", node)}
                >
                  <ArrowRight />
                  Indent
                </DropdownMenuItem>
                <DropdownMenuItem
                  disabled={depth === 0}
                  onSelect={() => onAction("outdent", node)}
                >
                  <CornerUpLeft />
                  Outdent
                </DropdownMenuItem>
                <DropdownMenuItem
                  disabled={siblingIndex === 0}
                  onSelect={() => onAction("up", node)}
                >
                  <ArrowUp />
                  Move up
                </DropdownMenuItem>
                <DropdownMenuItem
                  disabled={siblingIndex === siblingCount - 1}
                  onSelect={() => onAction("down", node)}
                >
                  <ArrowDown />
                  Move down
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem onSelect={() => onAction("history", node)}>
                  <History />
                  History
                </DropdownMenuItem>
                <DropdownMenuItem onSelect={() => onAction("backlinks", node)}>
                  <Link />
                  References to this note
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem
                  disabled={
                    Boolean(node.system) ||
                    node.has_children ||
                    Boolean(children?.nodes.length)
                  }
                  onSelect={() => onAction("archive", node)}
                >
                  <Archive />
                  Archive bullet
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        )}
      </div>
      {isExpanded && (
        <div className="branch">
          {loading.has(node.id) && !children ? (
            <p className="branch-loading">Loading…</p>
          ) : (
            children && (
              <>
                <ul className="outline-list" role="list">
                  {children.nodes.map((child, index) => (
                    <OutlineRow
                      key={child.id}
                      node={child}
                      depth={depth + 1}
                      siblingIndex={index}
                      siblingCount={children.nodes.length}
                      context={context}
                    />
                  ))}
                </ul>
                {children.next_cursor && (
                  <Button
                    className="load-more"
                    size="sm"
                    variant="ghost"
                    disabled={loading.has(node.id)}
                    onClick={() => onMore(node.id)}
                  >
                    Load more bullets
                  </Button>
                )}
                {children.nodes.length === 0 && (
                  <button
                    className="empty-child"
                    onClick={() => onAction("child", node)}
                  >
                    <Plus className="size-3" />
                    Add a child
                  </button>
                )}
              </>
            )
          )}
        </div>
      )}
    </li>
  );
}
