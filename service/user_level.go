package service

import (
	"fmt"
	"math"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/user_level_setting"
)

type UserLevelView struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	Description    string  `json:"description,omitempty"`
	ThresholdQuota int64   `json:"threshold_quota"`
	Ratio          float64 `json:"ratio"`
	BadgeColor     string  `json:"badge_color"`
	Archived       bool    `json:"archived"`
	State          string  `json:"state"`
}

type UserLevelProgress struct {
	CurrentQuota int64   `json:"current_quota"`
	TargetQuota  int64   `json:"target_quota"`
	Remaining    int64   `json:"remaining_quota"`
	Percent      float64 `json:"percent"`
}

type UserLevelStatus struct {
	Enabled            bool              `json:"enabled"`
	TotalConsumedQuota int64             `json:"total_consumed_quota"`
	CurrentLevel       UserLevelView     `json:"current_level"`
	NextLevel          *UserLevelView    `json:"next_level,omitempty"`
	ClaimableLevel     *UserLevelView    `json:"claimable_level,omitempty"`
	Progress           UserLevelProgress `json:"progress"`
	Levels             []UserLevelView   `json:"levels"`
}

type UserLevelClaimStatus struct {
	Changed       bool            `json:"changed"`
	PreviousLevel UserLevelView   `json:"previous_level"`
	Status        UserLevelStatus `json:"status"`
}

func GetUserLevelStatus(userId int) (UserLevelStatus, error) {
	if userId <= 0 {
		return UserLevelStatus{}, fmt.Errorf("无效的用户 ID")
	}
	currentConfig := user_level_setting.GetConfig()
	levelKey, totalConsumedQuota, err := model.GetUserLevelProgressFromDB(userId)
	if err != nil {
		return UserLevelStatus{}, err
	}

	currentIndex := 0
	for index, level := range currentConfig.Levels {
		if level.ID == levelKey || (levelKey == "" && level.ID == user_level_setting.BaseLevelID) {
			currentIndex = index
			break
		}
	}

	views := make([]UserLevelView, 0, len(currentConfig.Levels))
	claimableIndex := -1
	nextIndex := -1
	for index, level := range currentConfig.Levels {
		state := "locked"
		switch {
		case index == currentIndex:
			state = "current"
		case level.Archived:
			state = "archived"
		case index < currentIndex:
			state = "passed"
		case level.ThresholdQuota <= totalConsumedQuota:
			state = "passed"
			claimableIndex = index
		}
		if nextIndex == -1 && index > currentIndex && !level.Archived {
			nextIndex = index
		}
		views = append(views, levelView(level, state))
	}
	if claimableIndex >= 0 {
		views[claimableIndex].State = "claimable"
	}

	status := UserLevelStatus{
		Enabled:            currentConfig.Enabled,
		TotalConsumedQuota: totalConsumedQuota,
		CurrentLevel:       views[currentIndex],
		Levels:             views,
	}
	if nextIndex >= 0 {
		next := views[nextIndex]
		status.NextLevel = &next
		remaining := next.ThresholdQuota - totalConsumedQuota
		if remaining < 0 {
			remaining = 0
		}
		currentThreshold := status.CurrentLevel.ThresholdQuota
		span := next.ThresholdQuota - currentThreshold
		percent := 100.0
		if span > 0 {
			percent = float64(totalConsumedQuota-currentThreshold) / float64(span) * 100
		}
		status.Progress = UserLevelProgress{
			CurrentQuota: totalConsumedQuota,
			TargetQuota:  next.ThresholdQuota,
			Remaining:    remaining,
			Percent:      math.Max(0, math.Min(100, percent)),
		}
	} else {
		status.Progress = UserLevelProgress{
			CurrentQuota: totalConsumedQuota,
			TargetQuota:  totalConsumedQuota,
			Percent:      100,
		}
	}
	if claimableIndex >= 0 {
		claimable := views[claimableIndex]
		status.ClaimableLevel = &claimable
	}
	return status, nil
}

func ClaimUserLevel(userId int) (UserLevelClaimStatus, error) {
	claim, err := model.ClaimHighestEligibleUserLevel(userId)
	if err != nil {
		return UserLevelClaimStatus{}, err
	}
	status, err := GetUserLevelStatus(userId)
	if err != nil {
		return UserLevelClaimStatus{}, err
	}
	return UserLevelClaimStatus{
		Changed:       claim.Changed,
		PreviousLevel: levelView(claim.PreviousLevel, "passed"),
		Status:        status,
	}, nil
}

func levelView(level user_level_setting.Level, state string) UserLevelView {
	return UserLevelView{
		ID:             level.ID,
		Name:           level.Name,
		Description:    level.Description,
		ThresholdQuota: level.ThresholdQuota,
		Ratio:          level.Ratio,
		BadgeColor:     level.BadgeColor,
		Archived:       level.Archived,
		State:          state,
	}
}
