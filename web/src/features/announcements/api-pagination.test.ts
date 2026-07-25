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

import { collectAnnouncementStats } from './api'
import type { AnnouncementStat } from './types'

function createStat(index: number): AnnouncementStat {
  return {
    id: index,
    key: `announcement-${index}`,
    title: `公告 ${index}`,
    content: '公告正文',
    publishDate: '2026-07-25T08:00:00+08:00',
    level: 'normal',
    forceRead: false,
    immediate: true,
    category: 'system',
    pinned: false,
    published: true,
    read: false,
    read_count: 0,
    unread_count: 1,
    read_rate: 0,
  }
}

describe('announcement statistics pagination', () => {
  test('loads every page when more than 100 announcements exist', async () => {
    const allStats = Array.from({ length: 205 }, (_, index) =>
      createStat(index + 1)
    )
    const requests: Array<[number, number]> = []

    const result = await collectAnnouncementStats(async (page, pageSize) => {
      requests.push([page, pageSize])
      const start = (page - 1) * pageSize
      return {
        page,
        page_size: pageSize,
        total: allStats.length,
        items: allStats.slice(start, start + pageSize),
      }
    })

    assert.deepEqual(requests, [
      [1, 100],
      [2, 100],
      [3, 100],
    ])
    assert.equal(result.length, 205)
    assert.equal(result[204]?.key, 'announcement-205')
  })
})
