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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import {
  claimUserLevel,
  getUserLevelAdminConfig,
  getUserLevelStatus,
  updateUserLevelAdminConfig,
} from './api'
import type { UserLevelClaimResult, UserLevelConfigUpdate } from './types'

export const USER_LEVEL_STATUS_QUERY_KEY = ['user-level', 'status'] as const
export const USER_LEVEL_ADMIN_QUERY_KEY = [
  'user-level',
  'admin-config',
] as const

export function useUserLevelStatus(enabled: boolean) {
  return useQuery({
    queryKey: USER_LEVEL_STATUS_QUERY_KEY,
    queryFn: getUserLevelStatus,
    enabled,
    staleTime: 30_000,
    refetchOnWindowFocus: true,
  })
}

export function useClaimUserLevel() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (beforeLevelId: string) => {
      try {
        return await claimUserLevel()
      } catch (claimError) {
        // A mutating request may have committed even when its response was
        // interrupted. Re-read durable state before telling the user it failed.
        try {
          const status = await getUserLevelStatus()
          if (status.current_level.id !== beforeLevelId) {
            const previousLevel =
              status.levels.find((level) => level.id === beforeLevelId) ??
              status.current_level
            return {
              changed: true,
              previous_level: previousLevel,
              status,
              reconciled: true,
            } satisfies UserLevelClaimResult
          }
        } catch {
          // Preserve the original claim error when reconciliation also fails.
        }
        throw claimError
      }
    },
    onSuccess: (result) => {
      queryClient.setQueryData(USER_LEVEL_STATUS_QUERY_KEY, result.status)
    },
    onSettled: async () => {
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: USER_LEVEL_STATUS_QUERY_KEY,
        }),
        queryClient.invalidateQueries({ queryKey: ['pricing'] }),
        queryClient.invalidateQueries({ queryKey: ['user-groups'] }),
      ])
    },
  })
}

export function useUserLevelAdminConfig() {
  return useQuery({
    queryKey: USER_LEVEL_ADMIN_QUERY_KEY,
    queryFn: getUserLevelAdminConfig,
  })
}

export function useUpdateUserLevelAdminConfig() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: async (update: UserLevelConfigUpdate) => {
      try {
        return await updateUserLevelAdminConfig(update)
      } catch (updateError) {
        // The option may be durable even if the response is interrupted or a
        // post-commit read fails. Confirm server state before reporting a
        // retryable error that could make the administrator submit twice.
        try {
          const current = await getUserLevelAdminConfig()
          if (
            JSON.stringify(current.config) === JSON.stringify(update.config)
          ) {
            return { ...current, reconciled: true }
          }
        } catch {
          // Preserve the original mutation error when reconciliation fails.
        }
        throw updateError
      }
    },
    onSuccess: (data) => {
      queryClient.setQueryData(USER_LEVEL_ADMIN_QUERY_KEY, data)
      void Promise.all([
        queryClient.invalidateQueries({
          queryKey: USER_LEVEL_STATUS_QUERY_KEY,
        }),
        queryClient.invalidateQueries({ queryKey: ['status'] }),
        queryClient.invalidateQueries({ queryKey: ['pricing'] }),
        queryClient.invalidateQueries({ queryKey: ['user-groups'] }),
      ])
    },
  })
}
