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
import { api } from '@/lib/api'

import type {
  Announcement,
  AnnouncementPage,
  AnnouncementReadReceipt,
  AnnouncementStat,
  AnnouncementStatsPage,
  AnnouncementUnreadCount,
  AnnouncementUnreadUsersPage,
} from './types'

type ApiResponse<T> = {
  success: boolean
  message?: string
  data: T
}

function unwrapResponse<T>(response: ApiResponse<T>): T {
  if (!response.success) {
    throw new Error(response.message || '请求失败')
  }
  return response.data
}

export async function getAnnouncements(
  page: number,
  pageSize: number
): Promise<AnnouncementPage> {
  const response = await api.get<ApiResponse<AnnouncementPage>>(
    '/api/announcements',
    { params: { p: page, page_size: pageSize } }
  )
  return unwrapResponse(response.data)
}

export async function getPublicAnnouncements(
  page: number,
  pageSize: number
): Promise<AnnouncementPage> {
  const response = await api.get<ApiResponse<AnnouncementPage>>(
    '/api/announcements/public',
    { params: { p: page, page_size: pageSize } }
  )
  return unwrapResponse(response.data)
}

export async function getMandatoryAnnouncements(): Promise<Announcement[]> {
  const response = await api.get<ApiResponse<Announcement[]>>(
    '/api/announcements/mandatory'
  )
  return unwrapResponse(response.data)
}

export async function getAnnouncementUnreadCount(): Promise<AnnouncementUnreadCount> {
  const response = await api.get<ApiResponse<AnnouncementUnreadCount>>(
    '/api/announcements/unread-count'
  )
  return unwrapResponse(response.data)
}

export async function markAnnouncementRead(
  announcementKey: string
): Promise<AnnouncementReadReceipt> {
  const response = await api.post<ApiResponse<AnnouncementReadReceipt>>(
    `/api/announcements/${encodeURIComponent(announcementKey)}/read`
  )
  return unwrapResponse(response.data)
}

export async function getAnnouncementStats(
  page = 1,
  pageSize = 100
): Promise<AnnouncementStatsPage> {
  const response = await api.get<ApiResponse<AnnouncementStatsPage>>(
    '/api/announcements/admin/stats',
    { params: { p: page, page_size: pageSize } }
  )
  return unwrapResponse(response.data)
}

export async function collectAnnouncementStats(
  fetchPage: (page: number, pageSize: number) => Promise<AnnouncementStatsPage>
): Promise<AnnouncementStat[]> {
  const pageSize = 100
  const firstPage = await fetchPage(1, pageSize)
  const items = [...firstPage.items]
  const totalPages = Math.ceil(firstPage.total / pageSize)

  for (let page = 2; page <= totalPages; page += 1) {
    const nextPage = await fetchPage(page, pageSize)
    items.push(...nextPage.items)
  }

  return items
}

export function getAllAnnouncementStats(): Promise<AnnouncementStat[]> {
  return collectAnnouncementStats(getAnnouncementStats)
}

export async function getAnnouncementUnreadUsers(
  announcementKey: string,
  page: number,
  pageSize: number
): Promise<AnnouncementUnreadUsersPage> {
  const response = await api.get<ApiResponse<AnnouncementUnreadUsersPage>>(
    `/api/announcements/admin/${encodeURIComponent(announcementKey)}/unread-users`,
    { params: { p: page, page_size: pageSize } }
  )
  return unwrapResponse(response.data)
}
