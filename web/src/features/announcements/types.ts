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
export type AnnouncementLevel = 'normal' | 'important' | 'urgent'

export type Announcement = {
  id?: number | string | null
  key: string
  title: string
  content: string
  summary?: string
  publishDate: string
  type?: string
  level: AnnouncementLevel
  forceRead: boolean
  immediate: boolean
  extra?: string
  category: string
  pinned: boolean
  offlineAt?: string
  published: boolean
  read: boolean
  readAt?: number
}

export type AnnouncementPage = {
  page: number
  page_size: number
  total: number
  unread_count: number
  items: Announcement[]
}

export type AnnouncementReadReceipt = {
  read_at: number
  newly_read: boolean
}

export type AnnouncementUnreadCount = {
  unread_count: number
}

export type AnnouncementStat = Announcement & {
  read_count: number
  unread_count: number
  read_rate: number
}

export type AnnouncementStatsPage = {
  page: number
  page_size: number
  total: number
  items: AnnouncementStat[]
}

export type AnnouncementUnreadUser = {
  id: number
  username: string
  display_name?: string
  email?: string
  created_at: number
  last_login_at: number
}

export type AnnouncementUnreadUsersPage = {
  page: number
  page_size: number
  total: number
  items: AnnouncementUnreadUser[]
}
