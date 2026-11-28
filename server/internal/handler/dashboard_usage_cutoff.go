package handler

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// usageHourCutoff turns a dashboard cutoff into the task_usage.created_at bound
// the hourly rollups effectively apply: they keep a bucket when bucket_hour >=
// cutoff, which for a usage row means created_at >= the cutoff rounded up to
// the hour. Viewing time zones with a non-whole-hour offset put the cutoff
// mid-hour.
func usageHourCutoff(since pgtype.Timestamptz) pgtype.Timestamptz {
	t := since.Time.Truncate(time.Hour)
	if t.Before(since.Time) {
		t = t.Add(time.Hour)
	}
	return pgtype.Timestamptz{Time: t, Valid: since.Valid}
}
