import { createRoot } from 'react-dom/client'

// Placeholder entry for the public reader; the real UI lands in a later task.
const root = document.getElementById('zn-root')
if (!root) throw new Error('#zn-root not found')
createRoot(root).render(<div>ZenNotes</div>)
