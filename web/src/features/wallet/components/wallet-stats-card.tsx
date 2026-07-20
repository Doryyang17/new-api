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
import { AccountBalanceSummary } from '@/features/checkin'

import type { UserWalletData } from '../types'

interface WalletStatsCardProps {
  user: UserWalletData | null
  loading?: boolean
  checkinEnabled: boolean
}

export function WalletStatsCard(props: WalletStatsCardProps) {
  return (
    <AccountBalanceSummary
      balance={props.user?.quota ?? 0}
      usedQuota={props.user?.used_quota ?? 0}
      requestCount={props.user?.request_count ?? 0}
      loading={props.loading === true}
      checkinEnabled={props.checkinEnabled}
      variant='standalone'
    />
  )
}
