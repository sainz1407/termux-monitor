// Package cron implements the Cron tab: crontab management for the Termux user
// plus a fallback in-process scheduler when cronie is unavailable.
//
// The schedule math mirrors the OpenLocalPanel reference implementation
// (pure 5-field parsing, no external dependencies) so the two panels agree on
// what a given expression means.
package cron

import (
	"strconv"
	"strings"
	"time"
)

// MonthNames maps the 3-letter month alias to its numeric value.
var MonthNames = map[string]int{
	"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6,
	"jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12,
}

// DowNames maps the 3-letter weekday alias to its numeric value. Sunday is 0
// and the numeric form 7 is normalised to 0 while parsing.
var DowNames = map[string]int{
	"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6,
}

// CronField is one parsed cron field: the closed value set plus whether the
// field was a bare "*". The star flag matters because the day-of-month and
// day-of-week fields combine with an OR rule only when both are restricted.
type CronField struct {
	Values map[int]bool
	Star   bool
}

// tokenValue resolves a single token to a number, honouring the alias table.
func tokenValue(token string, names map[string]int) (int, bool) {
	lower := strings.ToLower(token)
	if names != nil {
		if v, ok := names[lower]; ok {
			return v, true
		}
	}
	v, err := strconv.Atoi(token)
	if err != nil {
		return 0, false
	}
	return v, true
}

// parseCronField parses one field into its value set. It returns false when the
// field is malformed or any value falls outside [min, max]. The numeric form 7
// is accepted in the day-of-week field and normalised to 0.
func parseCronField(field string, min, max int, names map[string]int) (CronField, bool) {
	if field == "*" {
		values := make(map[int]bool, max-min+1)
		for v := min; v <= max; v++ {
			values[v] = true
		}
		return CronField{Values: values, Star: true}, true
	}
	values := map[int]bool{}
	for _, part := range strings.Split(field, ",") {
		if part == "" {
			return CronField{}, false
		}
		rangePart, stepRaw, hasStep := strings.Cut(part, "/")
		step := 1
		if hasStep {
			n, err := strconv.Atoi(stepRaw)
			if err != nil || n < 1 {
				return CronField{}, false
			}
			step = n
		}
		var lo, hi int
		switch {
		case rangePart == "*":
			lo, hi = min, max
		case strings.Contains(rangePart, "-"):
			a, b, ok := strings.Cut(rangePart, "-")
			if !ok {
				return CronField{}, false
			}
			aVal, aOK := tokenValue(a, names)
			bVal, bOK := tokenValue(b, names)
			if !aOK || !bOK || aVal > bVal {
				return CronField{}, false
			}
			lo, hi = aVal, bVal
		default:
			single, ok := tokenValue(rangePart, names)
			if !ok {
				return CronField{}, false
			}
			lo, hi = single, single
		}
		if lo < min || hi > max {
			return CronField{}, false
		}
		dow := isDowNames(names)
		// Walk the range with a dedicated counter: the day-of-week field folds
		// 7 onto 0, and mutating the loop variable there would never advance.
		for v := lo; v <= hi; v += step {
			values[normalizeDow(dow, v)] = true
		}
	}
	if len(values) == 0 {
		return CronField{}, false
	}
	return CronField{Values: values, Star: false}, true
}

// isDowNames reports whether names is the day-of-week alias table. The tables
// are compared by identity of a sentinel entry so no extra parameter is needed
// by the parser signature.
func isDowNames(names map[string]int) bool {
	v, ok := names["sun"]
	return ok && v == 0
}

// normalizeDow folds the non-standard value 7 onto Sunday (0) for the
// day-of-week field only; all other fields pass the value through unchanged.
func normalizeDow(dow bool, v int) int {
	if dow && v == 7 {
		return 0
	}
	return v
}

// CronSchedule is a parsed 5-field schedule.
type CronSchedule struct {
	Minute CronField
	Hour   CronField
	Dom    CronField
	Month  CronField
	Dow    CronField
}

// ParseCronSchedule parses "min hour dom month dow" (exactly 5 whitespace
// separated parts). It returns false when any field is malformed.
func ParseCronSchedule(schedule string) (CronSchedule, bool) {
	parts := strings.Fields(schedule)
	if len(parts) != 5 {
		return CronSchedule{}, false
	}
	specs := []struct {
		min, max int
		names    map[string]int
	}{
		{0, 59, nil},
		{0, 23, nil},
		{1, 31, nil},
		{1, 12, MonthNames},
		{0, 7, DowNames},
	}
	fields := make([]CronField, 5)
	for i, spec := range specs {
		f, ok := parseCronField(parts[i], spec.min, spec.max, spec.names)
		if !ok {
			return CronSchedule{}, false
		}
		fields[i] = f
	}
	return CronSchedule{
		Minute: fields[0], Hour: fields[1], Dom: fields[2], Month: fields[3], Dow: fields[4],
	}, true
}

// yearCapMinutes bounds the forward search for the next fire time.
const yearCapMinutes = 366 * 24 * 60

// NextRun returns the next fire time strictly after from, or the zero time when
// the schedule is unparseable or nothing fires within a year. The day-of-month
// versus day-of-week combination follows the standard OR rule: when one of the
// two is "*" the other decides, and when both are restricted either may match.
//
// All arithmetic is done in the time.Time location, which lets tests pin the
// evaluation to a fixed zone.
func NextRun(schedule string, from time.Time) (time.Time, bool) {
	parsed, ok := ParseCronSchedule(schedule)
	if !ok {
		return time.Time{}, false
	}
	loc := from.Location()
	// Align to the next whole minute after from.
	cursor := from.Truncate(time.Minute).Add(time.Minute)
	for i := 0; i < yearCapMinutes; i++ {
		local := cursor.In(loc)
		if parsed.Minute.Values[local.Minute()] &&
			parsed.Hour.Values[local.Hour()] &&
			parsed.Month.Values[int(local.Month())] {
			domMatch := parsed.Dom.Values[local.Day()]
			dowMatch := parsed.Dow.Values[int(local.Weekday())]
			var dayMatch bool
			switch {
			case parsed.Dom.Star && parsed.Dow.Star:
				dayMatch = true
			case parsed.Dom.Star:
				dayMatch = dowMatch
			case parsed.Dow.Star:
				dayMatch = domMatch
			default:
				dayMatch = domMatch || dowMatch
			}
			if dayMatch {
				return cursor, true
			}
		}
		cursor = cursor.Add(time.Minute)
	}
	return time.Time{}, false
}

// NextRunMs is the millisecond form of NextRun. It returns false when no fire
// time exists within a year, which the API surfaces as a JSON null.
func NextRunMs(schedule string, fromMs int64) (int64, bool) {
	from := time.UnixMilli(fromMs)
	next, ok := NextRun(schedule, from)
	if !ok {
		return 0, false
	}
	return next.UnixMilli(), true
}

// timeNowMillis is the wall clock in milliseconds, matching the JavaScript
// Date.now() values the OpenLocalPanel records so exported histories stay
// comparable.
func timeNowMillis() int64 {
	return time.Now().UnixMilli()
}

// TimeNowMillis exposes the millisecond clock to sibling handler packages that
// must compute next-run values without importing time-sensitive helpers twice.
func TimeNowMillis() int64 {
	return timeNowMillis()
}
