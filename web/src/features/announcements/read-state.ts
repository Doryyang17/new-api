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
import type { AnnouncementPage, AnnouncementReadReceipt } from './types'

export function applyAnnouncementReadReceipt(
  current: AnnouncementPage | undefined,
  announcementKey: string,
  receipt: AnnouncementReadReceipt
): AnnouncementPage | undefined {
  if (!current) return current

  let itemChanged = false
  const items = current.items.map((item) => {
    if (item.key !== announcementKey || item.read) return item
    itemChanged = true
    return { ...item, read: true, readAt: receipt.read_at }
  })
  const unreadCount = receipt.newly_read
    ? Math.max(0, current.unread_count - 1)
    : current.unread_count
  if (!itemChanged && unreadCount === current.unread_count) return current

  return {
    ...current,
    unread_count: unreadCount,
    items,
  }
}
