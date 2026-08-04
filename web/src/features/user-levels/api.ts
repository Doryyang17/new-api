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
  UserLevelAdminData,
  UserLevelApiResponse,
  UserLevelClaimResult,
  UserLevelConfigUpdate,
  UserLevelStatus,
} from './types'

function requireData<T>(
  response: UserLevelApiResponse<T>,
  fallback: string
): T {
  if (!response.success || !response.data) {
    throw new Error(response.message || fallback)
  }
  return response.data
}

export async function getUserLevelStatus(): Promise<UserLevelStatus> {
  const response =
    await api.get<UserLevelApiResponse<UserLevelStatus>>('/api/user/level')
  return requireData(response.data, '用户等级信息加载失败')
}

export async function claimUserLevel(): Promise<UserLevelClaimResult> {
  const response = await api.post<UserLevelApiResponse<UserLevelClaimResult>>(
    '/api/user/level/claim'
  )
  return requireData(response.data, '等级领取失败')
}

export async function getUserLevelAdminConfig(): Promise<UserLevelAdminData> {
  const response = await api.get<UserLevelApiResponse<UserLevelAdminData>>(
    '/api/option/user-level'
  )
  return requireData(response.data, '用户等级设置加载失败')
}

export async function updateUserLevelAdminConfig(
  update: UserLevelConfigUpdate
): Promise<UserLevelAdminData> {
  const response = await api.put<UserLevelApiResponse<UserLevelAdminData>>(
    '/api/option/user-level',
    update
  )
  return requireData(response.data, '用户等级设置保存失败')
}
