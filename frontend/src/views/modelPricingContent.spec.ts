import { describe, expect, it } from 'vitest'
import { addReferencePriceLabels, pricingMarkdown, renderModelPricingMarkdown } from './modelPricingContent'

describe('模型价格静态内容', () => {
  it('保留附件中的模型和价格文本', () => {
    expect(pricingMarkdown).toContain('gpt-6.1-sol')
    expect(pricingMarkdown).toContain('claude-fable-5-1')
    expect(pricingMarkdown).toContain('¥0.20 / 张')
  })

  it('给每个人民币价格添加参考价标识', () => {
    const markdown = '| 模型 | 输入 | 输出 |\n| --- | ---: | ---: |\n| `demo` | **¥1.00** / $2.00 | **¥3.00** / $6.00 |'
    const labelled = addReferencePriceLabels(markdown)

    expect(labelled.match(/pricing-reference/g)).toHaveLength(2)
    expect(renderModelPricingMarkdown(markdown)).toContain('参考价')
  })
})
