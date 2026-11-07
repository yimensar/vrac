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

func TestLevenshtein(t *testing.T) {
	if levenshtein("tsak", "task") != 2 || levenshtein("", "abc") != 3 {
		t.Fatal("levenshtein")
	}
}
