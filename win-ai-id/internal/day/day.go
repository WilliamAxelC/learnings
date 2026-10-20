// Package day holds the pure, clock-free rules of win.ai.id:
// what "today" is, which habits are due, and how streaks are counted.
package day

import (
	"fmt"
	"time"
)

// RolloverHour is the local hour at which a new day starts. Wins logged
// before this hour count for the previous day.
const RolloverHour = 4

// Layout is the canonical day key format stored in the database.
const Layout = "2006-01-02"

// Today returns the day key for instant now in loc, honoring RolloverHour.
func Today(now time.Time, loc *time.Location) string {
	return now.In(loc).Add(-RolloverHour * time.Hour).Format(Layout)
}

// Parse turns a day key into a midnight UTC time (only the date matters).
func Parse(key string) (time.Time, error) {
	return time.Parse(Layout, key)
}

// Add shifts a day key by n days.
func Add(key string, n int) string {
	t, err := Parse(key)
	if err != nil {
		return key
	}
	return t.AddDate(0, 0, n).Format(Layout)
}

// WeekKey returns the ISO year-week for a day key, e.g. "2026-W41".
func WeekKey(key string) string {
	t, err := Parse(key)
	if err != nil {
		return ""
	}
	y, w := t.ISOWeek()
	return fmt.Sprintf("%d-W%02d", y, w)
}

// WeekStart returns the Monday day key of the ISO week containing key.
func WeekStart(key string) string {
	t, err := Parse(key)
	if err != nil {
		return key
	}
	offset := (int(t.Weekday()) + 6) % 7 // Monday=0 ... Sunday=6
	return t.AddDate(0, 0, -offset).Format(Layout)
}

// ScheduleKind enumerates how a habit recurs.
type ScheduleKind string

const (
	Daily    ScheduleKind = "daily"
	Weekdays ScheduleKind = "weekdays" // specific days, via WeekdayMask
	PerWeek  ScheduleKind = "per_week" // N times per ISO week, any days
)

// Schedule describes when a habit is due.
type Schedule struct {
	Kind ScheduleKind
	// WeekdayMask: bit i set means time.Weekday(i) is a due day (Sunday=0).
	WeekdayMask int
	// PerWeek: target completions per ISO week.
	PerWeek int
}

// Due reports whether a habit should appear on day key.
// doneThisWeekBefore is the number of completions earlier in the same ISO
// week (excluding key itself); only used for PerWeek.
func (s Schedule) Due(key string, doneThisWeekBefore int) bool {
	t, err := Parse(key)
	if err != nil {
		return false
	}
	switch s.Kind {
	case Daily:
		return true
	case Weekdays:
		return s.WeekdayMask&(1<<uint(t.Weekday())) != 0
	case PerWeek:
		return doneThisWeekBefore < s.PerWeek
	default:
		return false
	}
}

// StreakResult is the outcome of Streak.
type StreakResult struct {
	Days        int      // consecutive won days (skipped days don't add)
	SkipsUsed   []string // day keys covered by a free skip
	TodayWon    bool
	SkipLeftNow bool // whether this ISO week still has its free skip
}

// maxLookback bounds the streak walk.
const maxLookback = 3660

// Streak counts the global "daily win" streak ending at today.
// won reports whether a day key has at least one completed win.
// An unwon today never breaks the streak (the day isn't over).
// Each ISO week grants one free skip that covers a single missed day.
func Streak(today string, won func(string) bool) StreakResult {
	res := StreakResult{TodayWon: won(today)}
	usedWeek := map[string]bool{}

	d := today
	if !res.TodayWon {
		d = Add(today, -1)
	}
	// A skip only counts if a won day exists further back; track pending
	// skips and commit them once we hit a win.
	var pending []string
	for i := 0; i < maxLookback; i++ {
		if won(d) {
			res.Days++
			res.SkipsUsed = append(res.SkipsUsed, pending...)
			pending = nil
		} else {
			wk := WeekKey(d)
			if usedWeek[wk] {
				break
			}
			usedWeek[wk] = true
			pending = append(pending, d)
		}
		d = Add(d, -1)
	}
	committed := map[string]bool{}
	for _, s := range res.SkipsUsed {
		committed[WeekKey(s)] = true
	}
	res.SkipLeftNow = !committed[WeekKey(today)]
	return res
}

// HabitStreak counts consecutive completions for a habit ending at today.
// done reports whether this habit was completed on day key.
func HabitStreak(today string, sc Schedule, done func(string) bool) int {
	if sc.Kind == PerWeek {
		streakWeeks := 0
		curWeekStart := WeekStart(today)
		countThisWeek := 0
		for i := 0; i < 7; i++ {
			dayInCurWeek := Add(curWeekStart, i)
			if dayInCurWeek > today {
				break
			}
			if done(dayInCurWeek) {
				countThisWeek++
			}
		}
		if countThisWeek >= sc.PerWeek {
			streakWeeks++
		}
		wStart := Add(curWeekStart, -7)
		for i := 0; i < 520; i++ {
			count := 0
			for j := 0; j < 7; j++ {
				if done(Add(wStart, j)) {
					count++
				}
			}
			if count >= sc.PerWeek {
				streakWeeks++
				wStart = Add(wStart, -7)
			} else {
				break
			}
		}
		return streakWeeks
	}

	streak := 0
	d := today
	t, _ := Parse(today)
	isDueToday := sc.Kind == Daily || (sc.WeekdayMask&(1<<uint(t.Weekday())) != 0)
	if isDueToday && !done(today) {
		d = Add(today, -1)
	}

	for i := 0; i < maxLookback; i++ {
		pt, err := Parse(d)
		if err != nil {
			break
		}
		isDue := sc.Kind == Daily || (sc.WeekdayMask&(1<<uint(pt.Weekday())) != 0)
		if isDue {
			if done(d) {
				streak++
			} else {
				break
			}
		}
		d = Add(d, -1)
	}
	return streak
}

