import { describe, expect, it } from 'vitest'
import { addReferencePriceLabels, renderModelPricingMarkdown } from './modelPricingContent'

describe('模型价格静态内容', () => {
  it('给每个人民币价格添加参考价标识', () => {
    const markdown = '| 模型 | 输入 | 输出 |\n| --- | ---: | ---: |\n| `demo` | **¥1.00** / $2.00 | **¥3.00** / $6.00 |'
    const labelled = addReferencePriceLabels(markdown)

    expect(labelled.match(/pricing-reference/g)).toHaveLength(2)
    expect(renderModelPricingMarkdown(markdown)).toContain('参考价')
  })

  it('不将充值额度说明误标为模型价格', () => {
    const markdown = '> **充值 ¥1 = $1 站内 API 额度**\n| `demo` | **¥1.00** / $2.00 |'
    const labelled = addReferencePriceLabels(markdown)

    expect(labelled).toContain('充值 ¥1 = $1')
    expect(labelled.match(/pricing-reference/g)).toHaveLength(1)
  })
})
