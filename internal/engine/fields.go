package engine

import (
	"slices"
	"time"
)

type fields struct{ Status, Due, Start, End string }

// normalizeFields applies the fixed field rules of built-in types and,
// when asked, extracts date phrases from text.
func normalizeFields(types []string, f fields, text string, parse bool, day string) (fields, string, error) {
	ref, _ := time.Parse(dateLayout, day)
	if slices.Contains(types, TypeTask) {
		if f.Status == "" {
			f.Status = StatusOpen
		}
		if f.Status != StatusOpen && f.Status != StatusDone {
			return f, text, invalid("task status is open or done")
		}
		if f.Due == "" && parse {
			if rest, date, _ := parseDates(text, ref, false); date != "" {
				f.Due, text = date, rest
			}
		}
		if f.Due != "" {
			if err := validDate(f.Due); err != nil {
				return f, text, err
			}
		}
	} else {
		f.Status, f.Due = "", ""
	}
	if slices.Contains(types, TypeMeeting) || slices.Contains(types, TypeEvent) {
		if f.Start == "" && parse {
			if rest, date, clock := parseDates(text, ref, true); date != "" || clock != "" {
				if date == "" {
					date = day
				}
				f.Start, text = date, rest
				if clock != "" {
					f.Start += "T" + clock
				}
			}
		}
		for _, m := range []string{f.Start, f.End} {
			if m != "" {
				if err := validMoment(m); err != nil {
					return f, text, err
				}
			}
		}
		if f.End != "" && (f.Start == "" || momentKey(f.End) < momentKey(f.Start)) {
			return f, text, invalid("end requires a start and cannot precede it")
		}
	} else {
		f.Start, f.End = "", ""
	}
	return f, text, nil
}

func momentKey(m string) string {
	if len(m) == len(dateLayout) {
		return m + "T00:00"
	}
	return m
}
