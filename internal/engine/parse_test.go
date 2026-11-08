package engine

import (
	"slices"
	"testing"
	"time"
)

func TestParseInput(t *testing.T) {
	p, err := parseInput("Call [[Marc]] about #project/Northwind today #task #Task C# [[abcdefghijklmnopqrstuvwxyz]] [[A|B|abcdefghijklmnopqrstuvwxy2]]")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(p.Types, []string{"task"}) {
		t.Fatalf("types = %v", p.Types)
	}
	want := []refToken{{Name: "Marc"}, {Name: "Northwind", Type: "project"}, {ID: "abcdefghijklmnopqrstuvwxyz"}, {ID: "abcdefghijklmnopqrstuvwxy2"}}
	if !slices.Equal(p.Refs, want) {
		t.Fatalf("refs = %+v", p.Refs)
	}
	if p.Text != "Call \x000\x00 about \x001\x00 today #task #task C# \x002\x00 \x003\x00" {
		t.Fatalf("text = %q", p.Text)
	}
	if _, err := parseInput("bad #ty.pe"); err != nil {
		t.Fatalf("punctuation ends a type: %v", err)
	}
}

func TestParseDates(t *testing.T) {
	thu := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	if thu.Weekday() != time.Thursday {
		t.Fatalf("reference day is %v", thu.Weekday())
	}
	cases := []struct {
		in, rest, date, clock string
	}{
		{"Send the estimate Friday", "Send the estimate", "2026-11-06", ""},
		{"Envoyer le chiffrage pour vendredi", "Envoyer le chiffrage", "2026-11-06", ""},
		{"Copil jeudi 14h30", "Copil", "2026-11-12", "14:30"},
		{"Review tomorrow at 2pm", "Review", "2026-11-06", "14:00"},
		{"Déjeuner demain 12:15", "Déjeuner", "2026-11-06", "12:15"},
		{"Trip 2026-12-01", "Trip", "2026-12-01", ""},
		{"Point aujourd'hui 9h", "Point", "2026-11-05", "09:00"},
		{"Nothing here", "Nothing here", "", ""},
		{"Livraison le 12 novembre", "Livraison", "2026-11-12", ""},
		{"Ship on nov 12", "Ship", "2026-11-12", ""},
		{"Rétro 3 novembre", "Rétro", "2027-11-03", ""},
		{"Point 12/11 14h", "Point", "2026-11-12", "14:00"},
		{"Audit 03/02/2027", "Audit", "2027-02-03", ""},
		{"Ratio 3/2 is fine", "Ratio is fine", "2027-02-03", ""},
		// Casual words, both ways in time.
		{"yesterday", "", "2026-11-04", ""},
		{"Debrief hier 18h", "Debrief", "2026-11-04", "18:00"},
		{"avant-hier", "", "2026-11-03", ""},
		{"tonight", "", "2026-11-05", "20:00"},
		{"Apéro ce soir 19h", "Apéro", "2026-11-05", "19:00"},
		{"tmr", "", "2026-11-06", ""},
		// Amounts.
		{"in 2 days", "", "2026-11-07", ""},
		{"dans 3 jours", "", "2026-11-08", ""},
		{"in a week", "", "2026-11-12", ""},
		{"dans une semaine", "", "2026-11-12", ""},
		{"2 weeks ago", "", "2026-10-22", ""},
		{"il y a 2 jours", "", "2026-11-03", ""},
		{"in 2 months", "", "2027-01-05", ""},
		{"Renew in 1 year", "Renew", "2027-11-05", ""},
		// Periods.
		{"next week", "", "2026-11-09", ""},
		{"la semaine prochaine", "", "2026-11-09", ""},
		{"last week", "", "2026-10-26", ""},
		{"end of week", "", "2026-11-06", ""},
		{"fin de semaine", "", "2026-11-06", ""},
		{"end of the month", "", "2026-11-30", ""},
		{"fin du mois", "", "2026-11-30", ""},
		{"next month", "", "2026-12-01", ""},
		{"le mois prochain", "", "2026-12-01", ""},
		{"end of year", "", "2026-12-31", ""},
		{"this weekend", "", "2026-11-07", ""},
		{"ce week-end", "", "2026-11-07", ""},
		{"Plan the week", "Plan the week", "", ""},
		// Weekdays with modifiers and short names.
		{"last friday", "", "2026-10-30", ""},
		{"vendredi dernier", "", "2026-10-30", ""},
		{"this thursday", "", "2026-11-05", ""},
		{"thursday", "", "2026-11-12", ""},
		{"fri 5:30pm", "", "2026-11-06", "17:30"},
		{"mon. 9am", "", "2026-11-09", "09:00"},
		{"ven 14h", "", "2026-11-06", "14:00"},
		{"mar 12", "", "2027-03-12", ""},
		// Times alone.
		{"noon", "", "", "12:00"},
		{"à midi", "", "", "12:00"},
		{"minuit", "", "", "00:00"},
		{"at 9:15", "", "", "09:15"},
		{"12am", "", "", "00:00"},
	}
	for _, c := range cases {
		rest, date, clock := parseDates(c.in, thu, true)
		if rest != c.rest || date != c.date || clock != c.clock {
			t.Errorf("%q => %q %q %q", c.in, rest, date, clock)
		}
	}
	if rest, _, clock := parseDates("Call at 14h Friday", thu, false); clock != "" || rest != "Call at 14h" {
		t.Errorf("tasks ignore times: %q %q", rest, clock)
	}
}

// Lines survive, for tables and short notes; blank lines and runs of
// spaces do not, and the first line names the node.
func TestParseInputLines(t *testing.T) {
	p, err := parseInput("Prices   #reference\r\n\n| TV | Price |\n|---|---|\n| 77OLED811  | 2199 € |\n")
	if err != nil {
		t.Fatal(err)
	}
	if p.Text != "Prices #reference\n| TV | Price |\n|---|---|\n| 77OLED811 | 2199 € |" {
		t.Fatalf("text = %q", p.Text)
	}
	if l := labelOf(p.Text); l != "Prices" {
		t.Fatalf("label = %q", l)
	}
}

func TestLevenshtein(t *testing.T) {
	if levenshtein("tsak", "task") != 2 || levenshtein("", "abc") != 3 {
		t.Fatal("levenshtein")
	}
}
