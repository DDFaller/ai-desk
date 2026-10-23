package routines

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const maxCronSearchMinutes = 525600 // 365 days of minutes

type fieldSet struct {
	bits uint64
	star bool
}

func (f fieldSet) has(value int) bool {
	if value < 0 || value > 63 {
		return false
	}
	return f.bits&(1<<uint(value)) != 0
}

// CronSchedule represents a parsed 5-field cron schedule.
type CronSchedule struct {
	Minute   fieldSet
	Hour     fieldSet
	Day      fieldSet
	Month    fieldSet
	Weekday  fieldSet
	Location *time.Location
	RawSpec  string
}

// NormalizeCronSpec converts aliases like @daily, @hourly or interval expressions to 5-field cron.
func NormalizeCronSpec(spec string) string {
	s := strings.TrimSpace(spec)
	lower := strings.ToLower(s)
	switch lower {
	case "@yearly", "@annually":
		return "0 0 1 1 *"
	case "@monthly":
		return "0 0 1 * *"
	case "@weekly":
		return "0 0 * * 0"
	case "@daily", "@midnight":
		return "0 0 * * *"
	case "@hourly":
		return "0 * * * *"
	}

	// Handle "every 15m", "every 2h"
	if strings.HasPrefix(lower, "every ") {
		cadence := strings.TrimPrefix(lower, "every ")
		cadence = strings.TrimSpace(cadence)
		if strings.HasSuffix(cadence, "m") {
			mStr := strings.TrimSuffix(cadence, "m")
			if m, err := strconv.Atoi(mStr); err == nil && m > 0 && m <= 59 {
				return fmt.Sprintf("*/%d * * * *", m)
			}
		}
		if strings.HasSuffix(cadence, "h") {
			hStr := strings.TrimSuffix(cadence, "h")
			if h, err := strconv.Atoi(hStr); err == nil && h > 0 && h <= 23 {
				return fmt.Sprintf("0 */%d * * *", h)
			}
		}
	}

	return s
}

// ParseCron parses a five-field cron expression: minute hour day month weekday.
func ParseCron(spec string, loc *time.Location) (*CronSchedule, error) {
	if loc == nil {
		loc = time.Local
	}
	normSpec := NormalizeCronSpec(spec)
	fields := strings.Fields(normSpec)
	if len(fields) != 5 {
		return nil, fmt.Errorf("cron %q: want 5 fields, got %d", spec, len(fields))
	}

	minute, err := parseCronField(fields[0], 0, 59)
	if err != nil {
		return nil, fmt.Errorf("cron %q minute: %w", spec, err)
	}
	hour, err := parseCronField(fields[1], 0, 23)
	if err != nil {
		return nil, fmt.Errorf("cron %q hour: %w", spec, err)
	}
	day, err := parseCronField(fields[2], 1, 31)
	if err != nil {
		return nil, fmt.Errorf("cron %q day: %w", spec, err)
	}
	month, err := parseCronField(fields[3], 1, 12)
	if err != nil {
		return nil, fmt.Errorf("cron %q month: %w", spec, err)
	}
	weekday, err := parseCronField(fields[4], 0, 7)
	if err != nil {
		return nil, fmt.Errorf("cron %q weekday: %w", spec, err)
	}
	// Cron accepts both 0 and 7 for Sunday.
	if weekday.has(7) {
		weekday.bits |= 1
	}

	return &CronSchedule{
		Minute:   minute,
		Hour:     hour,
		Day:      day,
		Month:    month,
		Weekday:  weekday,
		Location: loc,
		RawSpec:  spec,
	}, nil
}

func parseCronField(field string, min, max int) (fieldSet, error) {
	var result fieldSet
	if field == "*" {
		result.star = true
		for v := min; v <= max; v++ {
			result.bits |= 1 << uint(v)
		}
		return result, nil
	}

	for _, part := range strings.Split(field, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return result, fmt.Errorf("empty element in %q", field)
		}

		step := 1
		rangeStr := part
		if strings.Contains(part, "/") {
			subParts := strings.SplitN(part, "/", 2)
			rangeStr = subParts[0]
			s, err := strconv.Atoi(subParts[1])
			if err != nil || s <= 0 {
				return result, fmt.Errorf("invalid step %q in %q", subParts[1], field)
			}
			step = s
		}

		start := min
		end := max

		if rangeStr != "*" {
			if strings.Contains(rangeStr, "-") {
				bounds := strings.SplitN(rangeStr, "-", 2)
				bStart, err1 := strconv.Atoi(bounds[0])
				bEnd, err2 := strconv.Atoi(bounds[1])
				if err1 != nil || err2 != nil {
					return result, fmt.Errorf("invalid range %q in %q", rangeStr, field)
				}
				if bStart < min || bEnd > max || bStart > bEnd {
					return result, fmt.Errorf("range %d-%d outside bounds %d-%d", bStart, bEnd, min, max)
				}
				start = bStart
				end = bEnd
			} else {
				val, err := strconv.Atoi(rangeStr)
				if err != nil {
					return result, fmt.Errorf("invalid number %q in %q", rangeStr, field)
				}
				if val < min || val > max {
					return result, fmt.Errorf("value %d outside bounds %d-%d", val, min, max)
				}
				start = val
				if !strings.Contains(part, "/") {
					end = val
				}
			}
		} else {
			result.star = true
		}

		for v := start; v <= end; v += step {
			result.bits |= 1 << uint(v)
		}
	}

	return result, nil
}

// Next returns the next time the schedule fires strictly after `after`.
func (c *CronSchedule) Next(after time.Time) (time.Time, bool) {
	if c == nil {
		return time.Time{}, false
	}
	t := after.In(c.Location).Truncate(time.Minute).Add(time.Minute)

	for i := 0; i < maxCronSearchMinutes; i++ {
		if c.matches(t) {
			return t, true
		}
		t = t.Add(time.Minute)
	}
	return time.Time{}, false
}

func (c *CronSchedule) matches(t time.Time) bool {
	if !c.Minute.has(t.Minute()) || !c.Hour.has(t.Hour()) || !c.Month.has(int(t.Month())) {
		return false
	}

	dayMatch := c.Day.has(t.Day())
	weekdayMatch := c.Weekday.has(int(t.Weekday()))

	switch {
	case c.Day.star && c.Weekday.star:
		return true
	case c.Day.star:
		return weekdayMatch
	case c.Weekday.star:
		return dayMatch
	default:
		return dayMatch || weekdayMatch
	}
}

// DescribeCron renders a schedule into human-friendly description.
func DescribeCron(spec string) string {
	norm := NormalizeCronSpec(spec)
	fields := strings.Fields(norm)
	if len(fields) != 5 {
		return spec
	}
	minute, hour, day, month, weekday := fields[0], fields[1], fields[2], fields[3], fields[4]

	if day == "*" && month == "*" && weekday == "*" {
		if strings.HasPrefix(minute, "*/") && hour == "*" {
			return "every " + strings.TrimPrefix(minute, "*/") + "m"
		}
		if hour == "*" && minute == "*" {
			return "every 1m"
		}
		if hour == "*" {
			if _, err := strconv.Atoi(minute); err == nil {
				return "hourly at :" + minute
			}
		}
		if minuteNum, err := strconv.Atoi(minute); err == nil {
			if hourNum, hourErr := strconv.Atoi(hour); hourErr == nil {
				return fmt.Sprintf("daily at %02d:%02d", hourNum, minuteNum)
			}
		}
	}
	return spec
}
