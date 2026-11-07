package engine

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	maxTextLength = 10000
	maxTypeLength = 40
)

// refToken is a reference found in input. Name is empty when ID is set.
// Type is set by the #type/Name shortcut.
type refToken struct {
	ID   string
	Name string
	Type string
}

// parsedInput is input split into canonical text, types and references.
// Text keeps #type tokens in place and contains placeholders \x00N\x00 for
// refs[N].
type parsedInput struct {
	Text  string
	Types []string
	Refs  []refToken
}

// parseInput splits raw Vrac syntax. It never touches the database.
func parseInput(raw string) (parsedInput, error) {
	var p parsedInput
	if !utf8.ValidString(raw) {
		return p, invalid("text is not valid UTF-8")
	}
	if strings.ContainsRune(raw, 0) {
		return p, invalid("text contains a NUL character")
	}
	raw = strings.ReplaceAll(raw, "\n", " ")
	var b strings.Builder
	seenType := map[string]bool{}
	addRef := func(t refToken) {
		fmt.Fprintf(&b, "\x00%d\x00", len(p.Refs))
		p.Refs = append(p.Refs, t)
	}
	for i := 0; i < len(raw); {
		if strings.HasPrefix(raw[i:], "[[") {
			end := strings.Index(raw[i+2:], "]]")
			if end > 0 {
				inner := strings.TrimSpace(raw[i+2 : i+2+end])
				if inner != "" {
					// [[Name|ID]] is the readable form of a canonical reference.
					if bar := strings.LastIndexByte(inner, '|'); bar >= 0 && isID(strings.TrimSpace(inner[bar+1:])) {
						addRef(refToken{ID: strings.TrimSpace(inner[bar+1:])})
					} else if isID(inner) {
						addRef(refToken{ID: inner})
					} else {
						addRef(refToken{Name: inner})
					}
					i += end + 4
					continue
				}
			}
		}
		if raw[i] == '#' && (i == 0 || boundaryBefore(raw[:i])) {
			word, n := tagWord(raw[i+1:])
			if word != "" {
				typ, name, shortcut := strings.Cut(word, "/")
				typ = strings.ToLower(typ)
				if err := validTypeName(typ); err != nil {
					return p, err
				}
				if shortcut && name != "" {
					addRef(refToken{Name: name, Type: typ})
				} else {
					// The type stays where it was written, so a sentence
					// such as "#meeting with Alice" reads naturally.
					b.WriteString("#" + typ)
					if !seenType[typ] {
						seenType[typ] = true
						p.Types = append(p.Types, typ)
					}
				}
				i += 1 + n
				continue
			}
		}
		b.WriteByte(raw[i])
		i++
	}
	p.Text = strings.Join(strings.Fields(b.String()), " ")
	if len(p.Text) > maxTextLength {
		return p, invalid("text exceeds %d bytes", maxTextLength)
	}
	return p, nil
}

func boundaryBefore(s string) bool {
	r, _ := utf8.DecodeLastRuneInString(s)
	return unicode.IsSpace(r) || r == '(' || r == '['
}

// tagWord reads a type word, optionally followed by /Name. Name may contain
// slashes for nested names.
func tagWord(s string) (string, int) {
	n := 0
	for n < len(s) {
		r, size := utf8.DecodeRuneInString(s[n:])
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '/') {
			break
		}
		n += size
	}
	word := strings.TrimRight(s[:n], "/")
	return word, len(word)
}

func validTypeName(name string) error {
	if name == "" || len(name) > maxTypeLength {
		return invalid("type names have 1 to %d characters", maxTypeLength)
	}
	for _, r := range name {
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_') {
			return invalid("type %q may only contain letters, digits, - and _", name)
		}
	}
	return nil
}

// Date phrases. Go's RE2 has no lookaround, so boundaries are captured and
// only the inner group is removed from the text.
var (
	isoDateRe = regexp.MustCompile(`(?i)(^|[\s(])(\d{4}-\d{2}-\d{2})($|[\s),.;!?])`)
	relDayRe  = regexp.MustCompile(`(?i)(^|[\s(])(today|tomorrow|aujourd['’]hui|apr[eè]s-demain|demain)($|[\s),.;!?])`)
	weekdayRe = regexp.MustCompile(`(?i)(^|[\s(])((?:(?:next|this|ce|cette)\s+)?(monday|tuesday|wednesday|thursday|friday|saturday|sunday|lundi|mardi|mercredi|jeudi|vendredi|samedi|dimanche)(?:\s+prochain)?)($|[\s),.;!?])`)
	clockRe   = regexp.MustCompile(`(?i)(^|[\s(])((?:at\s+|à\s+)?(\d{1,2})(?:h(\d{2})?|:(\d{2})|\s?(am|pm)))($|[\s),.;!?])`)
	// "12 novembre", "12 nov", "november 12", "nov 12": the next such day.
	monthNames = `janv(?:ier)?|f[ée]v(?:rier)?|mars|avr(?:il)?|mai|juin|juil(?:let)?|ao[uû]t|sept(?:embre)?|oct(?:obre)?|nov(?:embre)?|d[ée]c(?:embre)?|jan(?:uary)?|feb(?:ruary)?|mar(?:ch)?|apr(?:il)?|may|june?|july?|aug(?:ust)?|sep(?:tember)?|october|november|december`
	dayMonthRe = regexp.MustCompile(`(?i)(^|[\s(])((\d{1,2})(?:er|st|nd|rd|th)?\s+(` + monthNames + `)\.?)($|[\s),.;!?])`)
	monthDayRe = regexp.MustCompile(`(?i)(^|[\s(])((` + monthNames + `)\.?\s+(\d{1,2})(?:st|nd|rd|th)?)($|[\s),.;!?])`)
	// "12/11", "12/11/2026": day first, as the user writes it.
	numericRe = regexp.MustCompile(`(^|[\s(])((\d{1,2})/(\d{1,2})(?:/(\d{4}))?)($|[\s),.;!?])`)
	leadWords = map[string]bool{"on": true, "by": true, "before": true, "for": true, "due": true, "le": true, "pour": true, "avant": true, "d'ici": true}
)

// monthOf reads a month name, French or English, full or abbreviated.
func monthOf(name string) time.Month {
	name = strings.ToLower(name)
	name = strings.NewReplacer("é", "e", "û", "u").Replace(name)
	for i, prefixes := range [][]string{
		{"janv", "jan"}, {"fev", "feb"}, {"mars", "mar"}, {"avr", "apr"}, {"mai", "may"}, {"juin", "jun"},
		{"juil", "jul"}, {"aout", "aug"}, {"sept", "sep"}, {"oct"}, {"nov"}, {"dec"},
	} {
		for _, p := range prefixes {
			if strings.HasPrefix(name, p) {
				return time.Month(i + 1)
			}
		}
	}
	return 0
}

// nextDate returns day/month in the reference year, or the year after
// when that day is already past: dates in text look ahead.
func nextDate(ref time.Time, day int, month time.Month, year int) (string, bool) {
	if month == 0 || day < 1 || day > 31 {
		return "", false
	}
	if year == 0 {
		year = ref.Year()
	}
	d := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	if d.Day() != day {
		return "", false
	}
	if year == ref.Year() && d.Before(ref) && !sameDay(d, ref) {
		d = d.AddDate(1, 0, 0)
	}
	return d.Format(dateLayout), true
}

func sameDay(a, b time.Time) bool {
	return a.Year() == b.Year() && a.YearDay() == b.YearDay()
}

var weekdays = map[string]time.Weekday{
	"sunday": time.Sunday, "monday": time.Monday, "tuesday": time.Tuesday, "wednesday": time.Wednesday,
	"thursday": time.Thursday, "friday": time.Friday, "saturday": time.Saturday,
	"dimanche": time.Sunday, "lundi": time.Monday, "mardi": time.Tuesday, "mercredi": time.Wednesday,
	"jeudi": time.Thursday, "vendredi": time.Friday, "samedi": time.Saturday,
}

// parseDates finds the first date phrase and, when withClock is set, the
// first time phrase in text, relative to day ref, and returns the text
// without them. Date is YYYY-MM-DD, clock is HH:MM; both may be empty.
func parseDates(text string, ref time.Time, withClock bool) (rest, date, clock string) {
	rest = text
	if m := isoDateRe.FindStringSubmatchIndex(rest); m != nil {
		if _, err := time.Parse(dateLayout, rest[m[4]:m[5]]); err == nil {
			date = rest[m[4]:m[5]]
			rest = cut(rest, m[4], m[5])
		}
	}
	if date == "" {
		if m := relDayRe.FindStringSubmatchIndex(rest); m != nil {
			word := strings.ToLower(rest[m[4]:m[5]])
			offset := 0
			switch {
			case word == "tomorrow" || word == "demain":
				offset = 1
			case strings.HasPrefix(word, "apr"):
				offset = 2
			}
			date = ref.AddDate(0, 0, offset).Format(dateLayout)
			rest = cut(rest, m[4], m[5])
		}
	}
	if date == "" {
		if m := dayMonthRe.FindStringSubmatchIndex(rest); m != nil {
			day, _ := strconv.Atoi(rest[m[6]:m[7]])
			if d, ok := nextDate(ref, day, monthOf(rest[m[8]:m[9]]), 0); ok {
				date = d
				rest = cut(rest, m[4], m[5])
			}
		}
	}
	if date == "" {
		if m := monthDayRe.FindStringSubmatchIndex(rest); m != nil {
			day, _ := strconv.Atoi(rest[m[8]:m[9]])
			if d, ok := nextDate(ref, day, monthOf(rest[m[6]:m[7]]), 0); ok {
				date = d
				rest = cut(rest, m[4], m[5])
			}
		}
	}
	if date == "" {
		if m := numericRe.FindStringSubmatchIndex(rest); m != nil {
			day, _ := strconv.Atoi(rest[m[6]:m[7]])
			month, _ := strconv.Atoi(rest[m[8]:m[9]])
			year := 0
			if m[10] >= 0 {
				year, _ = strconv.Atoi(rest[m[10]:m[11]])
			}
			if d, ok := nextDate(ref, day, time.Month(month), year); ok {
				date = d
				rest = cut(rest, m[4], m[5])
			}
		}
	}
	if date == "" {
		if m := weekdayRe.FindStringSubmatchIndex(rest); m != nil {
			target := weekdays[strings.ToLower(rest[m[6]:m[7]])]
			days := (int(target) - int(ref.Weekday()) + 7) % 7
			if days == 0 {
				days = 7
			}
			date = ref.AddDate(0, 0, days).Format(dateLayout)
			rest = cut(rest, m[4], m[5])
		}
	}
	if m := clockRe.FindStringSubmatchIndex(rest); withClock && m != nil {
		hour, _ := strconv.Atoi(rest[m[6]:m[7]])
		minute := 0
		if m[8] >= 0 {
			minute, _ = strconv.Atoi(rest[m[8]:m[9]])
		} else if m[10] >= 0 {
			minute, _ = strconv.Atoi(rest[m[10]:m[11]])
		}
		if m[12] >= 0 {
			pm := strings.EqualFold(rest[m[12]:m[13]], "pm")
			if hour < 1 || hour > 12 {
				hour = -1
			} else if pm && hour != 12 {
				hour += 12
			} else if !pm && hour == 12 {
				hour = 0
			}
		}
		if hour >= 0 && hour < 24 && minute < 60 {
			clock = fmt.Sprintf("%02d:%02d", hour, minute)
			rest = cut(rest, m[4], m[5])
		}
	}
	return strings.Join(strings.Fields(rest), " "), date, clock
}

// cut removes text[start:end] and a directly preceding lead word such as
// "on" or "pour".
func cut(text string, start, end int) string {
	before := strings.TrimRight(text[:start], " ")
	i := strings.LastIndexByte(before, ' ')
	if leadWords[strings.ToLower(before[i+1:])] {
		before = before[:i+1]
	}
	return before + " " + text[end:]
}
