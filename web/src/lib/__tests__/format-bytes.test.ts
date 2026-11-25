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
import { expect, test } from 'vitest'

import { formatBytes } from '../format'

test('formatBytes formats fractional megabytes in Chinese', () => {
  expect(formatBytes(1_200_000, 'zh-CN')).toBe('1.2 MB')
})

test('formatBytes keeps small sizes in bytes and moves up at each thousand', () => {
  expect(formatBytes(512)).toBe('512 B')
  expect(formatBytes(800_000)).toBe('800 KB')
  expect(formatBytes(3_000_000_000)).toBe('3 GB')
})
