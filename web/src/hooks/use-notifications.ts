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
import { useQuery } from '@tanstack/react-query'
import { useMemo, useState } from 'react'

import {
  useAnnouncementUnreadCount,
  useAnnouncements,
  useMarkAnnouncementRead,
  usePublicAnnouncements,
} from '@/features/announcements/hooks'
import type { Announcement } from '@/features/announcements/types'
import { useStatus } from '@/hooks/use-status'
import { getNotice } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'
import { useNotificationStore } from '@/stores/notification-store'

import {
  shouldMarkNoticeRead,
  type NotificationTab,
} from './notification-read-state'

/**
 * Hook to manage notifications (Notice + Announcements)
 * Uses server-side read receipts for authenticated announcement state.
 */
export function useNotifications() {
  const [popoverOpen, setPopoverOpen] = useState(false)
  const [activeTab, setActiveTab] = useState<NotificationTab>('announcements')

  const {
    data: noticeResponse,
    isLoading: noticeLoading,
    refetch: refetchNoticeQuery,
  } = useQuery({
    queryKey: ['notice'],
    queryFn: getNotice,
    staleTime: 1000 * 60 * 5,
  })
  const { status, loading: statusLoading } = useStatus()
  const isAuthenticated = useAuthStore((state) => Boolean(state.auth.user))
  const announcementsEnabled = status?.announcements_enabled === true
  const hasAnnouncementCenter = isAuthenticated && announcementsEnabled
  const shouldLoadAnnouncements = popoverOpen && activeTab === 'announcements'
  const announcementsQuery = useAnnouncements(
    1,
    5,
    hasAnnouncementCenter && shouldLoadAnnouncements
  )
  const publicAnnouncementsQuery = usePublicAnnouncements(
    1,
    5,
    !isAuthenticated && shouldLoadAnnouncements
  )
  const unreadCountQuery = useAnnouncementUnreadCount(hasAnnouncementCenter)
  const markReadMutation = useMarkAnnouncementRead()
  const lastReadNotice = useNotificationStore((state) => state.lastReadNotice)
  const markNoticeRead = useNotificationStore((state) => state.markNoticeRead)
  let announcements: Announcement[] = []
  let announcementsLoading = false
  if (isAuthenticated && hasAnnouncementCenter) {
    announcements = announcementsQuery.data?.items ?? []
    announcementsLoading = announcementsQuery.isLoading
  } else if (!isAuthenticated) {
    announcements = publicAnnouncementsQuery.data?.items ?? []
    announcementsLoading = publicAnnouncementsQuery.isLoading
  }
  const noticeContent = noticeResponse?.success
    ? (noticeResponse.data || '').trim()
    : ''
  const unreadAnnouncementCount = hasAnnouncementCenter
    ? (unreadCountQuery.data?.unread_count ?? 0)
    : 0
  const unreadNoticeCount = useMemo(
    () => (noticeContent && noticeContent !== lastReadNotice ? 1 : 0),
    [lastReadNotice, noticeContent]
  )

  const handleOpenPopover = (tab?: NotificationTab) => {
    const nextTab = tab || activeTab
    if (shouldMarkNoticeRead(nextTab, noticeContent)) {
      markNoticeRead(noticeContent)
    }
    setActiveTab(nextTab)
    setPopoverOpen(true)
  }

  const handlePopoverOpenChange = (open: boolean) => {
    if (open) {
      handleOpenPopover(activeTab)
      return
    }

    setPopoverOpen(false)
  }

  const handleTabChange = (tab: NotificationTab) => {
    if (shouldMarkNoticeRead(tab, noticeContent)) {
      markNoticeRead(noticeContent)
    }
    setActiveTab(tab)
  }

  const handleAnnouncementRead = async (announcement: Announcement) => {
    if (!hasAnnouncementCenter || announcement.read || !announcement.key) return
    await markReadMutation.mutateAsync(announcement.key)
  }

  return {
    // Data
    notice: noticeContent,
    announcements,
    loading: noticeLoading || statusLoading || announcementsLoading,

    unreadCount: unreadAnnouncementCount + unreadNoticeCount,
    unreadNoticeCount,
    unreadAnnouncementsCount: unreadAnnouncementCount,
    hasAnnouncementCenter,

    // Popover state
    popoverOpen,
    setPopoverOpen: handlePopoverOpenChange,
    activeTab,
    setActiveTab: handleTabChange,

    // Actions
    openPopover: handleOpenPopover,
    closePopover: () => setPopoverOpen(false),
    markAnnouncementRead: handleAnnouncementRead,
    refetchNotice: async () => {
      const refreshes: Promise<unknown>[] = [refetchNoticeQuery()]
      if (hasAnnouncementCenter) refreshes.push(unreadCountQuery.refetch())
      if (hasAnnouncementCenter && shouldLoadAnnouncements) {
        refreshes.push(announcementsQuery.refetch())
      }
      if (!isAuthenticated && shouldLoadAnnouncements) {
        refreshes.push(publicAnnouncementsQuery.refetch())
      }
      await Promise.all(refreshes)
    },
  }
}
