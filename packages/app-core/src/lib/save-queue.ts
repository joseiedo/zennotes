/**
 * Serialises "save the latest desired state" calls: at most one request is in
 * flight, and changes made meanwhile coalesce into one follow-up request, so
 * the last desired state is always persisted last. A failed save surfaces its
 * error, drops any queued newer state (it was built on the failed one) and
 * resyncs the caller to server truth.
 */
export interface SaveQueueHandlers<T, R> {
  send: (desired: T) => Promise<R>
  /** Called with the server response only when nothing newer is queued. */
  onSaved: (result: R) => void
  onError: (err: unknown) => void
  /** Re-fetch server state after a failure. */
  resync: () => Promise<void>
  onBusyChange?: (busy: boolean) => void
}

export function createSaveQueue<T, R>(h: SaveQueueHandlers<T, R>): (desired: T) => Promise<void> {
  let pending: { value: T } | null = null
  let running: Promise<void> | null = null

  async function drain(): Promise<void> {
    h.onBusyChange?.(true)
    try {
      while (pending) {
        const { value } = pending
        pending = null
        try {
          const result = await h.send(value)
          if (!pending) h.onSaved(result)
        } catch (err) {
          pending = null
          h.onError(err)
          try {
            await h.resync()
          } catch {
            /* error already surfaced */
          }
        }
      }
    } finally {
      running = null
      h.onBusyChange?.(false)
    }
  }

  return (desired: T): Promise<void> => {
    pending = { value: desired }
    if (!running) running = drain()
    return running
  }
}
