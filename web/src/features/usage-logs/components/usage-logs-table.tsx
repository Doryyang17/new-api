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
import { getRouteApi } from '@tanstack/react-router'
import type { ColumnDef } from '@tanstack/react-table'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  DataTablePage,
  DataTableRow,
  useDataTable,
} from '@/components/data-table'
import { useMediaQuery } from '@/hooks'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import { cn } from '@/lib/utils'

import { getAllLogs, getLogStats, getUserLogs, getUserLogStats } from '../api'
import {
  DEFAULT_LOGS_DATA,
  LOG_TYPE_ALL_VALUE,
  LOG_TYPE_ENUM,
} from '../constants'
import type { UsageLog } from '../data/schema'
import { useColumnsByCategory } from '../lib/columns'
import { parseLogOther } from '../lib/format'
import {
  buildCommonLogFilterParams,
  fetchLogsByCategory,
  getDefaultTimeRange,
} from '../lib/utils'
import type { GetLogsResponse, LogCategory, LogStatistics } from '../types'
import { CommonLogsFilterBar } from './common-logs-filter-bar'
import { TaskLogsFilterBar } from './task-logs-filter-bar'
import { UsageLogsMobileList } from './usage-logs-mobile-card'
import { useLogsViewScope } from './usage-logs-provider'

const route = getRouteApi('/_authenticated/usage-logs/$section')

const logTypeRowTint: Record<number, string> = {
  [LOG_TYPE_ENUM.ERROR]: 'bg-rose-50/40 dark:bg-rose-950/20',
  [LOG_TYPE_ENUM.REFUND]: 'bg-blue-50/30 dark:bg-blue-950/15',
}

// Warning tint for logs where a quota conversion saturated (admin-only marker).
// Takes precedence over the per-type tint since it flags a billing anomaly.
const quotaSaturationRowTint = 'bg-amber-50/60 dark:bg-amber-950/25'

function getColumnVisibilityStorageKey(
  logCategory: LogCategory,
  isAdmin: boolean
): string {
  return `usage-logs:${logCategory}:${isAdmin ? 'admin' : 'user'}:column-visibility`
}

function deserializeLogTypeFilter(value: unknown): unknown[] {
  let values: unknown[]
  if (Array.isArray(value)) {
    values = value
  } else if (value) {
    values = [value]
  } else {
    values = []
  }
  return values.filter((item) => String(item) !== LOG_TYPE_ALL_VALUE)
}

interface UsageLogsTableProps {
  logCategory: LogCategory
}

export function UsageLogsTable({ logCategory }: UsageLogsTableProps) {
  const { t } = useTranslation()
  const { isAdminView: isAdmin } = useLogsViewScope()
  const isMobile = useMediaQuery('(max-width: 640px)')
  const searchParams = route.useSearch()
  const [defaultTimeRange] = useState(getDefaultTimeRange)

  const {
    columnFilters,
    onColumnFiltersChange,
    pagination,
    onPaginationChange,
    ensurePageInRange,
  } = useTableUrlState({
    search: route.useSearch(),
    navigate: route.useNavigate(),
    pagination: { defaultPage: 1, defaultPageSize: isMobile ? 20 : 100 },
    globalFilter: { enabled: false },
    columnFilters: [
      {
        columnId: 'created_at',
        searchKey: 'type',
        type: 'array' as const,
        deserialize: deserializeLogTypeFilter,
      },
      { columnId: 'model_name', searchKey: 'model', type: 'string' as const },
      { columnId: 'token_name', searchKey: 'token', type: 'string' as const },
      { columnId: 'group', searchKey: 'group', type: 'string' as const },
      ...(isAdmin
        ? [
            {
              columnId: 'channel',
              searchKey: 'channel',
              type: 'string' as const,
            },
            {
              columnId: 'username',
              searchKey: 'username',
              type: 'string' as const,
            },
          ]
        : []),
    ],
  })

  const isCommon = logCategory === 'common'
  const commonFilterParams = useMemo(
    () =>
      buildCommonLogFilterParams({
        searchParams,
        isAdmin,
        defaultTimeRange,
      }),
    [defaultTimeRange, isAdmin, searchParams]
  )
  const cursorKey = useMemo(
    () => JSON.stringify([isAdmin, pagination.pageSize, commonFilterParams]),
    [commonFilterParams, isAdmin, pagination.pageSize]
  )
  const cursorMapRef = useRef<
    Map<
      number,
      { createdAt: number; id?: number; requestId?: string; rowId?: string }
    >
  >(new Map())
  const cursorKeyRef = useRef(cursorKey)
  if (cursorKeyRef.current !== cursorKey) {
    cursorMapRef.current.clear()
    cursorKeyRef.current = cursorKey
  }
  const page = pagination.pageIndex + 1
  const cursor =
    isCommon && page > 1 ? cursorMapRef.current.get(page) : undefined
  const commonListParams = useMemo(
    () => ({
      ...commonFilterParams,
      p: page,
      page_size: pagination.pageSize,
      with_count: false,
      compact: true,
      cursor_mode: true,
      ...(cursor
        ? {
            cursor_created_at: cursor.createdAt,
            cursor_id: cursor.id,
            cursor_request_id: cursor.requestId,
            cursor_row_id: cursor.rowId,
          }
        : {}),
    }),
    [commonFilterParams, cursor, page, pagination.pageSize]
  )

  const statsQuery = useQuery<LogStatistics>({
    queryKey: ['usage-logs-stats', isAdmin, commonFilterParams],
    enabled: isCommon,
    queryFn: async () => {
      const result = isAdmin
        ? await getLogStats(commonFilterParams)
        : await getUserLogStats(commonFilterParams)
      if (!result.success || !result.data) {
        throw new Error(result.message || '日志统计加载失败')
      }
      if (typeof result.data.total !== 'number') {
        throw new Error('日志统计缺少总数')
      }
      return result.data
    },
    placeholderData: (previousData) => previousData,
    retry: false,
    staleTime: 10_000,
    gcTime: 60_000,
  })
  const statsData = statsQuery.data
  const statsIsError = statsQuery.isError
  const statsIsPlaceholderData = statsQuery.isPlaceholderData

  const countFallbackQuery = useQuery({
    queryKey: ['usage-logs-count-fallback', isAdmin, commonFilterParams],
    enabled: isCommon && statsIsError,
    queryFn: async () => {
      const params = {
        ...commonFilterParams,
        p: 1,
        page_size: 1,
        with_count: true,
        compact: true,
      }
      const result = isAdmin
        ? await getAllLogs(params)
        : await getUserLogs(params)
      if (
        !result.success ||
        !result.data ||
        typeof result.data.total !== 'number'
      ) {
        throw new Error(result.message || '日志总数加载失败')
      }
      return result.data.total
    },
    retry: false,
    staleTime: 10_000,
    gcTime: 60_000,
  })

  const { data, isLoading, isFetching, isPlaceholderData } = useQuery({
    queryKey: isCommon
      ? ['logs', logCategory, isAdmin, commonListParams]
      : [
          'logs',
          logCategory,
          isAdmin,
          page,
          pagination.pageSize,
          columnFilters,
          searchParams,
        ],
    queryFn: async () => {
      let result: GetLogsResponse
      if (isCommon) {
        result = isAdmin
          ? await getAllLogs(commonListParams)
          : await getUserLogs(commonListParams)
      } else {
        result = await fetchLogsByCategory({
          logCategory,
          isAdmin,
          page,
          pageSize: pagination.pageSize,
          searchParams,
          columnFilters,
          defaultTimeRange,
        })
      }

      if (!result?.success) {
        toast.error(result?.message || t('Failed to load logs'))
        return DEFAULT_LOGS_DATA
      }

      return result.data || DEFAULT_LOGS_DATA
    },
    placeholderData: (previousData, previousQuery) => {
      if (previousQuery?.queryKey[1] === logCategory) {
        return previousData
      }
      return undefined
    },
    staleTime: 5_000,
    gcTime: 30_000,
  })

  useEffect(() => {
    if (!isCommon || isPlaceholderData) return
    const nextPage = page + 1
    for (const cachedPage of cursorMapRef.current.keys()) {
      if (cachedPage >= nextPage) {
        cursorMapRef.current.delete(cachedPage)
      }
    }
    if (!data?.items?.length) return
    const last = data.items.at(-1) as Partial<UsageLog> | undefined
    if (!last || !last.created_at) return
    if (data.items.length < pagination.pageSize) return
    cursorMapRef.current.set(nextPage, {
      createdAt: last.created_at,
      id: last.cursor_id,
      requestId: last.request_id,
      rowId: last.row_id,
    })
  }, [data, isCommon, isPlaceholderData, page, pagination.pageSize])

  const currentStatsTotal =
    !statsIsError && !statsIsPlaceholderData && statsData
      ? statsData.total
      : undefined
  const logs = data?.items || []
  const columns = useColumnsByCategory(logCategory, isAdmin)
  const isLoadingData = isLoading || (isFetching && !data)

  const { table } = useDataTable({
    data: logs as Record<string, unknown>[],
    columns: columns as ColumnDef<Record<string, unknown>>[],
    columnFilters,
    columnVisibilityStorageKey: getColumnVisibilityStorageKey(
      logCategory,
      isAdmin
    ),
    pagination,
    enableRowSelection: false,
    onPaginationChange,
    onColumnFiltersChange,
    manualPagination: true,
    manualFiltering: true,
    totalCount: isCommon
      ? (currentStatsTotal ?? countFallbackQuery.data ?? 0)
      : data?.total || 0,
    ensurePageInRange,
  })

  return (
    <DataTablePage
      table={table}
      columns={columns as ColumnDef<Record<string, unknown>>[]}
      isLoading={isLoadingData}
      isFetching={isFetching}
      emptyTitle={t('No Logs Found')}
      emptyDescription={t(
        'No usage logs available. Logs will appear here once API calls are made.'
      )}
      skeletonKeyPrefix='usage-log-skeleton'
      applyHeaderSize
      tableClassName={cn(
        '[&_[data-slot=table]]:text-[13px] [&_[data-slot=table]_td]:text-[13px] [&_[data-slot=table]_td_*]:text-[13px] [&_[data-slot=table]_th]:text-[13px] [&_[data-slot=table]_th_*]:text-[13px]'
      )}
      mobile={
        <UsageLogsMobileList
          table={table}
          isLoading={isLoadingData}
          logCategory={logCategory}
        />
      }
      toolbar={
        isCommon ? (
          <CommonLogsFilterBar
            table={table}
            stats={
              statsQuery.isPlaceholderData || statsQuery.isError
                ? undefined
                : statsQuery.data
            }
            statsLoading={statsQuery.isLoading || statsQuery.isPlaceholderData}
            statsError={statsQuery.isError}
          />
        ) : (
          <TaskLogsFilterBar table={table} logCategory={logCategory} />
        )
      }
      renderRow={(row) => {
        const logType = (row.original as Record<string, unknown>).type as
          | number
          | undefined
        let tintClass =
          isCommon && logType != null ? (logTypeRowTint[logType] ?? '') : ''
        if (isCommon && isAdmin) {
          const other = parseLogOther(
            ((row.original as Record<string, unknown>).other as string) ?? ''
          )
          if (other?.admin_info?.quota_saturation) {
            tintClass = quotaSaturationRowTint
          }
        }

        return (
          <DataTableRow
            key={row.id}
            row={row}
            className={cn('transition-colors', tintClass)}
            getColumnClassName={() => (isCommon ? 'py-2' : 'py-3.5')}
          />
        )
      }}
    />
  )
}
