import { useMemo, useState } from 'react'
import type { PublicNoteMeta } from './api'
import { buildTree, noteUrl, type TreeNode } from './tree'

function FolderIcon({ open }: { open: boolean }): JSX.Element {
  return (
    <svg viewBox="0 0 16 16" width="12" height="12" aria-hidden="true" className="zn-chevron">
      <path
        d={open ? 'M4 6l4 4 4-4' : 'M6 4l4 4-4 4'}
        fill="none"
        stroke="currentColor"
        strokeWidth="1.6"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  )
}

function TreeItems({
  nodes,
  currentPath,
  depth,
  open,
  toggle
}: {
  nodes: TreeNode[]
  currentPath: string | null
  depth: number
  open: (path: string) => boolean
  toggle: (path: string) => void
}): JSX.Element {
  return (
    <ul className="zn-tree" role={depth === 0 ? 'tree' : 'group'}>
      {nodes.map((node) =>
        node.kind === 'folder' ? (
          <li key={`f:${node.path}`} role="treeitem" aria-expanded={open(node.path)}>
            <button
              type="button"
              className="zn-tree-row zn-tree-folder"
              style={{ paddingLeft: `${0.5 + depth * 0.9}rem` }}
              onClick={() => toggle(node.path)}
            >
              <FolderIcon open={open(node.path)} />
              <span className="zn-tree-label">{node.name}</span>
            </button>
            {open(node.path) && (
              <TreeItems
                nodes={node.children}
                currentPath={currentPath}
                depth={depth + 1}
                open={open}
                toggle={toggle}
              />
            )}
          </li>
        ) : (
          <li key={`n:${node.path}`} role="treeitem">
            <a
              href={noteUrl(node.path)}
              className={`zn-tree-row zn-tree-note${node.path === currentPath ? ' is-active' : ''}`}
              aria-current={node.path === currentPath ? 'page' : undefined}
              style={{ paddingLeft: `${0.5 + depth * 0.9 + 0.95}rem` }}
            >
              <span className="zn-tree-label">{node.name}</span>
            </a>
          </li>
        )
      )}
    </ul>
  )
}

/** Ancestor folder paths of a note path (`a/b/c.md` → `a`, `a/b`). */
function ancestors(path: string | null): string[] {
  if (!path) return []
  const segs = path.split('/').slice(0, -1)
  return segs.map((_, i) => segs.slice(0, i + 1).join('/'))
}

export function Sidebar({
  notes,
  currentPath,
  searchSlot
}: {
  notes: PublicNoteMeta[]
  currentPath: string | null
  /** Placeholder for the search box (Task 9). */
  searchSlot?: React.ReactNode
}): JSX.Element {
  const tree = useMemo(() => buildTree(notes), [notes])
  // Folders are collapsed by default; the current note's folders open. A
  // user's explicit toggle wins over that default.
  const [toggled, setToggled] = useState<Record<string, boolean>>({})
  const autoOpen = useMemo(() => new Set(ancestors(currentPath)), [currentPath])
  const open = (path: string): boolean => toggled[path] ?? autoOpen.has(path)
  const toggle = (path: string): void => setToggled((t) => ({ ...t, [path]: !open(path) }))

  return (
    <nav className="zn-sidebar" aria-label="Notes">
      <a href="/" className="zn-site-home">
        Home
      </a>
      <div className="zn-search-slot" data-slot="search">
        {searchSlot}
      </div>
      {tree.length === 0 ? (
        <p className="zn-tree-empty">No public notes.</p>
      ) : (
        <TreeItems nodes={tree} currentPath={currentPath} depth={0} open={open} toggle={toggle} />
      )}
    </nav>
  )
}
