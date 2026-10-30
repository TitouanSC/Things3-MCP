package domain

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

// Date is a calendar date without time zone, as Things stores start dates
// and deadlines. The zero value means "no date".
type Date struct {
	Year  int
	Month time.Month
	Day   int
}

// DateOf returns the calendar date of t in t's location.
func DateOf(t time.Time) Date {
	y, m, d := t.Date()
	return Date{Year: y, Month: m, Day: d}
}

// ParseDate parses a strict YYYY-MM-DD date.
func ParseDate(s string) (Date, error) {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return Date{}, fmt.Errorf("expected YYYY-MM-DD, got %q", s)
	}
	return DateOf(t), nil
}

// IsZero reports whether d is the "no date" value.
func (d Date) IsZero() bool { return d == Date{} }

// String formats d as YYYY-MM-DD, or "" for the zero value.
func (d Date) String() string {
	if d.IsZero() {
		return ""
	}
	return fmt.Sprintf("%04d-%02d-%02d", d.Year, d.Month, d.Day)
}

// Before reports whether d is strictly before other.
func (d Date) Before(other Date) bool { return d.time().Before(other.time()) }

// AddDays returns d shifted by n days.
func (d Date) AddDays(n int) Date { return DateOf(d.time().AddDate(0, 0, n)) }

func (d Date) time() time.Time { return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC) }

// WhenKind is the scheduling bucket of a task or project.
type WhenKind string

// Scheduling buckets. WhenNone means "not scheduled" (Inbox when the item has
// no project or area, Anytime otherwise).
const (
	WhenNone    WhenKind = ""
	WhenToday   WhenKind = "today"
	WhenEvening WhenKind = "evening"
	WhenAnytime WhenKind = "anytime"
	WhenSomeday WhenKind = "someday"
	WhenDate    WhenKind = "date"
)

// When is the "when" field of Things. A Reminder ("HH:MM", 24h) may only be
// set together with a date-like kind.
type When struct {
	Kind     WhenKind
	Date     Date   // set when Kind == WhenDate
	Reminder string // optional "HH:MM"
}

// IsZero reports whether w is unscheduled.
func (w When) IsZero() bool { return w == When{} }

// String formats w the way ParseWhen accepts it.
func (w When) String() string {
	var s string
	switch w.Kind {
	case WhenDate:
		s = w.Date.String()
	default:
		s = string(w.Kind)
	}
	if w.Reminder != "" {
		s += "@" + w.Reminder
	}
	return s
}

var whenRe = regexp.MustCompile(`^([a-z]+|\d{4}-\d{2}-\d{2})(?:@(\d{1,2}):(\d{2}))?$`)

// ParseWhen parses the accepted "when" values:
// today | evening | tomorrow | anytime | someday | YYYY-MM-DD, each optionally
// followed by "@HH:MM" to add a reminder (not allowed with anytime/someday).
// "tomorrow" is resolved against today. Natural-language dates are rejected on
// purpose: parsing must be deterministic and identical on every backend.
func ParseWhen(s string, today Date) (When, error) {
	m := whenRe.FindStringSubmatch(s)
	if m == nil {
		return When{}, Invalid("when", "expected today, evening, tomorrow, anytime, someday or YYYY-MM-DD, optionally followed by @HH:MM; got %q", s)
	}
	var w When
	switch m[1] {
	case "today":
		w.Kind = WhenToday
	case "evening":
		w.Kind = WhenEvening
	case "tomorrow":
		w = When{Kind: WhenDate, Date: today.AddDays(1)}
	case "anytime":
		w.Kind = WhenAnytime
	case "someday":
		w.Kind = WhenSomeday
	default:
		d, err := ParseDate(m[1])
		if err != nil {
			return When{}, Invalid("when", "%v", err)
		}
		w = When{Kind: WhenDate, Date: d}
	}
	if m[2] != "" {
		if w.Kind == WhenAnytime || w.Kind == WhenSomeday {
			return When{}, Invalid("when", "a reminder time cannot be set on %q", w.Kind)
		}
		h, _ := strconv.Atoi(m[2]) // digits guaranteed by whenRe
		min, _ := strconv.Atoi(m[3])
		if h > 23 || min > 59 {
			return When{}, Invalid("when", "invalid reminder time %s:%s", m[2], m[3])
		}
		w.Reminder = fmt.Sprintf("%02d:%02d", h, min)
	}
	return w, nil
}

// ParseDeadline parses a deadline, which is always a plain YYYY-MM-DD date.
func ParseDeadline(s string) (Date, error) {
	d, err := ParseDate(s)
	if err != nil {
		return Date{}, Invalid("deadline", "%v", err)
	}
	return d, nil
}
