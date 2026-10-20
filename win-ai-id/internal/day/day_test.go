package day

import (
	"testing"
	"time"
)

func TestTodayRollover(t *testing.T) {
	jkt, _ := time.LoadLocation("Asia/Jakarta")
	cases := []struct {
		local string
		want  string
	}{
		{"2026-10-07 03:59", "2026-10-06"},
		{"2026-10-07 04:00", "2026-10-07"},
		{"2026-10-07 23:30", "2026-10-07"},
		{"2026-10-08 01:00", "2026-10-07"},
	}
	for _, c := range cases {
		ts, _ := time.ParseInLocation("2006-01-02 15:04", c.local, jkt)
		if got := Today(ts.UTC(), jkt); got != c.want {
			t.Errorf("Today(%s) = %s, want %s", c.local, got, c.want)
		}
	}
}

func TestScheduleDue(t *testing.T) {
	// 2026-10-07 is a Wednesday.
	wed := "2026-10-07"
	mwf := Schedule{Kind: Weekdays, WeekdayMask: 1<<time.Monday | 1<<time.Wednesday | 1<<time.Friday}
	if !mwf.Due(wed, 0) {
		t.Error("MWF habit should be due on Wednesday")
	}
	if mwf.Due("2026-10-08", 0) {
		t.Error("MWF habit should not be due on Thursday")
	}
	three := Schedule{Kind: PerWeek, PerWeek: 3}
	if !three.Due(wed, 2) || three.Due(wed, 3) {
		t.Error("PerWeek due logic wrong")
	}
	if !(Schedule{Kind: Daily}).Due(wed, 0) {
		t.Error("daily always due")
	}
}

func TestWeekStart(t *testing.T) {
	if got := WeekStart("2026-10-11"); got != "2026-10-05" { // Sunday -> Monday
		t.Errorf("WeekStart(Sun) = %s", got)
	}
	if got := WeekStart("2026-10-05"); got != "2026-10-05" {
		t.Errorf("WeekStart(Mon) = %s", got)
	}
}

func set(days ...string) func(string) bool {
	m := map[string]bool{}
	for _, d := range days {
		m[d] = true
	}
	return func(k string) bool { return m[k] }
}

func TestStreak(t *testing.T) {
	today := "2026-10-07" // Wed, ISO week 41 (Mon 10-05)

	t.Run("today not yet won does not break", func(t *testing.T) {
		r := Streak(today, set("2026-10-06", "2026-10-05"))
		if r.Days != 2 || r.TodayWon {
			t.Fatalf("got %+v", r)
		}
	})

	t.Run("single miss covered by weekly skip", func(t *testing.T) {
		r := Streak(today, set(today, "2026-10-05", "2026-10-04"))
		if r.Days != 3 || len(r.SkipsUsed) != 1 || r.SkipsUsed[0] != "2026-10-06" {
			t.Fatalf("got %+v", r)
		}
		if r.SkipLeftNow {
			t.Fatal("this week's skip should be used")
		}
	})

	t.Run("two misses in one week break", func(t *testing.T) {
		r := Streak(today, set(today, "2026-10-05"))
		// 10-06 skipped (wk41), 10-05 won, 10-04 is wk40 -> skip pending, 10-03 miss wk40 -> break
		if r.Days != 2 {
			t.Fatalf("got %+v", r)
		}
		r = Streak(today, set(today))
		// 10-06 skip pending, 10-05 miss same week -> break; pending not committed
		if r.Days != 1 || len(r.SkipsUsed) != 0 || !r.SkipLeftNow {
			t.Fatalf("got %+v", r)
		}
	})

	t.Run("empty history", func(t *testing.T) {
		if r := Streak(today, set()); r.Days != 0 {
			t.Fatalf("got %+v", r)
		}
	})
}

func TestHabitStreak(t *testing.T) {
	today := "2026-10-07" // Wednesday

	t.Run("daily habit", func(t *testing.T) {
		sc := Schedule{Kind: Daily}
		// Done today, yesterday, and day before = 3
		s := HabitStreak(today, sc, set(today, "2026-10-06", "2026-10-05"))
		if s != 3 {
			t.Fatalf("expected streak 3, got %d", s)
		}
		// Not done today, but done yesterday and day before = 2 (today doesn't break)
		s = HabitStreak(today, sc, set("2026-10-06", "2026-10-05"))
		if s != 2 {
			t.Fatalf("expected streak 2, got %d", s)
		}
	})

	t.Run("weekdays habit (Mon/Wed/Fri)", func(t *testing.T) {
		// 10-07 is Wed, 10-05 is Mon, 10-02 is Fri
		sc := Schedule{Kind: Weekdays, WeekdayMask: 1<<time.Monday | 1<<time.Wednesday | 1<<time.Friday}
		s := HabitStreak(today, sc, set(today, "2026-10-05", "2026-10-02"))
		if s != 3 {
			t.Fatalf("expected weekday streak 3, got %d", s)
		}
	})
}

