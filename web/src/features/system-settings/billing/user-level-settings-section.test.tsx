/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import assert from 'node:assert/strict'
import { after, describe, test } from 'node:test'

import { Window } from 'happy-dom'

import type { UserLevelAdminData } from '@/features/user-levels'

const domWindow = new Window()
const domGlobals = [
  'window',
  'document',
  'navigator',
  'HTMLElement',
  'HTMLButtonElement',
  'HTMLInputElement',
  'HTMLFormElement',
  'HTMLLabelElement',
  'SVGElement',
  'Node',
  'Element',
  'Event',
  'CustomEvent',
  'MutationObserver',
  'ResizeObserver',
  'requestAnimationFrame',
  'cancelAnimationFrame',
  'getComputedStyle',
] as const

for (const key of domGlobals) {
  Object.defineProperty(globalThis, key, {
    configurable: true,
    value: domWindow[key],
  })
}

const { act } = await import('react')
const { createRoot } = await import('react-dom/client')
const { QueryClient, QueryClientProvider } =
  await import('@tanstack/react-query')
const { createInstance } = await import('i18next')
const { I18nextProvider, initReactI18next } = await import('react-i18next')
const { USER_LEVEL_ADMIN_QUERY_KEY } =
  await import('@/features/user-levels/hooks')
const { UserLevelSettingsSection } =
  await import('./user-level-settings-section')

const i18n = createInstance()
await i18n.use(initReactI18next).init({
  lng: 'zh-CN',
  resources: { 'zh-CN': { translation: {} } },
})

const reactTestGlobals = globalThis as typeof globalThis & {
  IS_REACT_ACT_ENVIRONMENT?: boolean
}
reactTestGlobals.IS_REACT_ACT_ENVIRONMENT = true

const cachedConfig: UserLevelAdminData = {
  config: {
    schema_version: 1,
    enabled: true,
    levels: [
      {
        id: 'base',
        name: '普通用户',
        description: '默认等级',
        threshold_quota: 0,
        ratio: 1,
        badge_color: 'neutral',
        archived: false,
      },
      {
        id: 'gold',
        name: '黄金会员',
        description: '已保存的高级等级',
        threshold_quota: 5_000_000,
        ratio: 0.8,
        badge_color: 'warning',
        archived: false,
      },
    ],
  },
  member_counts: { base: 10, gold: 2 },
  revision: 'cached-revision',
}

describe('user level settings initialization', () => {
  after(() => {
    domWindow.close()
  })

  test('hydrates the form from cached server data when reopening the menu', async () => {
    const container = document.createElement('div')
    document.body.append(container)
    const root = createRoot(container)
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: 60_000 } },
    })
    queryClient.setQueryData(USER_LEVEL_ADMIN_QUERY_KEY, cachedConfig)

    await act(async () => {
      root.render(
        <QueryClientProvider client={queryClient}>
          <I18nextProvider i18n={i18n}>
            <UserLevelSettingsSection />
          </I18nextProvider>
        </QueryClientProvider>
      )
    })

    const levelNames = [
      ...container.querySelectorAll<HTMLInputElement>('input[name$=".name"]'),
    ].map((input) => input.value)
    assert.deepEqual(levelNames, ['普通用户', '黄金会员'])
    assert.match(container.textContent ?? '', /12 名用户/)

    await act(async () => root.unmount())
    container.remove()
    queryClient.clear()
  })
})
