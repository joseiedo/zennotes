import { describe, expect, it } from 'vitest'
import { createSaveQueue } from './save-queue'

function deferred<T>() {
  let resolve!: (v: T) => void
  let reject!: (e: unknown) => void
  const promise = new Promise<T>((a, b) => ((resolve = a), (reject = b)))
  return { promise, resolve, reject }
}

describe('createSaveQueue', () => {
  it('sends strictly sequentially, coalescing so the last state is persisted last', async () => {
    const sent: number[] = []
    let inFlight = 0
    let maxInFlight = 0
    const gates: ReturnType<typeof deferred<number>>[] = []
    const saved: number[] = []
    const push = createSaveQueue<number, number>({
      send: (v) => {
        sent.push(v)
        inFlight++
        maxInFlight = Math.max(maxInFlight, inFlight)
        const d = deferred<number>()
        gates.push(d)
        return d.promise.finally(() => inFlight--)
      },
      onSaved: (r) => saved.push(r),
      onError: () => {},
      resync: async () => {}
    })
    const done = push(1)
    push(2)
    push(3)
    expect(sent).toEqual([1])
    gates[0].resolve(1)
    await new Promise((r) => setTimeout(r, 0))
    expect(sent).toEqual([1, 3])
    gates[1].resolve(3)
    await done
    expect(maxInFlight).toBe(1)
    expect(saved).toEqual([3])
  })

  it('surfaces a failure, drops queued state, and resyncs', async () => {
    const errors: unknown[] = []
    let resyncs = 0
    const sent: number[] = []
    const gate = deferred<number>()
    const push = createSaveQueue<number, number>({
      send: (v) => {
        sent.push(v)
        return gate.promise
      },
      onSaved: () => {},
      onError: (e) => errors.push(e),
      resync: async () => {
        resyncs++
      }
    })
    const done = push(1)
    push(2)
    gate.reject(new Error('boom'))
    await done
    expect(sent).toEqual([1])
    expect(errors).toHaveLength(1)
    expect(resyncs).toBe(1)
  })

  it('accepts new saves after finishing', async () => {
    const sent: number[] = []
    const push = createSaveQueue<number, number>({
      send: async (v) => (sent.push(v), v),
      onSaved: () => {},
      onError: () => {},
      resync: async () => {}
    })
    await push(1)
    await push(2)
    expect(sent).toEqual([1, 2])
  })
})
