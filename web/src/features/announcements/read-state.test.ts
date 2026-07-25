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

import { applyAnnouncementReadReceipt } from './read-state'
import type { AnnouncementPage } from './types'

function announcementPage(itemKey: string): AnnouncementPage {
  return {
    page: 1,
    page_size: 1,
    total: 2,
    unread_count: 2,
    items: [
      {
        key: itemKey,
        title: '测试公告',
        content: '公告正文',
        publishDate: '2026-07-25T08:00:00+08:00',
        level: 'normal',
        forceRead: false,
        immediate: true,
        category: 'system',
        pinned: false,
        published: true,
        read: false,
      },
    ],
  }
}

describe('announcement read cache synchronization', () => {
  test('decrements the global unread count even when this page omits the item', () => {
    const page = announcementPage('another-announcement')
    const result = applyAnnouncementReadReceipt(page, 'read-announcement', {
      read_at: 123,
      newly_read: true,
    })

    assert.equal(result?.unread_count, 1)
    assert.equal(result?.items[0].read, false)
  })

  test('does not decrement twice for an existing receipt', () => {
    const page = announcementPage('read-announcement')
    const result = applyAnnouncementReadReceipt(page, 'read-announcement', {
      read_at: 123,
      newly_read: false,
    })

    assert.equal(result?.unread_count, 2)
    assert.equal(result?.items[0].read, true)
    assert.equal(result?.items[0].readAt, 123)
  })
})
