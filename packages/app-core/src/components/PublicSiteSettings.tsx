import { useCallback, useEffect, useMemo, useState } from "react";
import type { NoteMeta } from "@shared/ipc";
import {
  effectiveVisibility,
  type PublicMark,
  type PublicSiteConfig,
} from "@shared/public-site-rules";
import { createSaveQueue } from "../lib/save-queue";
import { useStore } from "../store";

type Rules = Record<string, PublicMark>;
type Choice = PublicMark | "inherit";

export interface PublicTreeNode {
  path: string;
  name: string;
  kind: "folder" | "note";
  children: PublicTreeNode[];
}

/** Notes that can ever be a public page: markdown, not trash, not a drawing,
 *  no dot-segments. Mirrors the server's always-private rules. */
export function isPublicPageCandidate(note: NoteMeta): boolean {
  if (note.folder === "trash") return false;
  // Case-sensitive ".md", like the server's IsPublicNote; drawings are
  // matched case-insensitively there too.
  if (!note.path.endsWith(".md")) return false;
  if (note.path.toLowerCase().endsWith(".excalidraw.md")) return false;
  return !note.path.split("/").some((s) => s === "" || s.startsWith("."));
}

export function buildPublicTree(notes: NoteMeta[]): PublicTreeNode[] {
  const root: PublicTreeNode = { path: "", name: "", kind: "folder", children: [] };
  for (const note of notes) {
    if (!isPublicPageCandidate(note)) continue;
    const segs = note.path.split("/");
    let cur = root;
    segs.forEach((seg, i) => {
      const path = segs.slice(0, i + 1).join("/");
      const kind = i === segs.length - 1 ? "note" : "folder";
      let child = cur.children.find((c) => c.path === path);
      if (!child) {
        child = { path, name: seg, kind, children: [] };
        cur.children.push(child);
      }
      cur = child;
    });
  }
  const sort = (nodes: PublicTreeNode[]): void => {
    nodes.sort((a, b) =>
      a.kind !== b.kind
        ? a.kind === "folder"
          ? -1
          : 1
        : a.name.localeCompare(b.name, undefined, { numeric: true }),
    );
    nodes.forEach((n) => sort(n.children));
  };
  sort(root.children);
  return root.children;
}

/** Returns a new rules map with `path` set to `mark`, or cleared (inherit). */
export function withMark(rules: Rules, path: string, mark: PublicMark | null): Rules {
  const next = { ...rules };
  if (mark === null) delete next[path];
  else next[path] = mark;
  return next;
}

export function homeNoteCandidates(notes: NoteMeta[], rules: Rules): NoteMeta[] {
  return notes
    .filter((n) => isPublicPageCandidate(n) && effectiveVisibility(n.path, rules).visible)
    .sort((a, b) => a.path.localeCompare(b.path));
}

const CHOICES: { value: Choice; label: string }[] = [
  { value: "public", label: "Public" },
  { value: "hidden", label: "Hidden" },
  { value: "inherit", label: "Inherit" },
];

function StateBadge({ path, rules }: { path: string; rules: Rules }): JSX.Element {
  const { visible, source } = effectiveVisibility(path, rules);
  const label = visible ? "Public" : "Private";
  const how =
    source === "explicit" ? "set here" : source === "inherited" ? "inherited" : "default";
  return (
    <span
      className={`shrink-0 text-[11px] ${visible ? "text-emerald-500" : "text-ink-500"}`}
      title={`${label} (${how})`}
    >
      {label} · {how}
    </span>
  );
}

function TreeRow({
  node,
  depth,
  rules,
  open,
  toggleOpen,
  onChoose,
}: {
  node: PublicTreeNode;
  depth: number;
  rules: Rules;
  open: Set<string>;
  toggleOpen: (path: string) => void;
  onChoose: (path: string, choice: Choice) => void;
}): JSX.Element {
  const current: Choice = rules[node.path] ?? "inherit";
  const isOpen = open.has(node.path);
  return (
    <>
      <div
        className="flex items-center gap-3 px-4 py-1.5"
        style={{ paddingLeft: 16 + depth * 16 }}
      >
        {node.kind === "folder" ? (
          <button
            type="button"
            onClick={() => toggleOpen(node.path)}
            aria-expanded={isOpen}
            className="w-4 shrink-0 text-xs text-ink-500"
            aria-label={`${isOpen ? "Collapse" : "Expand"} ${node.name}`}
          >
            {isOpen ? "▾" : "▸"}
          </button>
        ) : (
          <span className="w-4 shrink-0" />
        )}
        <span
          className={`min-w-0 flex-1 truncate text-sm ${node.kind === "folder" ? "font-medium text-ink-900" : "text-ink-800"}`}
          title={node.path}
        >
          {node.name}
        </span>
        <StateBadge path={node.path} rules={rules} />
        <div
          role="radiogroup"
          aria-label={`Visibility of ${node.path}`}
          className="inline-flex shrink-0 rounded-lg border border-paper-300 bg-paper-100 p-0.5"
        >
          {CHOICES.map((c) => (
            <button
              key={c.value}
              type="button"
              role="radio"
              aria-checked={current === c.value}
              onClick={() => onChoose(node.path, c.value)}
              className={`rounded-md px-2 py-0.5 text-xs transition-colors ${
                current === c.value
                  ? "bg-accent text-white"
                  : "text-ink-600 hover:bg-paper-200"
              }`}
            >
              {c.label}
            </button>
          ))}
        </div>
      </div>
      {node.kind === "folder" &&
        isOpen &&
        node.children.map((child) => (
          <TreeRow
            key={child.path}
            node={child}
            depth={depth + 1}
            rules={rules}
            open={open}
            toggleOpen={toggleOpen}
            onChoose={onChoose}
          />
        ))}
    </>
  );
}

export function PublicSiteSettings(): JSX.Element {
  const notes = useStore((s) => s.notes);
  const [config, setConfig] = useState<PublicSiteConfig | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [open, setOpen] = useState<Set<string>>(new Set());

  useEffect(() => {
    let cancelled = false;
    void window.zen
      .getPublicSiteConfig?.()
      .then((c) => !cancelled && setConfig(c))
      .catch((e) => !cancelled && setError(e instanceof Error ? e.message : String(e)));
    return () => {
      cancelled = true;
    };
  }, []);

  const save = useMemo(
    () =>
      createSaveQueue<{ homeNote: string; rules: Rules }, PublicSiteConfig>({
        send: (next) => window.zen.setPublicSiteConfig!(next),
        onSaved: (saved) => {
          setError(null);
          setConfig(saved);
        },
        onError: (e) => setError(e instanceof Error ? e.message : String(e)),
        resync: async () => {
          setConfig(await window.zen.getPublicSiteConfig!());
        },
        onBusyChange: setSaving,
      }),
    [],
  );
  const saveNext = useCallback(
    (next: { homeNote: string; rules: Rules }): void => {
      setConfig((c) => (c ? { ...c, ...next } : c));
      void save(next);
    },
    [save],
  );

  const tree = useMemo(() => buildPublicTree(notes), [notes]);
  const rules = config?.rules ?? {};
  const candidates = useMemo(() => homeNoteCandidates(notes, rules), [notes, rules]);

  const toggleOpen = useCallback((path: string) => {
    setOpen((prev) => {
      const next = new Set(prev);
      if (next.has(path)) next.delete(path);
      else next.add(path);
      return next;
    });
  }, []);

  if (!config) {
    return (
      <div className="px-5 py-4 text-sm text-ink-500">
        {error ?? "Loading public site settings…"}
      </div>
    );
  }

  const choose = (path: string, choice: Choice): void =>
    saveNext({
      homeNote: config.homeNote,
      rules: withMark(config.rules, path, choice === "inherit" ? null : choice),
    });

  const homeInList = candidates.some((c) => c.path === config.homeNote);

  return (
    <>
      <div className="flex items-center justify-between gap-4 px-5 py-4">
        <div className="min-w-0">
          <div className="text-sm font-medium text-ink-900">Public site</div>
          <div className="mt-1 text-xs leading-5 text-ink-500">
            Served read-only at the site root. Notes are private unless a folder
            or note is marked Public; the nearest mark wins. Renaming or moving
            in ZenNotes keeps Hidden items hidden, but a renamed Public item
            becomes private. Renames made outside ZenNotes (Obsidian, sync, the
            shell) can&apos;t be tracked: check the warnings below after them.
            {saving ? " Saving…" : ""}
          </div>
        </div>
        <a
          href="/"
          target="_blank"
          rel="noopener noreferrer"
          className="shrink-0 rounded-md border border-paper-300 px-3 py-1.5 text-xs text-ink-900 hover:bg-paper-200"
        >
          Open public site
        </a>
      </div>

      {error && (
        <div role="alert" className="px-5 py-3 text-xs text-rose-400">
          {error}
        </div>
      )}
      {!config.homeNotePublic && (
        <div role="alert" className="px-5 py-3 text-xs text-amber-500">
          {config.homeNote
            ? `The home note "${config.homeNote}" is not public, so the site root shows no home page.`
            : "No home note is set, so the site root shows no home page."}
        </div>
      )}
      {config.missingPaths.map((p) => (
        <div key={p} className="flex items-center gap-3 px-5 py-2 text-xs text-amber-500">
          <span className="min-w-0 flex-1 truncate">
            Rule for <span className="font-mono">{p}</span> matches nothing in the
            vault (renamed or deleted).
          </span>
          <button
            type="button"
            onClick={() =>
              saveNext({ homeNote: config.homeNote, rules: withMark(config.rules, p, null) })
            }
            className="shrink-0 rounded-md px-2 py-1 text-ink-500 hover:bg-rose-500/15 hover:text-rose-400"
          >
            Remove rule
          </button>
        </div>
      ))}

      <div className="flex items-center justify-between gap-4 px-5 py-4">
        <label htmlFor="public-home-note" className="text-sm font-medium text-ink-900">
          Home note
        </label>
        <select
          id="public-home-note"
          value={config.homeNote}
          onChange={(e) => saveNext({ homeNote: e.target.value, rules: config.rules })}
          className="min-w-0 max-w-xs rounded-md border border-paper-300 bg-paper-100 px-2.5 py-1.5 text-xs text-ink-900"
        >
          <option value="">None</option>
          {!homeInList && config.homeNote && (
            <option value={config.homeNote}>{config.homeNote} (not public)</option>
          )}
          {candidates.map((c) => (
            <option key={c.path} value={c.path}>
              {c.path}
            </option>
          ))}
        </select>
      </div>

      <div className="max-h-96 overflow-y-auto py-2">
        {tree.length === 0 ? (
          <div className="px-5 py-3 text-xs text-ink-500">No notes in this vault yet.</div>
        ) : (
          tree.map((node) => (
            <TreeRow
              key={node.path}
              node={node}
              depth={0}
              rules={config.rules}
              open={open}
              toggleOpen={toggleOpen}
              onChoose={choose}
            />
          ))
        )}
      </div>
    </>
  );
}
