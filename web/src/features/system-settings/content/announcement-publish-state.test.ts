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
import { describe, test } from 'node:test'

import {
  normalizeAnnouncementImmediate,
  resolveAnnouncementPublishDate,
} from './announcement-publish-state'

describe('announcement publish state compatibility', () => {
  test('derives legacy scheduling from its publish time', () => {
    const now = new Date('2026-07-25T08:00:00Z').getTime()

    assert.equal(
      normalizeAnnouncementImmediate(undefined, '2026-07-25T09:00:00Z', now),
      false
    )
    assert.equal(
      normalizeAnnouncementImmediate(undefined, '2026-07-25T07:00:00Z', now),
      true
    )
  })

  test('preserves the original timestamp when editing an immediate notice', () => {
    const original = '2026-07-24T08:00:00Z'
    const result = resolveAnnouncementPublishDate({
      immediate: true,
      selectedPublishDate: original,
      editingAnnouncement: { immediate: true, publishDate: original },
      now: '2026-07-25T08:00:00Z',
    })

    assert.equal(result, original)
  })

  test('uses the current time when a scheduled notice switches to immediate', () => {
    const now = '2026-07-25T08:00:00Z'
    const result = resolveAnnouncementPublishDate({
      immediate: true,
      selectedPublishDate: '2026-07-26T08:00:00Z',
      editingAnnouncement: {
        immediate: false,
        publishDate: '2026-07-26T08:00:00Z',
      },
      now,
    })

    assert.equal(result, now)
  })
})
