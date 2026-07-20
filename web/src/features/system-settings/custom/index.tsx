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
import type { CustomSettings } from '../types'
import {
  CUSTOM_DEFAULT_SECTION,
  getCustomSectionContent,
  getCustomSectionMeta,
} from './section-registry.tsx'

const defaultCustomSettings: CustomSettings = {
  RegistrationCodeRegisterEnabled: false,
  'availability_setting.enabled': false,
  'availability_setting.unavailable_start': '22:00',
  'availability_setting.unavailable_end': '08:00',
  'availability_setting.timezone': 'Asia/Shanghai',
  'availability_setting.message':
    '当前处于宵禁状态，22:00-8:00期间服务不可用，敬请谅解~',
  'daily_usage_setting.enabled': false,
  'daily_usage_setting.limit_tokens': 0,
  'daily_usage_setting.timezone': 'Asia/Shanghai',
  'daily_usage_setting.message': '当日系统使用量已超上限，请每天再来。',
  'daily_usage_setting.model_limits': [],
  CheckSensitiveEnabled: false,
  CheckSensitiveOnPromptEnabled: false,
  SensitiveWords: '',
  'prompt_filter_setting.mode': 'block',
  'prompt_filter_setting.threshold': 50,
  'prompt_filter_setting.strict_threshold': 90,
  'prompt_filter_setting.log_matches': true,
  'prompt_filter_setting.max_text_length': 80 * 1024,
  'prompt_filter_setting.message':
    'Request contains content blocked by prompt filter',
  'prompt_filter_setting.block_status_code': 460,
  'prompt_filter_setting.block_error_code': 'prompt_blocked',
  'prompt_filter_setting.group_whitelist': [],
  'prompt_filter_setting.channel_whitelist': [],
  'prompt_filter_setting.custom_patterns': '[]',
  'prompt_filter_setting.disabled_patterns': '[]',
  'prompt_filter_setting.review_enabled': false,
  'prompt_filter_setting.review_base_url': 'https://api.openai.com',
  'prompt_filter_setting.review_model': 'omni-moderation-latest',
  'prompt_filter_setting.review_timeout_seconds': 10,
  'prompt_filter_setting.review_fail_closed': true,
  'checkin_setting.enabled': false,
  'checkin_setting.min_quota': 1000,
  'checkin_setting.max_quota': 10000,
  'checkin_bonus_setting.enabled': false,
  'checkin_bonus_setting.min_amount': 50000,
  'checkin_bonus_setting.max_amount': 500000,
  'request_risk_setting.enabled': false,
  'request_risk_setting.mode': 'observe',
  'request_risk_setting.concurrency_mode': '',
  'request_risk_setting.log_matches': true,
  'request_risk_setting.medium_cooldown_seconds': 10,
  'request_risk_setting.token_block_seconds': 300,
  'request_risk_setting.user_block_seconds': 120,
  'request_risk_setting.ip_block_seconds': 60,
  'request_risk_setting.user_concurrency_limit': 8,
  'request_risk_setting.token_concurrency_limit': 4,
  'request_risk_setting.group_whitelist': [],
}

export function CustomSettings() {
  return (
    <SettingsPage
      routePath='/_authenticated/system-settings/custom/$section'
      defaultSettings={defaultCustomSettings}
      defaultSection={CUSTOM_DEFAULT_SECTION}
      getSectionContent={getCustomSectionContent}
      getSectionMeta={getCustomSectionMeta}
      loadingMessage='Loading custom feature settings...'
    />
  )
}
