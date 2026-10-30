package local

import (
	"database/sql"
	"fmt"
	"math"
	"time"

	"github.com/TitouanSC/Things3-MCP/internal/domain"
)

// Encodings of the Things database, as documented by thingsapi/things.py
// (Apache-2.0) and checked against a real database (schema version 29).

// TMTask.type values.
const (
	typeTask    = 0
	typeProject = 1
	typeHeading = 2
)

// TMTask.start values.
const (
	startInbox   = 0
	startAnytime = 1
	startSomeday = 2
)

// decodeDate unpacks startDate/deadline: year<<16 | month<<12 | day<<7.
func decodeDate(v sql.NullInt64) domain.Date {
	if !v.Valid || v.Int64 == 0 {
		return domain.Date{}
	}
	return domain.Date{
		Year:  int(v.Int64 >> 16),
		Month: time.Month((v.Int64 >> 12) & 0xF),
		Day:   int((v.Int64 >> 7) & 0x1F),
	}
}

// encodeDate is the inverse of decodeDate (used by tests and fixtures).
func encodeDate(d domain.Date) int64 {
	return int64(d.Year)<<16 | int64(d.Month)<<12 | int64(d.Day)<<7
}

// decodeReminder unpacks reminderTime: hour<<26 | minute<<20.
func decodeReminder(v sql.NullInt64) string {
	if !v.Valid || v.Int64 == 0 {
		return ""
	}
	return fmt.Sprintf("%02d:%02d", (v.Int64>>26)&0x1F, (v.Int64>>20)&0x3F)
}

// decodeTime converts a Unix timestamp stored as REAL.
func decodeTime(v sql.NullFloat64) time.Time {
	if !v.Valid || v.Float64 == 0 {
		return time.Time{}
	}
	sec, frac := math.Modf(v.Float64)
	return time.Unix(int64(sec), int64(frac*1e9))
}

func decodeStatus(v int64) domain.Status {
	switch v {
	case 2:
		return domain.StatusCanceled
	case 3:
		return domain.StatusCompleted
	}
	return domain.StatusOpen
}

// decodeWhen rebuilds the scheduling bucket from start, startDate and
// startBucket (1 = This Evening), relative to today.
func decodeWhen(start int64, startDate sql.NullInt64, bucket int64, reminder sql.NullInt64, today domain.Date) domain.When {
	d := decodeDate(startDate)
	var w domain.When
	switch {
	case !d.IsZero() && d == today && bucket == 1:
		w.Kind = domain.WhenEvening
	case !d.IsZero() && d == today:
		w.Kind = domain.WhenToday
	case !d.IsZero():
		w = domain.When{Kind: domain.WhenDate, Date: d}
	case start == startSomeday:
		w.Kind = domain.WhenSomeday
	case start == startAnytime:
		w.Kind = domain.WhenAnytime
	default: // Inbox
		return domain.When{}
	}
	if w.Kind != domain.WhenSomeday && w.Kind != domain.WhenAnytime {
		w.Reminder = decodeReminder(reminder)
	}
	return w
}
