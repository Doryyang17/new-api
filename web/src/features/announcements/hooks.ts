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
import {
  keepPreviousData,
  useMutation,
  useQuery,
  useQueryClient,
} from '@tanstack/react-query'

import {
  getAnnouncements,
  getAnnouncementUnreadCount,
  getMandatoryAnnouncements,
  getPublicAnnouncements,
  markAnnouncementRead,
} from './api'
import { applyAnnouncementReadReceipt } from './read-state'
import type {
  Announcement,
  AnnouncementPage,
  AnnouncementUnreadCount,
} from './types'

export const announcementQueryKeys = {
  listRoot: ['announcements', 'list'] as const,
  list: (page: number, pageSize: number) =>
    ['announcements', 'list', page, pageSize] as const,
  mandatory: ['announcements', 'mandatory'] as const,
  unreadCount: ['announcements', 'unread-count'] as const,
  public: (page: number, pageSize: number) =>
    ['announcements', 'public', page, pageSize] as const,
  stats: ['announcements', 'admin', 'stats'] as const,
}

export function usePublicAnnouncements(
  page: number,
  pageSize: number,
  enabled = true
) {
  return useQuery({
    queryKey: announcementQueryKeys.public(page, pageSize),
    queryFn: () => getPublicAnnouncements(page, pageSize),
    enabled,
    staleTime: 15_000,
  })
}

export function useAnnouncements(
  page: number,
  pageSize: number,
  enabled = true
) {
  return useQuery({
    queryKey: announcementQueryKeys.list(page, pageSize),
    queryFn: () => getAnnouncements(page, pageSize),
    enabled,
    placeholderData: keepPreviousData,
    staleTime: 15_000,
  })
}

export function useMandatoryAnnouncements() {
  return useQuery({
    queryKey: announcementQueryKeys.mandatory,
    queryFn: getMandatoryAnnouncements,
    staleTime: 10_000,
    retry: 1,
  })
}

export function useAnnouncementUnreadCount(enabled = true) {
  return useQuery({
    queryKey: announcementQueryKeys.unreadCount,
    queryFn: getAnnouncementUnreadCount,
    enabled,
    staleTime: 15_000,
  })
}

export function useMarkAnnouncementRead() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: markAnnouncementRead,
    onSuccess: (result, announcementKey) => {
      queryClient.setQueriesData<AnnouncementPage>(
        { queryKey: announcementQueryKeys.listRoot },
        (current) =>
          applyAnnouncementReadReceipt(current, announcementKey, result)
      )
      queryClient.setQueryData<Announcement[]>(
        announcementQueryKeys.mandatory,
        (current) => current?.filter((item) => item.key !== announcementKey)
      )
      queryClient.setQueryData<AnnouncementUnreadCount>(
        announcementQueryKeys.unreadCount,
        (current) => {
          if (!current || !result.newly_read) return current
          return {
            unread_count: Math.max(0, current.unread_count - 1),
          }
        }
      )
      void queryClient.invalidateQueries({
        queryKey: announcementQueryKeys.stats,
      })
      void queryClient.invalidateQueries({
        queryKey: announcementQueryKeys.listRoot,
      })
      void queryClient.invalidateQueries({
        queryKey: announcementQueryKeys.unreadCount,
      })
    },
  })
}
