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

import type { ChannelAvailabilitySchedule } from '../types'
import {
  channelAvailabilitySchema,
  evaluateChannelAvailability,
  hasEnabledChannelAvailability,
  mergeChannelAvailabilitySchedule,
  parseChannelAvailabilitySchedule,
  shouldRefreshChannelAvailability,
} from './channel-availability'

const daytimeSchedule: ChannelAvailabilitySchedule = {
  enabled: true,
  start: '08:00',
  end: '12:00',
  timezone: 'Asia/Shanghai',
}

describe('channel availability schedule', () => {
  test('treats the window as start-inclusive and end-exclusive', () => {
    assert.deepEqual(
      evaluateChannelAvailability(
        daytimeSchedule,
        new Date('2026-07-28T00:00:00Z')
      ),
      {
        inWindow: true,
        nextAction: 'disable',
        nextTransitionAtMs: new Date('2026-07-28T04:00:00Z').getTime(),
        nextTime: '12:00',
      }
    )
    assert.deepEqual(
      evaluateChannelAvailability(
        daytimeSchedule,
        new Date('2026-07-28T04:00:00Z')
      ),
      {
        inWindow: false,
        nextAction: 'enable',
        nextTransitionAtMs: new Date('2026-07-29T00:00:00Z').getTime(),
        nextTime: '08:00',
      }
    )
  })

  test('supports an availability window that crosses midnight', () => {
    const overnightSchedule: ChannelAvailabilitySchedule = {
      enabled: true,
      start: '22:00',
      end: '06:00',
      timezone: 'Asia/Shanghai',
    }

    assert.equal(
      evaluateChannelAvailability(
        overnightSchedule,
        new Date('2026-07-28T15:00:00Z')
      )?.inWindow,
      true
    )
    assert.equal(
      evaluateChannelAvailability(
        overnightSchedule,
        new Date('2026-07-28T04:00:00Z')
      )?.inWindow,
      false
    )
  })

  test('resolves missing spring-forward boundaries at the first valid instant', () => {
    const springSchedule: ChannelAvailabilitySchedule = {
      enabled: true,
      start: '02:30',
      end: '04:00',
      timezone: 'America/New_York',
    }

    assert.deepEqual(
      evaluateChannelAvailability(
        springSchedule,
        new Date('2026-03-08T06:59:59Z')
      ),
      {
        inWindow: false,
        nextAction: 'enable',
        nextTransitionAtMs: new Date('2026-03-08T07:00:00Z').getTime(),
        nextTime: '03:00',
      }
    )
    assert.equal(
      evaluateChannelAvailability(
        springSchedule,
        new Date('2026-03-08T07:00:00Z')
      )?.inWindow,
      true
    )
    assert.equal(
      evaluateChannelAvailability(
        springSchedule,
        new Date('2026-03-08T08:00:00Z')
      )?.inWindow,
      false
    )
  })

  test('keeps a window collapsed entirely by spring-forward closed', () => {
    const collapsedSchedule: ChannelAvailabilitySchedule = {
      enabled: true,
      start: '02:10',
      end: '02:50',
      timezone: 'America/New_York',
    }

    for (const instant of [
      '2026-03-08T06:59:59Z',
      '2026-03-08T07:00:00Z',
      '2026-03-08T07:30:00Z',
    ]) {
      assert.equal(
        evaluateChannelAvailability(collapsedSchedule, new Date(instant))
          ?.inWindow,
        false
      )
    }
    assert.equal(
      evaluateChannelAvailability(
        collapsedSchedule,
        new Date('2026-03-08T06:59:59Z')
      )?.nextTransitionAtMs,
      new Date('2026-03-09T06:10:00Z').getTime()
    )
  })

  test('keeps a fall-back window continuous across repeated wall times', () => {
    const fallSchedule: ChannelAvailabilitySchedule = {
      enabled: true,
      start: '01:30',
      end: '01:45',
      timezone: 'America/New_York',
    }

    assert.equal(
      evaluateChannelAvailability(
        fallSchedule,
        new Date('2026-11-01T05:29:59Z')
      )?.inWindow,
      false
    )
    assert.equal(
      evaluateChannelAvailability(
        fallSchedule,
        new Date('2026-11-01T05:30:00Z')
      )?.inWindow,
      true
    )
    assert.deepEqual(
      evaluateChannelAvailability(
        fallSchedule,
        new Date('2026-11-01T06:15:00Z')
      ),
      {
        inWindow: true,
        nextAction: 'disable',
        nextTransitionAtMs: new Date('2026-11-01T06:45:00Z').getTime(),
        nextTime: '01:45',
      }
    )
    assert.equal(
      evaluateChannelAvailability(
        fallSchedule,
        new Date('2026-11-01T06:45:00Z')
      )?.inWindow,
      false
    )
  })

  test('resolves a missing cross-midnight end boundary consistently', () => {
    const overnightSpringSchedule: ChannelAvailabilitySchedule = {
      enabled: true,
      start: '22:00',
      end: '02:30',
      timezone: 'America/New_York',
    }

    assert.deepEqual(
      evaluateChannelAvailability(
        overnightSpringSchedule,
        new Date('2026-03-08T06:59:59Z')
      ),
      {
        inWindow: true,
        nextAction: 'disable',
        nextTransitionAtMs: new Date('2026-03-08T07:00:00Z').getTime(),
        nextTime: '03:00',
      }
    )
    assert.equal(
      evaluateChannelAvailability(
        overnightSpringSchedule,
        new Date('2026-03-08T07:00:00Z')
      )?.inWindow,
      false
    )
  })

  test('rejects equal boundaries and invalid timezones', () => {
    assert.equal(
      channelAvailabilitySchema.safeParse({
        ...daytimeSchedule,
        end: '08:00',
      }).success,
      false
    )
    assert.equal(
      channelAvailabilitySchema.safeParse({
        ...daytimeSchedule,
        timezone: 'Invalid/Timezone',
      }).success,
      false
    )
  })

  test('parses and merges the schedule without dropping other settings', () => {
    const settings = JSON.stringify({ disable_store: true })
    const merged = mergeChannelAvailabilitySchedule(settings, daytimeSchedule)

    assert.deepEqual(JSON.parse(merged), {
      disable_store: true,
      availability_schedule: daytimeSchedule,
    })
    assert.deepEqual(parseChannelAvailabilitySchedule(merged), daytimeSchedule)
  })

  test('keeps refreshing status-filtered lists after a scheduled row disappears', () => {
    assert.equal(shouldRefreshChannelAvailability([], ['enabled']), true)
    assert.equal(shouldRefreshChannelAvailability([], ['disabled']), true)
    assert.equal(shouldRefreshChannelAvailability([], ['all']), false)

    assert.equal(
      shouldRefreshChannelAvailability(
        [
          {
            settings: JSON.stringify({
              availability_schedule: daytimeSchedule,
            }),
          },
        ],
        []
      ),
      true
    )

    assert.equal(
      hasEnabledChannelAvailability([
        {
          children: [
            {
              settings: JSON.stringify({
                availability_schedule: daytimeSchedule,
              }),
            },
          ],
        },
      ]),
      true
    )
  })
})
