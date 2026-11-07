package engine

import (
	"time"
)

// fields are the properties any node may carry, whatever its tags: a
// checkbox (Status: none, open or done) and a Date, a day or a day and
// time, with an optional End.
type fields struct{ Status, Date, End string }

// normalizeFields validates fields. A node tagged with a to-do tag gets an
// open checkbox unless its status is given. dateText is a phrase such as
// "Friday 14h", read relative to day, that sets the date when given.
func normalizeFields(todo bool, f fields, dateText, day string) (fields, error) {
	if f.Status == "" && todo {
		f.Status = StatusOpen
	}
	if f.Status != "" && f.Status != StatusOpen && f.Status != StatusDone {
		return f, invalid("status is open or done, or empty for no checkbox")
	}
	if dateText != "" {
		ref, _ := time.Parse(dateLayout, day)
		_, date, clock := parseDates(dateText, ref, true)
		if date == "" && clock == "" {
			return f, invalid("date_text %q is not a date phrase such as Friday, tomorrow 14h or 2026-11-12", dateText)
		}
		if date == "" {
			date = day
		}
		f.Date = date
		if clock != "" {
			f.Date += "T" + clock
		}
	}
	for _, m := range []string{f.Date, f.End} {
		if m != "" {
			if err := validMoment(m); err != nil {
				return f, err
			}
		}
	}
	if f.End != "" && (f.Date == "" || momentKey(f.End) < momentKey(f.Date)) {
		return f, invalid("end requires a date and cannot precede it")
	}
	return f, nil
}

func momentKey(m string) string {
	if len(m) == len(dateLayout) {
		return m + "T00:00"
	}
	return m
}
