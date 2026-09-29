package service

import (
	"testing"
	"time"
)

func TestParsePaymentDashboardRange(t *testing.T) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 29, 15, 30, 0, 0, loc)
	for _, tc := range []struct {
		name, start, end, days, wantStart, wantEnd string
		wantDays                                   int
		wantError                                  bool
	}{
		{"default", "", "", "", "2026-08-31", "2026-09-30", 30, false},
		{"one day", "", "", "1", "2026-09-29", "2026-09-30", 1, false},
		{"historical leap month", "2024-02-01", "2024-02-29", "", "2024-02-01", "2024-03-01", 29, false},
		{"whole leap year", "2024-01-01", "2024-12-31", "", "2024-01-01", "2025-01-01", 366, false},
		{"cross year", "2025-12-01", "2026-02-28", "", "2025-12-01", "2026-03-01", 90, false},
		{"spring DST", "2026-03-07", "2026-03-09", "", "2026-03-07", "2026-03-10", 3, false},
		{"fall DST", "2025-11-01", "2025-11-03", "", "2025-11-01", "2025-11-04", 3, false},
		{"missing end", "2026-01-01", "", "", "", "", 0, true},
		{"missing start", "", "2026-01-01", "", "", "", 0, true},
		{"mixed modes", "2026-01-01", "2026-01-31", "30", "", "", 0, true},
		{"invalid day", "2026-02-29", "2026-03-01", "", "", "", 0, true},
		{"invalid format", "2026-1-1", "2026-01-31", "", "", "", 0, true},
		{"reversed", "2026-02-01", "2026-01-31", "", "", "", 0, true},
		{"future", "2026-09-29", "2026-09-30", "", "", "", 0, true},
		{"oversized range", "2024-01-01", "2025-01-01", "", "", "", 0, true},
		{"invalid days", "", "", "abc", "", "", 0, true},
		{"zero days", "", "", "0", "", "", 0, true},
		{"negative days", "", "", "-1", "", "", 0, true},
		{"oversized days", "", "", "367", "", "", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParsePaymentDashboardRange(tc.start, tc.end, tc.days, now)
			if tc.wantError {
				if err == nil {
					t.Fatal("expected validation error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Start.Format(time.DateOnly) != tc.wantStart || got.End.Format(time.DateOnly) != tc.wantEnd || got.Days != tc.wantDays {
				t.Fatalf("unexpected range: %+v", got)
			}
			if got.Start.Location() != loc || got.End.Hour() != 0 {
				t.Fatal("range must use system-local midnight")
			}
		})
	}
}
