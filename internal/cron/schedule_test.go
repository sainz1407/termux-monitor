package cron

import (
	"testing"
	"time"
)

// fixedZone pins the schedule math to a location with a known offset so next-run
// assertions do not depend on the machine's zone.
var fixedZone = time.FixedZone("TST", 0)

// at builds a time in the fixed test zone.
func at(y int, mo time.Month, d, h, mi int) time.Time {
	return time.Date(y, mo, d, h, mi, 0, 0, fixedZone)
}

func TestParseCronScheduleValid(t *testing.T) {
	cases := []string{
		"* * * * *",
		"0 0 * * *",
		"*/5 * * * *",
		"0 12 * * 1-5",
		"30 4 1,15 * 5",
		"0 0 1 jan *",
		"15 10 * * mon-fri",
		"0 0 * * 7",
		"0-30/10 8-18/2 * * *",
	}
	for _, c := range cases {
		if _, ok := ParseCronSchedule(c); !ok {
			t.Errorf("ParseCronSchedule(%q) = false, want true", c)
		}
	}
}

func TestParseCronScheduleInvalid(t *testing.T) {
	cases := []string{
		"",
		"* * * *",
		"* * * * * *",
		"60 * * * *",
		"* 24 * * *",
		"* * 0 * *",
		"* * 32 * *",
		"* * * 13 *",
		"* * * * 8",
		"@daily",
		"a * * * *",
		"*/0 * * * *",
		"5-1 * * * *",
		"1,,2 * * * *",
		"*/x * * * *",
	}
	for _, c := range cases {
		if _, ok := ParseCronSchedule(c); !ok {
			continue
		}
		t.Errorf("ParseCronSchedule(%q) = true, want false", c)
	}
}

func TestParseCronScheduleMonthAliases(t *testing.T) {
	sched, ok := ParseCronSchedule("0 0 1 JAN *")
	if !ok {
		t.Fatal("uppercase alias should parse")
	}
	if !sched.Month.Values[1] {
		t.Error("JAN should map to month 1")
	}
}

func TestParseCronScheduleDowSevenNormalised(t *testing.T) {
	sched, ok := ParseCronSchedule("0 0 * * 7")
	if !ok {
		t.Fatal("dow 7 should parse")
	}
	if !sched.Dow.Values[0] {
		t.Error("dow 7 should normalise to Sunday (0)")
	}
}

func TestNextRunEveryMinute(t *testing.T) {
	from := at(2026, time.March, 10, 8, 30)
	next, ok := NextRun("* * * * *", from)
	if !ok {
		t.Fatal("every-minute schedule should have a next run")
	}
	want := at(2026, time.March, 10, 8, 31)
	if !next.Equal(want) {
		t.Errorf("NextRun = %v, want %v", next, want)
	}
}

func TestNextRunIsStrictlyAfter(t *testing.T) {
	from := at(2026, time.March, 10, 8, 30)
	next, ok := NextRun("30 8 * * *", from)
	if !ok {
		t.Fatal("expected a next run")
	}
	// The current minute matches but NextRun must still advance a full day.
	want := at(2026, time.March, 11, 8, 30)
	if !next.Equal(want) {
		t.Errorf("NextRun = %v, want %v", next, want)
	}
}

func TestNextRunStep(t *testing.T) {
	from := at(2026, time.March, 10, 8, 31)
	next, ok := NextRun("*/15 * * * *", from)
	if !ok {
		t.Fatal("expected a next run")
	}
	want := at(2026, time.March, 10, 8, 45)
	if !next.Equal(want) {
		t.Errorf("NextRun = %v, want %v", next, want)
	}
}

func TestNextRunMonthlyAlias(t *testing.T) {
	from := at(2026, time.March, 10, 0, 0)
	next, ok := NextRun("0 0 1 apr *", from)
	if !ok {
		t.Fatal("expected a next run")
	}
	want := at(2026, time.April, 1, 0, 0)
	if !next.Equal(want) {
		t.Errorf("NextRun = %v, want %v", next, want)
	}
}

func TestNextRunDowRestricted(t *testing.T) {
	// 2026-03-10 is a Tuesday; the next Monday is 2026-03-16.
	from := at(2026, time.March, 10, 0, 0)
	next, ok := NextRun("0 9 * * mon", from)
	if !ok {
		t.Fatal("expected a next run")
	}
	want := at(2026, time.March, 16, 9, 0)
	if !next.Equal(want) {
		t.Errorf("NextRun = %v, want %v", next, want)
	}
}

func TestNextRunDomOrDow(t *testing.T) {
	// Both restricted: either field may match. From 2026-03-10 the next match is
	// 2026-03-15 (a Sunday) before the 1st of the next month.
	from := at(2026, time.March, 10, 0, 0)
	next, ok := NextRun("0 0 1 * sun", from)
	if !ok {
		t.Fatal("expected a next run")
	}
	want := at(2026, time.March, 15, 0, 0)
	if !next.Equal(want) {
		t.Errorf("NextRun = %v, want %v", next, want)
	}
}

func TestNextRunDomOnlyWhenDowStar(t *testing.T) {
	from := at(2026, time.March, 10, 0, 0)
	next, ok := NextRun("0 0 20 * *", from)
	if !ok {
		t.Fatal("expected a next run")
	}
	want := at(2026, time.March, 20, 0, 0)
	if !next.Equal(want) {
		t.Errorf("NextRun = %v, want %v", next, want)
	}
}

func TestNextRunImpossible(t *testing.T) {
	// 30 February never exists, so the search must exhaust its year cap.
	if next, ok := NextRun("0 0 30 2 *", at(2026, time.January, 1, 0, 0)); ok {
		t.Errorf("NextRun = %v, want no match", next)
	}
}

func TestNextRunInvalidSchedule(t *testing.T) {
	if _, ok := NextRun("not a schedule", time.Now()); ok {
		t.Error("invalid schedule should not resolve")
	}
}

func TestNextRunMsRoundsToMinute(t *testing.T) {
	// NextRunMs is zoned: it reads the wall clock in the process's local zone,
	// exactly like the JavaScript Date the reference panel uses. Build the
	// expectation in that same zone instead of assuming UTC.
	loc := time.Now().Location()
	from := time.Date(2026, time.March, 10, 8, 30, 45, 0, loc).UnixMilli()
	next, ok := NextRunMs("35 8 * * *", from)
	if !ok {
		t.Fatal("expected a next run")
	}
	want := time.Date(2026, time.March, 10, 8, 35, 0, 0, loc).UnixMilli()
	if next != want {
		t.Errorf("NextRunMs = %d, want %d", next, want)
	}
}
