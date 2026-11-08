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
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
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
	p.Text = normalizeLines(b.String())
	if len(p.Text) > maxTextLength {
		return p, invalid("text exceeds %d bytes", maxTextLength)
	}
	return p, nil
}

// normalizeLines keeps the line breaks of a text (a table, a few lines of
// a note) and normalizes the spaces of each line; blank lines are dropped.
func normalizeLines(text string) string {
	var lines []string
	for _, l := range strings.Split(text, "\n") {
		if l = strings.Join(strings.Fields(l), " "); l != "" {
			lines = append(lines, l)
		}
	}
	return strings.Join(lines, "\n")
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
// only the inner group is removed from the text. Phrases are read in
// English or French the way task tools read them: casual words (today,
// yesterday, tonight), amounts (in 3 days, 2 weeks ago), periods (next
// week, end of month, this weekend), weekdays with a modifier, dates by
// month name or number, and a time.
const (
	lead  = `(?i)(^|[\s(])(`
	trail = `)($|[\s),.;!?])`
)

var (
	isoDateRe = regexp.MustCompile(lead + `(\d{4}-\d{2}-\d{2})` + trail)
	casualRe  = regexp.MustCompile(lead + `(today|tod|tonight|tomorrow|tmrw?|yesterday|aujourd['’]hui|ce soir|demain|hier|apr[eè]s-demain|avant-hier)` + trail)
	// "in 3 days", "2 weeks ago", "dans une semaine", "il y a 2 jours".
	amountRe = regexp.MustCompile(lead + `(?:(in|dans|il y a)\s+)?(\d{1,3}|an?|one|une?)\s+(days?|weeks?|months?|years?|jours?|semaines?|mois|ans?|ann[ée]es?)(?:\s+(ago|from now|plus tard))?` + trail)
	// "next week", "la semaine prochaine", "end of month", "fin du mois",
	// "this weekend": a period needs a modifier to be a date.
	periodRe  = regexp.MustCompile(lead + `(?:(next|this|last|end of(?: the)?|la|le|l['’]|ce|cette|fin (?:de la|de l['’]|du|de)(?: cette?| ce)?)\s*)?(week-?end|week|month|year|semaine|mois|ann[ée]e)(?:\s+(prochaine?|derni[eè]re?))?` + trail)
	weekdayRe = regexp.MustCompile(lead + `(?:(next|this|last|ce|cette)\s+)?(monday|mon|tuesday|tues?|wednesday|wed|thursday|thu(?:rs?)?|friday|fri|saturday|sat|sunday|sun|lundi|lun|mardi|mar|mercredi|mer|jeudi|jeu|vendredi|ven|samedi|sam|dimanche|dim)\.?(?:\s+(prochain|dernier))?` + trail)
	// "14h", "14h30", "2pm", "5:30pm", "at 9:15", "noon", "minuit".
	clockRe = regexp.MustCompile(lead + `(?:at\s+|à\s+)?(?:(noon|midi|midnight|minuit)|(\d{1,2})(?:h(\d{2})?|:(\d{2})(?:\s?(am|pm))?|\s?(am|pm)))` + trail)
	// "12 novembre", "12 nov", "november 12", "nov 12": the next such day.
	monthNames = `janv(?:ier)?|f[ée]v(?:rier)?|mars|avr(?:il)?|mai|juin|juil(?:let)?|ao[uû]t|sept(?:embre)?|oct(?:obre)?|nov(?:embre)?|d[ée]c(?:embre)?|jan(?:uary)?|feb(?:ruary)?|mar(?:ch)?|apr(?:il)?|may|june?|july?|aug(?:ust)?|sep(?:tember)?|october|november|december`
	dayMonthRe = regexp.MustCompile(lead + `(\d{1,2})(?:er|st|nd|rd|th)?\s+(` + monthNames + `)\.?` + trail)
	monthDayRe = regexp.MustCompile(lead + `(` + monthNames + `)\.?\s+(\d{1,2})(?:st|nd|rd|th)?` + trail)
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

// weekdayOf reads a weekday name, French or English, full or three
// letters.
func weekdayOf(name string) (time.Weekday, bool) {
	name = strings.ToLower(name)
	if len(name) > 3 {
		name = name[:3]
	}
	d, ok := map[string]time.Weekday{
		"sun": time.Sunday, "mon": time.Monday, "tue": time.Tuesday, "wed": time.Wednesday, "thu": time.Thursday, "fri": time.Friday, "sat": time.Saturday,
		"dim": time.Sunday, "lun": time.Monday, "mar": time.Tuesday, "mer": time.Wednesday, "jeu": time.Thursday, "ven": time.Friday, "sam": time.Saturday,
	}[name]
	return d, ok
}

// weekdayFrom returns the days from ref to the coming weekday (today
// counts when allowToday), or to the past one when back.
func weekdayFrom(ref time.Time, target time.Weekday, allowToday, back bool) int {
	days := (int(target) - int(ref.Weekday()) + 7) % 7
	if back {
		return days - 7
	}
	if days == 0 && !allowToday {
		days = 7
	}
	return days
}

// amount reads a count word: "3", "a", "une".
func amount(word string) int {
	if n, err := strconv.Atoi(word); err == nil {
		return n
	}
	return 1
}

// periodDate reads a period phrase: the modifier before it, the unit, the
// modifier after it. A period without a modifier is not a date.
func periodDate(ref time.Time, before, unit, after string) (string, bool) {
	before, after = strings.ToLower(before), strings.ToLower(after)
	unit = strings.ToLower(strings.ReplaceAll(unit, "-", ""))
	if strings.HasPrefix(unit, "ann") {
		unit = "year"
	}
	var next, last, end, this bool
	switch {
	case before == "next" || strings.HasPrefix(after, "prochain"):
		next = true
	case before == "last" || strings.HasPrefix(after, "dernier") || strings.HasPrefix(after, "dernièr"):
		last = true
	case strings.HasPrefix(before, "end of") || strings.HasPrefix(before, "fin "):
		end = true
	case before == "this" || before == "ce" || before == "cette":
		this = true
	default:
		return "", false
	}
	monday := ref.AddDate(0, 0, -((int(ref.Weekday()) + 6) % 7))
	first := time.Date(ref.Year(), ref.Month(), 1, 0, 0, 0, 0, time.UTC)
	var d time.Time
	switch unit {
	case "weekend":
		if !next && !this {
			return "", false
		}
		d = ref.AddDate(0, 0, weekdayFrom(ref, time.Saturday, true, false))
		if next && ref.Weekday() == time.Saturday {
			d = d.AddDate(0, 0, 7)
		}
	case "week", "semaine":
		switch {
		case next:
			d = monday.AddDate(0, 0, 7)
		case last:
			d = monday.AddDate(0, 0, -7)
		case end:
			d = ref.AddDate(0, 0, weekdayFrom(ref, time.Friday, true, false))
		default:
			return "", false
		}
	case "month", "mois":
		switch {
		case next:
			d = first.AddDate(0, 1, 0)
		case last:
			d = first.AddDate(0, -1, 0)
		case end:
			d = first.AddDate(0, 1, -1)
		default:
			return "", false
		}
	case "year":
		switch {
		case next:
			d = time.Date(ref.Year()+1, 1, 1, 0, 0, 0, 0, time.UTC)
		case last:
			d = time.Date(ref.Year()-1, 1, 1, 0, 0, 0, 0, time.UTC)
		case end:
			d = time.Date(ref.Year(), 12, 31, 0, 0, 0, 0, time.UTC)
		default:
			return "", false
		}
	default:
		return "", false
	}
	return d.Format(dateLayout), true
}

// parseDates finds the first date phrase and, when withClock is set, the
// first time phrase in text, relative to day ref, and returns the text
// without them. Date is YYYY-MM-DD, clock is HH:MM; both may be empty.
func parseDates(text string, ref time.Time, withClock bool) (rest, date, clock string) {
	rest = text
	evening := false
	if m := isoDateRe.FindStringSubmatchIndex(rest); m != nil {
		if _, err := time.Parse(dateLayout, rest[m[4]:m[5]]); err == nil {
			date = rest[m[4]:m[5]]
			rest = cut(rest, m[4], m[5])
		}
	}
	if date == "" {
		if m := casualRe.FindStringSubmatchIndex(rest); m != nil {
			word := strings.ToLower(rest[m[4]:m[5]])
			offset := 0
			switch {
			case strings.HasPrefix(word, "tom") || strings.HasPrefix(word, "tmr") || word == "demain":
				offset = 1
			case strings.HasPrefix(word, "apr"):
				offset = 2
			case word == "yesterday" || word == "hier":
				offset = -1
			case word == "avant-hier":
				offset = -2
			case word == "tonight" || word == "ce soir":
				evening = true
			}
			date = ref.AddDate(0, 0, offset).Format(dateLayout)
			rest = cut(rest, m[4], m[5])
		}
	}
	if date == "" {
		if m := amountRe.FindStringSubmatchIndex(rest); m != nil {
			n := amount(strings.ToLower(rest[m[8]:m[9]]))
			if (m[6] >= 0 && strings.EqualFold(rest[m[6]:m[7]], "il y a")) || (m[12] >= 0 && strings.EqualFold(rest[m[12]:m[13]], "ago")) {
				n = -n
			}
			unit := strings.ToLower(rest[m[10]:m[11]])
			var d time.Time
			switch {
			case strings.HasPrefix(unit, "day") || strings.HasPrefix(unit, "jour"):
				d = ref.AddDate(0, 0, n)
			case strings.HasPrefix(unit, "week") || strings.HasPrefix(unit, "semaine"):
				d = ref.AddDate(0, 0, 7*n)
			case strings.HasPrefix(unit, "month") || unit == "mois":
				d = ref.AddDate(0, n, 0)
			default:
				d = ref.AddDate(n, 0, 0)
			}
			date = d.Format(dateLayout)
			rest = cut(rest, m[4], m[5])
		}
	}
	if date == "" {
		if m := periodRe.FindStringSubmatchIndex(rest); m != nil {
			before, after := "", ""
			if m[6] >= 0 {
				before = strings.TrimSpace(rest[m[6]:m[7]])
			}
			if m[10] >= 0 {
				after = rest[m[10]:m[11]]
			}
			if d, ok := periodDate(ref, before, rest[m[8]:m[9]], after); ok {
				date = d
				rest = cut(rest, m[4], m[5])
			}
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
			if target, ok := weekdayOf(rest[m[8]:m[9]]); ok {
				before, after := "", ""
				if m[6] >= 0 {
					before = strings.ToLower(rest[m[6]:m[7]])
				}
				if m[10] >= 0 {
					after = strings.ToLower(rest[m[10]:m[11]])
				}
				back := before == "last" || after == "dernier"
				this := before == "this" || before == "ce" || before == "cette"
				date = ref.AddDate(0, 0, weekdayFrom(ref, target, this, back)).Format(dateLayout)
				rest = cut(rest, m[4], m[5])
			}
		}
	}
	if m := clockRe.FindStringSubmatchIndex(rest); withClock && m != nil {
		hour, minute := -1, 0
		if m[6] >= 0 {
			word := strings.ToLower(rest[m[6]:m[7]])
			hour = 12
			if word == "midnight" || word == "minuit" {
				hour = 0
			}
		} else {
			hour, _ = strconv.Atoi(rest[m[8]:m[9]])
			if m[10] >= 0 {
				minute, _ = strconv.Atoi(rest[m[10]:m[11]])
			} else if m[12] >= 0 {
				minute, _ = strconv.Atoi(rest[m[12]:m[13]])
			}
			ampm := ""
			for _, g := range []int{14, 16} {
				if m[g] >= 0 {
					ampm = strings.ToLower(rest[m[g]:m[g+1]])
				}
			}
			if ampm != "" {
				if hour < 1 || hour > 12 {
					hour = -1
				} else if ampm == "pm" && hour != 12 {
					hour += 12
				} else if ampm == "am" && hour == 12 {
					hour = 0
				}
			}
		}
		if hour >= 0 && hour < 24 && minute < 60 {
			clock = fmt.Sprintf("%02d:%02d", hour, minute)
			rest = cut(rest, m[4], m[5])
		}
	}
	if clock == "" && evening && withClock {
		clock = "20:00"
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
