import { describe, expect, it } from 'vitest'
import source from '../AppHeader.vue?raw'

describe('AppHeader 布局', () => {
  it('限制长页面说明，避免撑宽整个页面', () => {
    expect(source).toContain('flex min-w-0 flex-1 items-center')
    expect(source).toContain('class="truncate text-xs')
    expect(source).toContain('flex shrink-0 items-center')
  })
})
