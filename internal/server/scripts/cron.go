// SPDX-License-Identifier: Apache-2.0

package scripts

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ErrCron means the schedule isn't a cron expression Sentinel understands.
var ErrCron = errors.New("not a valid cron schedule")

// Cron is a parsed five-field cron schedule: minute, hour, day of month,
// month and day of week, each a set of the values it matches.
type Cron struct {
	minutes, hours, days, months, weekdays uint64
	// anyDay and anyWeekday record a field given as "*": cron matches a
	// day by its day of month OR its weekday only when both are limited.
	anyDay, anyWeekday bool
}

// field is one of the five fields: its range, and names for its values.
type field struct {
	name     string
	min, max int
	names    []string
}

func fields() []field {
	return []field{
		{name: "minute", min: 0, max: 59},
		{name: "hour", min: 0, max: 23},
		{name: "day of month", min: 1, max: 31},
		{name: "month", min: 1, max: 12, names: []string{"jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec"}},
		{name: "day of week", min: 0, max: 7, names: []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}},
	}
}

// macros are the shorthand schedules.
func macros() map[string]string {
	return map[string]string{
		"@hourly": "0 * * * *", "@daily": "0 0 * * *", "@midnight": "0 0 * * *",
		"@weekly": "0 0 * * 0", "@monthly": "0 0 1 * *", "@yearly": "0 0 1 1 *", "@annually": "0 0 1 1 *",
	}
}

// ParseCron reads a cron schedule: five fields, each "*", a value, a
// range "a-b", a step "*/n" or "a-b/n", or a list of these separated by
// commas; months and weekdays can be named ("jan", "mon"); 7 is Sunday too.
func ParseCron(text string) (Cron, error) {
	text = strings.TrimSpace(strings.ToLower(text))
	if expanded, ok := macros()[text]; ok {
		text = expanded
	}
	parts := strings.Fields(text)
	if len(parts) != 5 {
		return Cron{}, fmt.Errorf("%w: expected 5 fields, got %d", ErrCron, len(parts))
	}
	var sets [5]uint64
	for i, spec := range fields() {
		set, err := parseField(parts[i], spec)
		if err != nil {
			return Cron{}, err
		}
		sets[i] = set
	}
	if sets[4]&(1<<7) != 0 {
		sets[4] |= 1 // 7 is Sunday, as 0 is
	}
	return Cron{
		minutes: sets[0], hours: sets[1], days: sets[2], months: sets[3], weekdays: sets[4],
		anyDay: parts[2] == "*", anyWeekday: parts[4] == "*",
	}, nil
}

func parseField(text string, spec field) (uint64, error) {
	var set uint64
	for item := range strings.SplitSeq(text, ",") {
		bits, err := parseItem(item, spec)
		if err != nil {
			return 0, fmt.Errorf("%w: %s %q: %w", ErrCron, spec.name, item, err)
		}
		set |= bits
	}
	return set, nil
}

// parseItem reads "*", "n", "a-b", with an optional "/step".
func parseItem(item string, spec field) (uint64, error) {
	rangePart, stepPart, hasStep := strings.Cut(item, "/")
	step := 1
	if hasStep {
		var err error
		if step, err = strconv.Atoi(stepPart); err != nil || step < 1 || step > spec.max {
			return 0, errors.New("the step must be a number from 1")
		}
	}
	low, high := spec.min, spec.max
	if rangePart != "*" {
		var err error
		if low, high, err = parseRange(rangePart, hasStep, spec); err != nil {
			return 0, err
		}
	}
	var set uint64
	for v := low; v <= high; v += step {
		set |= 1 << v
	}
	return set, nil
}

// parseRange reads "n" or "a-b". With a step and no range, "n/step" runs
// from n to the field's end.
func parseRange(text string, hasStep bool, spec field) (int, int, error) {
	first, last, isRange := strings.Cut(text, "-")
	low, err := value(first, spec)
	if err != nil {
		return 0, 0, err
	}
	high := low
	switch {
	case isRange:
		if high, err = value(last, spec); err != nil {
			return 0, 0, err
		}
	case hasStep:
		high = spec.max
	}
	if high < low {
		return 0, 0, errors.New("a range must go from low to high")
	}
	return low, high, nil
}

func value(text string, spec field) (int, error) {
	for i, name := range spec.names {
		if text == name {
			return i + spec.min, nil
		}
	}
	number, err := strconv.Atoi(text)
	if err != nil || number < spec.min || number > spec.max {
		return 0, fmt.Errorf("must be from %d to %d", spec.min, spec.max)
	}
	return number, nil
}

// Next is the first time after after that the schedule matches, in loc,
// to the minute; zero if there is none within five years (February 30th).
func (c Cron) Next(after time.Time, loc *time.Location) time.Time {
	t := after.In(loc).Truncate(time.Minute).Add(time.Minute)
	limit := t.AddDate(5, 0, 0)
	for t.Before(limit) {
		switch {
		case c.months&(1<<int(t.Month())) == 0:
			t = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, loc)
		case !c.dayMatches(t):
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, loc)
		case c.hours&(1<<t.Hour()) == 0:
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, loc)
		case c.minutes&(1<<t.Minute()) == 0:
			t = t.Add(time.Minute)
		default:
			return t
		}
	}
	return time.Time{}
}

// dayMatches applies cron's day rule: with both day fields limited, a day
// matching either one counts.
func (c Cron) dayMatches(t time.Time) bool {
	day := c.days&(1<<t.Day()) != 0
	weekday := c.weekdays&(1<<int(t.Weekday())) != 0
	switch {
	case c.anyDay && c.anyWeekday:
		return true
	case c.anyDay:
		return weekday
	case c.anyWeekday:
		return day
	}
	return day || weekday
}
