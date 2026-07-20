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
/* eslint-disable react-refresh/only-export-components */
import { CheckinSettingsSection } from '../general/checkin-settings-section'
import { AvailabilitySection } from '../maintenance/availability-section'
import { DailyUsageLimitSection } from '../maintenance/daily-usage-limit-section'
import { RequestRiskSection } from '../request-limits/request-risk-section'
import { SensitiveWordsSection } from '../request-limits/sensitive-words-section'
import type { CustomSettings } from '../types'
import { createSectionRegistry } from '../utils/section-registry'
import { QuotaGrantSection } from './quota-grant-section'
import { RegistrationCodesSection } from './registration-codes-section'

const CUSTOM_SECTIONS = [
  {
    id: 'quota-grants',
    titleKey: 'Quota Grants',
    build: () => <QuotaGrantSection />,
  },
  {
    id: 'registration-codes',
    titleKey: 'Registration Codes',
    build: (settings: CustomSettings) => (
      <RegistrationCodesSection
        defaultValues={{
          RegistrationCodeRegisterEnabled:
            settings.RegistrationCodeRegisterEnabled ?? false,
        }}
      />
    ),
  },
  {
    id: 'daily-usage-limit',
    titleKey: 'Daily Usage Limit',
    build: (settings: CustomSettings) => (
      <DailyUsageLimitSection
        defaultValues={{
          'daily_usage_setting.enabled':
            settings['daily_usage_setting.enabled'] ?? false,
          'daily_usage_setting.limit_tokens':
            settings['daily_usage_setting.limit_tokens'] ?? 0,
          'daily_usage_setting.timezone':
            settings['daily_usage_setting.timezone'] ?? 'Asia/Shanghai',
          'daily_usage_setting.message':
            settings['daily_usage_setting.message'] ??
            '当日系统使用量已超上限，请每天再来。',
          'daily_usage_setting.model_limits':
            settings['daily_usage_setting.model_limits'] ?? [],
        }}
      />
    ),
  },
  {
    id: 'availability',
    titleKey: 'System Availability',
    build: (settings: CustomSettings) => (
      <AvailabilitySection
        defaultValues={{
          'availability_setting.enabled':
            settings['availability_setting.enabled'] ?? false,
          'availability_setting.unavailable_start':
            settings['availability_setting.unavailable_start'] ?? '22:00',
          'availability_setting.unavailable_end':
            settings['availability_setting.unavailable_end'] ?? '08:00',
          'availability_setting.timezone':
            settings['availability_setting.timezone'] ?? 'Asia/Shanghai',
          'availability_setting.message':
            settings['availability_setting.message'] ??
            '当前处于宵禁状态，22:00-8:00期间服务不可用，敬请谅解~',
        }}
      />
    ),
  },
  {
    id: 'prompt-filter',
    titleKey: 'Prompt Filter',
    build: (settings: CustomSettings) => (
      <SensitiveWordsSection
        defaultValues={{
          CheckSensitiveEnabled: settings.CheckSensitiveEnabled,
          CheckSensitiveOnPromptEnabled: settings.CheckSensitiveOnPromptEnabled,
          SensitiveWords: settings.SensitiveWords,
          'prompt_filter_setting.mode': settings['prompt_filter_setting.mode'],
          'prompt_filter_setting.threshold':
            settings['prompt_filter_setting.threshold'],
          'prompt_filter_setting.strict_threshold':
            settings['prompt_filter_setting.strict_threshold'],
          'prompt_filter_setting.log_matches':
            settings['prompt_filter_setting.log_matches'],
          'prompt_filter_setting.max_text_length':
            settings['prompt_filter_setting.max_text_length'],
          'prompt_filter_setting.message':
            settings['prompt_filter_setting.message'],
          'prompt_filter_setting.block_status_code':
            settings['prompt_filter_setting.block_status_code'],
          'prompt_filter_setting.block_error_code':
            settings['prompt_filter_setting.block_error_code'],
          'prompt_filter_setting.group_whitelist':
            settings['prompt_filter_setting.group_whitelist'],
          'prompt_filter_setting.channel_whitelist':
            settings['prompt_filter_setting.channel_whitelist'],
          'prompt_filter_setting.custom_patterns':
            settings['prompt_filter_setting.custom_patterns'],
          'prompt_filter_setting.disabled_patterns':
            settings['prompt_filter_setting.disabled_patterns'],
          'prompt_filter_setting.review_enabled':
            settings['prompt_filter_setting.review_enabled'],
          'prompt_filter_setting.review_base_url':
            settings['prompt_filter_setting.review_base_url'],
          'prompt_filter_setting.review_model':
            settings['prompt_filter_setting.review_model'],
          'prompt_filter_setting.review_timeout_seconds':
            settings['prompt_filter_setting.review_timeout_seconds'],
          'prompt_filter_setting.review_fail_closed':
            settings['prompt_filter_setting.review_fail_closed'],
        }}
      />
    ),
  },
  {
    id: 'request-risk',
    titleKey: '批量测活与并发防护',
    build: (settings: CustomSettings) => (
      <RequestRiskSection
        defaultValues={{
          'request_risk_setting.enabled':
            settings['request_risk_setting.enabled'],
          'request_risk_setting.mode': settings['request_risk_setting.mode'],
          'request_risk_setting.concurrency_mode':
            settings['request_risk_setting.concurrency_mode'],
          'request_risk_setting.log_matches':
            settings['request_risk_setting.log_matches'],
          'request_risk_setting.medium_cooldown_seconds':
            settings['request_risk_setting.medium_cooldown_seconds'],
          'request_risk_setting.token_block_seconds':
            settings['request_risk_setting.token_block_seconds'],
          'request_risk_setting.user_block_seconds':
            settings['request_risk_setting.user_block_seconds'],
          'request_risk_setting.ip_block_seconds':
            settings['request_risk_setting.ip_block_seconds'],
          'request_risk_setting.user_concurrency_limit':
            settings['request_risk_setting.user_concurrency_limit'],
          'request_risk_setting.token_concurrency_limit':
            settings['request_risk_setting.token_concurrency_limit'],
          'request_risk_setting.group_whitelist':
            settings['request_risk_setting.group_whitelist'],
        }}
      />
    ),
  },
  {
    id: 'checkin',
    titleKey: 'Check-in Rewards',
    build: (settings: CustomSettings) => (
      <CheckinSettingsSection
        defaultValues={{
          enabled: settings['checkin_setting.enabled'],
          minQuota: settings['checkin_setting.min_quota'],
          maxQuota: settings['checkin_setting.max_quota'],
          bonusEnabled: settings['checkin_bonus_setting.enabled'],
          bonusMinAmount: settings['checkin_bonus_setting.min_amount'],
          bonusMaxAmount: settings['checkin_bonus_setting.max_amount'],
        }}
      />
    ),
  },
] as const

export type CustomSectionId = (typeof CUSTOM_SECTIONS)[number]['id']

const customRegistry = createSectionRegistry<CustomSectionId, CustomSettings>({
  sections: CUSTOM_SECTIONS,
  defaultSection: 'availability',
  basePath: '/system-settings/custom',
  urlStyle: 'path',
})

export const CUSTOM_SECTION_IDS = customRegistry.sectionIds
export const CUSTOM_DEFAULT_SECTION = customRegistry.defaultSection
export const getCustomSectionNavItems = customRegistry.getSectionNavItems
export const getCustomSectionContent = customRegistry.getSectionContent
export const getCustomSectionMeta = customRegistry.getSectionMeta
