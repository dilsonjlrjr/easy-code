import { describe, expect, it } from 'vitest'
import { NAVIGATION_ITEMS, visibleNavigationItems } from '../navigationItems'

describe('main navigation', () => {
  it('keeps the ESP-00 order of destinations', () => {
    expect(NAVIGATION_ITEMS.map((item) => item.routeName)).toEqual([
      'home',
      'projects',
      'pending-items',
      'agents',
      'settings',
    ])
  })

  it('hides Settings from users who are not administrators', () => {
    const routeNames = visibleNavigationItems(false).map((item) => item.routeName)
    expect(routeNames).not.toContain('settings')
    expect(routeNames).toHaveLength(4)
  })
})
