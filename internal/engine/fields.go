package engine

import (
	"time"
)

// fields are the properties of a node besides its text and tags: the
// checkbox (Status: open or done) of a to-do, and a Date, a day or a day
// and time, with an optional End, which any node may carry.
type fields struct{ Status, Date, End string }

// normalizeFields validates fields. The checkbox is the to-do tag: a node
// carrying one is open unless its status says done, a node without one
// has no status. dateText is a phrase such as "Friday 14h", read relative
// to day, that sets the date when given.
func normalizeFields(todo bool, f fields, dateText, day string) (fields, error) {
	switch {
	case !todo && f.Status != "":
		return f, invalid("a checkbox needs a to-do tag such as #task; write the tag in the text")
	case !todo:
	case f.Status == "":
		f.Status = StatusOpen
	case f.Status != StatusOpen && f.Status != StatusDone:
		return f, invalid("status is open or done")
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
