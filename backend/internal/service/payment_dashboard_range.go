package service

import (
	"fmt"
	"strconv"
	"time"
)

// Calendar ranges include both selected dates; SQL uses an exclusive next-day end.
const PaymentDashboardMaxDays = 366

type PaymentDashboardRange struct {
	Start time.Time
	End   time.Time
	Days  int
}

func ParsePaymentDashboardRange(startDate, endDate, daysValue string, now time.Time) (PaymentDashboardRange, error) {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	var start, end time.Time
	if startDate != "" || endDate != "" {
		if startDate == "" || endDate == "" || daysValue != "" {
			return PaymentDashboardRange{}, fmt.Errorf("provide start_date and end_date together, without days")
		}
		var err error
		start, err = time.ParseInLocation(time.DateOnly, startDate, now.Location())
		if err != nil {
			return PaymentDashboardRange{}, fmt.Errorf("start_date must be a valid YYYY-MM-DD date")
		}
		end, err = time.ParseInLocation(time.DateOnly, endDate, now.Location())
		if err != nil {
			return PaymentDashboardRange{}, fmt.Errorf("end_date must be a valid YYYY-MM-DD date")
		}
	} else {
		days := 30
		if daysValue != "" {
			value, err := strconv.Atoi(daysValue)
			if err != nil || value < 1 || value > PaymentDashboardMaxDays {
				return PaymentDashboardRange{}, fmt.Errorf("days must be between 1 and %d", PaymentDashboardMaxDays)
			}
			days = value
		}
		end = today
		start = today.AddDate(0, 0, 1-days)
	}
	if start.After(end) || end.After(today) {
		return PaymentDashboardRange{}, fmt.Errorf("date range must be ordered and cannot include future dates")
	}
	if end.After(start.AddDate(0, 0, PaymentDashboardMaxDays-1)) {
		return PaymentDashboardRange{}, fmt.Errorf("date range cannot exceed %d days", PaymentDashboardMaxDays)
	}
	// Count calendar dates, not 24-hour durations (DST days can be 23 or 25 hours).
	days := 0
	for day := start; !day.After(end); day = day.AddDate(0, 0, 1) {
		days++
	}
	return PaymentDashboardRange{Start: start, End: end.AddDate(0, 0, 1), Days: days}, nil
}
