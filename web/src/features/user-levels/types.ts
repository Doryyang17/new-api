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
export type UserLevelState =
  | 'locked'
  | 'claimable'
  | 'current'
  | 'passed'
  | 'archived'

export type UserLevel = {
  id: string
  name: string
  description?: string
  threshold_quota: number
  ratio: number
  badge_color: string
  archived: boolean
  state: UserLevelState
}

export type UserLevelProgress = {
  current_quota: number
  target_quota: number
  remaining_quota: number
  percent: number
}

export type UserLevelStatus = {
  enabled: boolean
  total_consumed_quota: number
  current_level: UserLevel
  next_level?: UserLevel
  claimable_level?: UserLevel
  progress: UserLevelProgress
  levels: UserLevel[]
}

export type UserLevelClaimResult = {
  changed: boolean
  previous_level: UserLevel
  status: UserLevelStatus
  reconciled?: boolean
}

export type UserLevelDefinition = {
  id: string
  name: string
  description?: string
  threshold_quota: number
  ratio: number
  badge_color: string
  archived: boolean
}

export type UserLevelConfig = {
  schema_version: 1
  enabled: boolean
  levels: UserLevelDefinition[]
}

export type UserLevelAdminData = {
  config: UserLevelConfig
  member_counts: Record<string, number>
  revision: string
  reconciled?: boolean
}

export type UserLevelConfigUpdate = {
  config: UserLevelConfig
  revision: string
}

export type UserLevelApiResponse<T> = {
  success: boolean
  message?: string
  data?: T
}
