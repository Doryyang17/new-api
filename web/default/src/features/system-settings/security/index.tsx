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
import { SettingsPage } from '../components/settings-page'
import type { SecuritySettings } from '../types'
import {
  SECURITY_DEFAULT_SECTION,
  getSecuritySectionContent,
  getSecuritySectionMeta,
} from './section-registry.tsx'

const defaultSecuritySettings: SecuritySettings = {
  ModelRequestRateLimitEnabled: false,
  ModelRequestRateLimitCount: 0,
  ModelRequestRateLimitSuccessCount: 1000,
  ModelRequestRateLimitDurationMinutes: 1,
  ModelRequestRateLimitGroup: '',
  'request_risk_setting.enabled': false,
  'request_risk_setting.mode': 'observe',
  'request_risk_setting.log_matches': true,
  'request_risk_setting.medium_cooldown_seconds': 10,
  'request_risk_setting.token_block_seconds': 300,
  'request_risk_setting.user_block_seconds': 120,
  'request_risk_setting.ip_block_seconds': 60,
  'request_risk_setting.user_concurrency_limit': 8,
  'request_risk_setting.token_concurrency_limit': 4,
  'request_risk_setting.group_whitelist': [],
  'fetch_setting.enable_ssrf_protection': true,
  'fetch_setting.allow_private_ip': false,
  'fetch_setting.domain_filter_mode': false,
  'fetch_setting.ip_filter_mode': false,
  'fetch_setting.domain_list': [],
  'fetch_setting.ip_list': [],
  'fetch_setting.allowed_ports': [],
  'fetch_setting.apply_ip_filter_for_domain': false,
  'token_setting.max_user_tokens': 1000,
}

export function SecuritySettings() {
  return (
    <SettingsPage
      routePath='/_authenticated/system-settings/security/$section'
      defaultSettings={defaultSecuritySettings}
      defaultSection={SECURITY_DEFAULT_SECTION}
      getSectionContent={getSecuritySectionContent}
      getSectionMeta={getSecuritySectionMeta}
    />
  )
}
