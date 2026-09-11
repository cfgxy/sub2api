// @vitest-environment node

import { build, type Rollup } from 'vite'
import { describe, expect, it } from 'vitest'
import { fileURLToPath } from 'node:url'
import { resolve } from 'node:path'

describe('production bundle', () => {
  it('does not create mutual static imports between chunks', async () => {
    const frontendRoot = fileURLToPath(new URL('../..', import.meta.url))
    const result = await build({
      root: frontendRoot,
      configFile: resolve(frontendRoot, 'vite.config.ts'),
      build: { write: false },
      logLevel: 'silent',
    })
    const outputs = (Array.isArray(result) ? result : [result]) as Rollup.RollupOutput[]
    const chunks = outputs.flatMap((output) => output.output)
      .filter((item): item is Rollup.OutputChunk => item.type === 'chunk')
    const chunksByFileName = new Map(chunks.map((chunk) => [chunk.fileName, chunk]))
    const mutualImports = chunks.flatMap((chunk) => chunk.imports
      .filter((importedFileName) => chunksByFileName.get(importedFileName)?.imports.includes(chunk.fileName))
      .map((importedFileName) => [chunk.fileName, importedFileName].sort().join(' <-> ')))

    expect([...new Set(mutualImports)]).toEqual([])
  }, 120_000)
})
