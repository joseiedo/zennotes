/**
 * Optimistic-concurrency helper for "replace the whole document" saves: every
 * request carries the version of the last state the server confirmed, and
 * each response's version becomes the one the next request sends. The server
 * rejects a request built on an older version (e.g. the public site rules
 * changed in another tab or by a rename), instead of letting it overwrite the
 * newer state.
 */
export function createVersionedSend<I extends { version: string }, R extends { version: string }>(
  send: (input: I) => Promise<R>
): { send: (input: Omit<I, 'version'>) => Promise<R>; setVersion: (version: string) => void } {
  let version = ''
  return {
    setVersion: (next) => {
      version = next
    },
    send: async (input) => {
      const result = await send({ ...input, version } as I)
      version = result.version
      return result
    }
  }
}
