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
// Text contains placeholders \x00N\x00 for refs[N].
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
				} else if !seenType[typ] {
					seenType[typ] = true
					p.Types = append(p.Types, typ)
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
	leadWords = map[string]bool{"on": true, "by": true, "before": true, "for": true, "due": true, "le": true, "pour": true, "avant": true, "d'ici": true}
)

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
