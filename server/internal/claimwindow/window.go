package claimwindow

import (
	"fmt"
	"strconv"
	"time"
)

const Duration = 5 * time.Hour
const DurationMinutes = 300

type State struct {
	Open      bool
	Start     time.Time
	End       time.Time
	NextStart time.Time
}

func ParseHHMM(raw string) (int, error) {
	if len(raw) != 5 || raw[2] != ':' {
		return 0, fmt.Errorf("time must use HH:MM format")
	}
	for _, index := range []int{0, 1, 3, 4} {
		if raw[index] < '0' || raw[index] > '9' {
			return 0, fmt.Errorf("time must use HH:MM format")
		}
	}

	hour, _ := strconv.Atoi(raw[:2])
	minute, _ := strconv.Atoi(raw[3:])
	if hour > 23 || minute > 59 {
		return 0, fmt.Errorf("time is outside the 24-hour clock")
	}
	return hour*60 + minute, nil
}

func FormatHHMM(minutes int) string {
	if minutes < 0 || minutes >= 24*60 {
		return ""
	}
	return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
}

func Evaluate(now time.Time, startMinutes int, timezone string) (State, error) {
	if startMinutes < 0 || startMinutes >= 24*60 {
		return State{}, fmt.Errorf("start minutes must be between 0 and 1439")
	}

	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return State{}, fmt.Errorf("load timezone %q: %w", timezone, err)
	}

	localNow := now.In(loc)
	year, month, day := localNow.Date()
	today := localDate{year: year, month: month, day: day}
	previous := today.addDays(-1)
	next := today.addDays(1)

	todayStart, err := resolveOccurrence(today, startMinutes, loc)
	if err != nil {
		return State{}, err
	}
	previousStart, err := resolveOccurrence(previous, startMinutes, loc)
	if err != nil {
		return State{}, err
	}
	nextStart, err := resolveOccurrence(next, startMinutes, loc)
	if err != nil {
		return State{}, err
	}

	start := todayStart
	upcoming := nextStart
	if todayStart.After(now) {
		start = previousStart
		upcoming = todayStart
	}
	end := start.Add(Duration)

	return State{
		Open:      !now.Before(start) && now.Before(end),
		Start:     start,
		End:       end,
		NextStart: upcoming,
	}, nil
}

type localDate struct {
	year  int
	month time.Month
	day   int
}

func (d localDate) addDays(days int) localDate {
	shifted := time.Date(d.year, d.month, d.day+days, 12, 0, 0, 0, time.UTC)
	year, month, day := shifted.Date()
	return localDate{year: year, month: month, day: day}
}

func resolveOccurrence(date localDate, startMinutes int, loc *time.Location) (time.Time, error) {
	// Scan real instants so DST gaps and repeated wall-clock minutes are handled
	// explicitly instead of relying on time.Date normalization.
	anchor := time.Date(date.year, date.month, date.day, 12, 0, 0, 0, time.UTC).Add(-36 * time.Hour)
	end := anchor.Add(72 * time.Hour)
	var firstLater time.Time

	for instant := anchor; !instant.After(end); instant = instant.Add(time.Minute) {
		local := instant.In(loc)
		year, month, day := local.Date()
		if year != date.year || month != date.month || day != date.day {
			continue
		}

		wallMinutes := local.Hour()*60 + local.Minute()
		if wallMinutes == startMinutes {
			return instant, nil
		}
		if wallMinutes > startMinutes && firstLater.IsZero() {
			firstLater = instant
		}
	}

	if !firstLater.IsZero() {
		return firstLater, nil
	}
	return time.Time{}, fmt.Errorf(
		"cannot resolve %04d-%02d-%02d %s in %s",
		date.year,
		date.month,
		date.day,
		FormatHHMM(startMinutes),
		loc,
	)
}
