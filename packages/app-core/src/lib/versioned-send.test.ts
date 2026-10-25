import { describe, expect, it } from 'vitest'
import { createVersionedSend } from './versioned-send'

describe('createVersionedSend', () => {
  it('sends the version from the latest server response', async () => {
    const sent: Array<{ n: number; version: string }> = []
    let next = 1
    const vs = createVersionedSend(async (input: { n: number; version: string }) => {
      sent.push(input)
      return { version: `v${++next}` }
    })
    vs.setVersion('v1')
    await vs.send({ n: 1 })
    await vs.send({ n: 2 })
    expect(sent).toEqual([
      { n: 1, version: 'v1' },
      { n: 2, version: 'v2' }
    ])
  })

  it('keeps the old version when a send fails, and takes a resynced one', async () => {
    const sent: string[] = []
    let fail = true
    const vs = createVersionedSend(async (input: { version: string }) => {
      sent.push(input.version)
      if (fail) throw new Error('409')
      return { version: 'after' }
    })
    vs.setVersion('v1')
    await expect(vs.send({})).rejects.toThrow('409')
    vs.setVersion('resynced')
    fail = false
    await vs.send({})
    expect(sent).toEqual(['v1', 'resynced'])
  })
})
