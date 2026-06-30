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
