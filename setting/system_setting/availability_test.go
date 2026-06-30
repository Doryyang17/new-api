package system_setting

import (
	"testing"
	"time"
)

func withAvailabilitySettings(t *testing.T, settings AvailabilitySettings) {
	t.Helper()
	original := availabilitySettings
	availabilitySettings = settings
	t.Cleanup(func() {
		availabilitySettings = original
	})
}

func mustShanghai(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(DefaultAvailabilityTZ)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func TestAvailabilityStatusDisabled(t *testing.T) {
	withAvailabilitySettings(t, AvailabilitySettings{
		Enabled:          false,
		UnavailableStart: "22:00",
		UnavailableEnd:   "08:00",
		Timezone:         DefaultAvailabilityTZ,
		Message:          DefaultAvailabilityMsg,
	})

	status := GetAvailabilityStatusAt(time.Date(2026, 6, 30, 23, 0, 0, 0, mustShanghai(t)))
	if status.Unavailable {
		t.Fatal("disabled availability setting should not block requests")
	}
}

func TestAvailabilityStatusCrossMidnightBoundaries(t *testing.T) {
	withAvailabilitySettings(t, AvailabilitySettings{
		Enabled:          true,
		UnavailableStart: "22:00",
		UnavailableEnd:   "08:00",
		Timezone:         DefaultAvailabilityTZ,
		Message:          DefaultAvailabilityMsg,
	})
	loc := mustShanghai(t)

	cases := []struct {
		name        string
		now         time.Time
		unavailable bool
	}{
		{"before start", time.Date(2026, 6, 30, 21, 59, 59, 0, loc), false},
		{"at start", time.Date(2026, 6, 30, 22, 0, 0, 0, loc), true},
		{"before end", time.Date(2026, 7, 1, 7, 59, 59, 0, loc), true},
		{"at end", time.Date(2026, 7, 1, 8, 0, 0, 0, loc), false},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			status := GetAvailabilityStatusAt(tt.now)
			if status.Unavailable != tt.unavailable {
				t.Fatalf("Unavailable = %v, want %v", status.Unavailable, tt.unavailable)
			}
		})
	}
}

func TestAvailabilityStatusSameDayWindow(t *testing.T) {
	withAvailabilitySettings(t, AvailabilitySettings{
		Enabled:          true,
		UnavailableStart: "09:00",
		UnavailableEnd:   "17:00",
		Timezone:         DefaultAvailabilityTZ,
		Message:          DefaultAvailabilityMsg,
	})
	loc := mustShanghai(t)

	cases := []struct {
		name        string
		now         time.Time
		unavailable bool
	}{
		{"before start", time.Date(2026, 6, 30, 8, 59, 59, 0, loc), false},
		{"at start", time.Date(2026, 6, 30, 9, 0, 0, 0, loc), true},
		{"before end", time.Date(2026, 6, 30, 16, 59, 59, 0, loc), true},
		{"at end", time.Date(2026, 6, 30, 17, 0, 0, 0, loc), false},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			status := GetAvailabilityStatusAt(tt.now)
			if status.Unavailable != tt.unavailable {
				t.Fatalf("Unavailable = %v, want %v", status.Unavailable, tt.unavailable)
			}
		})
	}
}

func TestAvailabilityStatusFailClosed(t *testing.T) {
	withAvailabilitySettings(t, AvailabilitySettings{
		Enabled:          true,
		UnavailableStart: "bad",
		UnavailableEnd:   "08:00",
		Timezone:         DefaultAvailabilityTZ,
		Message:          DefaultAvailabilityMsg,
	})

	status := GetAvailabilityStatusAt(time.Date(2026, 6, 30, 12, 0, 0, 0, mustShanghai(t)))
	if !status.Unavailable {
		t.Fatal("invalid enabled availability config should fail closed")
	}
	if status.EvaluationError == "" {
		t.Fatal("expected evaluation error for invalid availability config")
	}
}

func TestValidateAvailabilityOption(t *testing.T) {
	valid := map[string]string{
		"availability_setting.enabled":           "true",
		"availability_setting.unavailable_start": "22:00",
		"availability_setting.unavailable_end":   "08:00",
		"availability_setting.timezone":          DefaultAvailabilityTZ,
		"availability_setting.message":           DefaultAvailabilityMsg,
	}
	for key, value := range valid {
		t.Run(key, func(t *testing.T) {
			if err := ValidateAvailabilityOption(key, value); err != nil {
				t.Fatalf("ValidateAvailabilityOption(%q, %q) returned error: %v", key, value, err)
			}
		})
	}

	invalid := map[string]string{
		"availability_setting.enabled":           "yes",
		"availability_setting.unavailable_start": "24:00",
		"availability_setting.unavailable_end":   "bad",
		"availability_setting.timezone":          "Local/Nowhere",
		"availability_setting.message":           "",
	}
	for key, value := range invalid {
		t.Run(key, func(t *testing.T) {
			if err := ValidateAvailabilityOption(key, value); err == nil {
				t.Fatalf("ValidateAvailabilityOption(%q, %q) should reject invalid value", key, value)
			}
		})
	}
}
