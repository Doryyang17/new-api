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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import { clampAnnouncementPage, getAnnouncementTotalPages } from './pagination'

describe('announcement center pagination', () => {
  test('moves stale and oversized page parameters back into range', () => {
    assert.equal(getAnnouncementTotalPages(0, 12), 1)
    assert.equal(getAnnouncementTotalPages(25, 12), 3)
    assert.equal(clampAnnouncementPage(999, 3), 3)
    assert.equal(clampAnnouncementPage(-1, 3), 1)
  })
})
