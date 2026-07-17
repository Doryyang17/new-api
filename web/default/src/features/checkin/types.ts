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

export interface ApiResponse<T = unknown> {
  success: boolean
  message?: string
  data?: T
}

export interface CheckinRecord {
  checkin_date: string
  quota_awarded: number
  bonus_awarded?: number
}

export interface CheckinStats {
  checked_in_today: boolean
  total_checkins: number
  total_quota: number
  total_bonus: number
  total_reward: number
  monthly_reward: number
  checkin_count: number
  records: CheckinRecord[]
}

export interface CheckinBonus {
  amount: number
  remaining_amount: number
  created_at: number
  expire_at: number
  status: 'active' | 'consumed' | 'expired'
}

export interface CheckinBonusSetting {
  enabled: boolean
  min_amount: number
  max_amount: number
}

export interface CheckinStatusResponse {
  enabled: boolean
  bonus_setting: CheckinBonusSetting
  active_bonus: CheckinBonus | null
  latest_bonus: CheckinBonus | null
  stats: CheckinStats
}

export interface CheckinResponse {
  quota_awarded: number
  checkin_date: string
  bonus: CheckinBonus | null
}
