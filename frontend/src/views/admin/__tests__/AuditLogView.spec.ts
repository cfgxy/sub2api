import { describe, expect, it } from 'vitest'
import AuditLogView from '../AuditLogView.vue'
import source from '../AuditLogView.vue?raw'

// 平台审计页承载跨企业审计：设计稿不含「清空全部审计」入口，
// 且该能力不允许以无守护形式回流。以源码静态断言作为机械核验判据，
// 防止上游同步时把清空入口（按钮/确认弹窗/TOTP 流程/直连 API）带回来。
describe('AuditLogView（平台审计页清空入口移除）', () => {
  it('compiles as a valid SFC after the clear-all entry removal', () => {
    expect(AuditLogView).toBeTruthy()
  })

  it('exposes no clear-all entry: no button, confirm dialog, TOTP flow, or API call', () => {
    expect(source).not.toContain('clearAll')
    expect(source).not.toContain('openClearDialog')
    expect(source).not.toContain('onClearConfirmed')
    expect(source).not.toContain('submitClear')
    expect(source).not.toContain('clearConfirm')
    expect(source).not.toContain('clearTotp')
    expect(source).not.toContain('audit-logs/clear')
    expect(source).not.toContain('totpAPI')
  })

  it('keeps the audit list page scaffold intact', () => {
    expect(source).toContain('admin.audit.filters')
    expect(source).toContain('fetchLogs')
  })
})
