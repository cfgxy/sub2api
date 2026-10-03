import { describe, expect, it } from 'vitest'
import { routes } from '@/router'

describe('个人模型价格路由', () => {
  it('要求个人登录且不属于企业路由树', () => {
    const pricingRoute = routes.find((route) => route.path === '/model-pricing')

    expect(pricingRoute?.meta).toMatchObject({ requiresAuth: true, requiresAdmin: false })
    expect(routes.some((route) => String(route.path).startsWith('/enterprise/model-pricing'))).toBe(false)
  })
})
