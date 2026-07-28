package dto

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	DefaultChannelAvailabilityStart    = "08:00"
	DefaultChannelAvailabilityEnd      = "12:00"
	DefaultChannelAvailabilityTimezone = "Asia/Shanghai"
)

var channelAvailabilityClockPattern = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

type ChannelAvailabilitySchedule struct {
	Enabled  bool   `json:"enabled"`
	Start    string `json:"start"`
	End      string `json:"end"`
	Timezone string `json:"timezone"`
}

type ChannelAvailabilityWindow struct {
	Available        bool
	StartAt          time.Time
	EndAt            time.Time
	NextTransitionAt time.Time
}

func (s ChannelAvailabilitySchedule) Normalize() (ChannelAvailabilitySchedule, error) {
	s.Start = strings.TrimSpace(s.Start)
	s.End = strings.TrimSpace(s.End)
	s.Timezone = strings.TrimSpace(s.Timezone)
	if s.Start == "" {
		s.Start = DefaultChannelAvailabilityStart
	}
	if s.End == "" {
		s.End = DefaultChannelAvailabilityEnd
	}
	if s.Timezone == "" {
		s.Timezone = DefaultChannelAvailabilityTimezone
	}

	if !channelAvailabilityClockPattern.MatchString(s.Start) {
		return ChannelAvailabilitySchedule{}, fmt.Errorf("渠道可用开始时间 %q 无效，应为 HH:MM", s.Start)
	}
	if !channelAvailabilityClockPattern.MatchString(s.End) {
		return ChannelAvailabilitySchedule{}, fmt.Errorf("渠道可用结束时间 %q 无效，应为 HH:MM", s.End)
	}
	if s.Start == s.End {
		return ChannelAvailabilitySchedule{}, fmt.Errorf("渠道可用开始时间和结束时间不能相同")
	}
	if _, err := time.LoadLocation(s.Timezone); err != nil {
		return ChannelAvailabilitySchedule{}, fmt.Errorf("渠道可用时区 %q 无效: %w", s.Timezone, err)
	}
	return s, nil
}

func (s ChannelAvailabilitySchedule) WindowAt(now time.Time) (ChannelAvailabilityWindow, error) {
	normalized, err := s.Normalize()
	if err != nil {
		return ChannelAvailabilityWindow{}, err
	}
	location, err := time.LoadLocation(normalized.Timezone)
	if err != nil {
		return ChannelAvailabilityWindow{}, err
	}
	startMinute, err := channelAvailabilityMinute(normalized.Start)
	if err != nil {
		return ChannelAvailabilityWindow{}, err
	}
	endMinute, err := channelAvailabilityMinute(normalized.End)
	if err != nil {
		return ChannelAvailabilityWindow{}, err
	}

	localNow := now.In(location)
	localDate := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, time.UTC)
	startToday, err := channelAvailabilityBoundaryAt(localDate, startMinute, location, false)
	if err != nil {
		return ChannelAvailabilityWindow{}, err
	}
	endToday, err := channelAvailabilityBoundaryAt(localDate, endMinute, location, true)
	if err != nil {
		return ChannelAvailabilityWindow{}, err
	}

	if startMinute < endMinute {
		if !startToday.Before(endToday) {
			startTomorrow, err := channelAvailabilityBoundaryAt(localDate.AddDate(0, 0, 1), startMinute, location, false)
			if err != nil {
				return ChannelAvailabilityWindow{}, err
			}
			return ChannelAvailabilityWindow{NextTransitionAt: startTomorrow}, nil
		}
		if localNow.Before(startToday) {
			return ChannelAvailabilityWindow{NextTransitionAt: startToday}, nil
		}
		if localNow.Before(endToday) {
			return ChannelAvailabilityWindow{
				Available:        true,
				StartAt:          startToday,
				EndAt:            endToday,
				NextTransitionAt: endToday,
			}, nil
		}
		startTomorrow, err := channelAvailabilityBoundaryAt(localDate.AddDate(0, 0, 1), startMinute, location, false)
		if err != nil {
			return ChannelAvailabilityWindow{}, err
		}
		return ChannelAvailabilityWindow{NextTransitionAt: startTomorrow}, nil
	}

	if !localNow.Before(startToday) {
		endTomorrow, err := channelAvailabilityBoundaryAt(localDate.AddDate(0, 0, 1), endMinute, location, true)
		if err != nil {
			return ChannelAvailabilityWindow{}, err
		}
		return ChannelAvailabilityWindow{
			Available:        true,
			StartAt:          startToday,
			EndAt:            endTomorrow,
			NextTransitionAt: endTomorrow,
		}, nil
	}
	if localNow.Before(endToday) {
		startYesterday, err := channelAvailabilityBoundaryAt(localDate.AddDate(0, 0, -1), startMinute, location, false)
		if err != nil {
			return ChannelAvailabilityWindow{}, err
		}
		return ChannelAvailabilityWindow{
			Available:        true,
			StartAt:          startYesterday,
			EndAt:            endToday,
			NextTransitionAt: endToday,
		}, nil
	}
	return ChannelAvailabilityWindow{NextTransitionAt: startToday}, nil
}

// channelAvailabilityBoundaryAt resolves one local wall-clock boundary to an
// absolute instant. During a DST fall-back, starts use the first occurrence and
// ends use the second so the window stays continuous. During a spring-forward,
// a missing wall time resolves to the first valid instant after the gap.
func channelAvailabilityBoundaryAt(localDate time.Time, minute int, location *time.Location, preferLatest bool) (time.Time, error) {
	targetWall := time.Date(
		localDate.Year(),
		localDate.Month(),
		localDate.Day(),
		minute/60,
		minute%60,
		0,
		0,
		time.UTC,
	)
	offsets := make(map[int]struct{})
	for _, delta := range [...]time.Duration{
		-7 * 24 * time.Hour,
		-48 * time.Hour,
		-24 * time.Hour,
		0,
		24 * time.Hour,
		48 * time.Hour,
		7 * 24 * time.Hour,
	} {
		_, offset := targetWall.Add(delta).In(location).Zone()
		offsets[offset] = struct{}{}
	}

	exact := make([]time.Time, 0, len(offsets))
	var beforeGap time.Time
	var beforeGapWall time.Time
	var afterGap time.Time
	var afterGapWall time.Time
	for offset := range offsets {
		candidate := targetWall.Add(-time.Duration(offset) * time.Second)
		candidateWall := channelAvailabilityWallTime(candidate.In(location))
		if candidateWall.Equal(targetWall) {
			exact = append(exact, candidate)
			continue
		}
		if candidateWall.Before(targetWall) {
			if beforeGap.IsZero() || candidateWall.After(beforeGapWall) {
				beforeGap = candidate
				beforeGapWall = candidateWall
			}
			continue
		}
		if afterGap.IsZero() || candidateWall.Before(afterGapWall) {
			afterGap = candidate
			afterGapWall = candidateWall
		}
	}

	if len(exact) > 0 {
		selected := exact[0]
		for _, candidate := range exact[1:] {
			if (preferLatest && candidate.After(selected)) || (!preferLatest && candidate.Before(selected)) {
				selected = candidate
			}
		}
		return selected.In(location), nil
	}
	if !beforeGap.IsZero() && !afterGap.IsZero() && beforeGap.Before(afterGap) {
		low := beforeGap.Unix()
		high := afterGap.Unix()
		for low < high {
			middle := low + (high-low)/2
			if channelAvailabilityWallTime(time.Unix(middle, 0).In(location)).Before(targetWall) {
				low = middle + 1
			} else {
				high = middle
			}
		}
		return time.Unix(low, 0).In(location), nil
	}
	return time.Time{}, fmt.Errorf(
		"无法解析渠道可用时间边界 %04d-%02d-%02d %02d:%02d (%s)",
		localDate.Year(),
		localDate.Month(),
		localDate.Day(),
		minute/60,
		minute%60,
		location.String(),
	)
}

func channelAvailabilityWallTime(value time.Time) time.Time {
	return time.Date(
		value.Year(),
		value.Month(),
		value.Day(),
		value.Hour(),
		value.Minute(),
		value.Second(),
		0,
		time.UTC,
	)
}

func channelAvailabilityMinute(value string) (int, error) {
	parsed, err := time.Parse("15:04", value)
	if err != nil {
		return 0, err
	}
	return parsed.Hour()*60 + parsed.Minute(), nil
}
