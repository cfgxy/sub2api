import { marked } from 'marked'
import DOMPurify from 'dompurify'

/**
 * 给静态稿中的每个人民币价格加上参考价标识，避免页面被误读为实时计费配置。
 */
export function addReferencePriceLabels(markdown: string): string {
  return markdown.split('\n').map((line) => line.trimStart().startsWith('|')
    ? line.replace(/¥\s*[\d.]+(?:\s*\/\s*张)?/g,
      (price) => `<span class="pricing-reference">参考价</span> ${price}`)
    : line).join('\n')
}

export function renderModelPricingMarkdown(markdown: string): string {
  const html = marked.parse(addReferencePriceLabels(markdown)) as string
  return DOMPurify.sanitize(html)
}
