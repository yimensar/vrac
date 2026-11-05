package main

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/yan-imensar/vrac/internal/engine"
)

var paris, _ = time.LoadLocation("Europe/Paris")

// world is a workspace under evaluation.
type world struct {
	e       *engine.Engine
	answer  string
	changes []change
}

// change is one revision written by the evaluated AI.
type change struct {
	action string
	node   engine.Node
	source string
}

// scenario is a task given to the AI, with what it must leave behind.
type scenario struct {
	name   string
	prompt string
	// prompt is built at run time when set, for prompts naming days.
	promptf func() string
	prepare func(w *world) map[string]any
	check   func(w *world, before map[string]any) []string
}

func changes(e *engine.Engine) []change {
	var out []change
	cursor := ""
	for {
		page, err := e.Activity(ctx, "eval", cursor, 200)
		if err != nil {
			return out
		}
		for _, a := range page.Items {
			v, err := e.GetNode(ctx, a.Node.ID)
			if err == nil {
				out = append(out, change{action: a.Action, node: v.Node, source: a.Source})
			}
		}
		if page.NextCursor == "" {
			return out
		}
		cursor = page.NextCursor
	}
}

// created lists nodes the AI created and kept: trashed ones do not count.
func (w *world) created(types ...string) []engine.Node {
	var out []engine.Node
	for _, c := range w.changes {
		if c.action == "create" && !c.node.Trashed && (len(types) == 0 || slices.ContainsFunc(types, func(t string) bool { return slices.Contains(c.node.Types, t) })) {
			out = append(out, c.node)
		}
	}
	return out
}

func (w *world) node(id string) engine.Node {
	v, _ := w.e.GetNode(ctx, id)
	return v.Node
}

func (w *world) one(name string) string {
	c, _ := w.e.Resolve(ctx, name, 1)
	if len(c) == 0 {
		return ""
	}
	return c[0].ID
}

func (w *world) search(q engine.Query) []engine.Node {
	q.Limit = 200
	p, _ := w.e.Search(ctx, q)
	return p.Nodes
}

// find returns the first node whose plain text contains words.
func (w *world) find(words string, q engine.Query) engine.Node {
	q.Text = words
	for _, n := range w.search(q) {
		return n
	}
	return engine.Node{}
}

func concerns(w *world, n engine.Node, subject string) bool {
	for _, x := range w.search(engine.Query{Concerns: []string{subject}}) {
		if x.ID == n.ID {
			return true
		}
	}
	return false
}

var accents = strings.NewReplacer("à", "a", "â", "a", "ä", "a", "ç", "c", "é", "e", "è", "e", "ê", "e", "ë", "e",
	"î", "i", "ï", "i", "ô", "o", "ö", "o", "ù", "u", "û", "u", "ü", "u", "ÿ", "y", "œ", "oe", "æ", "ae")

// fold lowercases and strips French accents and punctuation.
func fold(s string) string {
	s = accents.Replace(strings.ToLower(s))
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }), " ")
}

// mentions reports whether the answer covers most significant words of a
// node's text.
func mentions(answer, text string) bool {
	a := " " + fold(answer) + " "
	words, hit := 0, 0
	for _, word := range strings.Fields(fold(refless(text))) {
		if len(word) < 4 {
			continue
		}
		words++
		if strings.Contains(a, word) {
			hit++
		}
	}
	return words == 0 || float64(hit)/float64(words) >= 0.6
}

func refless(text string) string {
	for {
		i := strings.Index(text, "[[")
		j := strings.Index(text, "]]")
		if i < 0 || j < i {
			break
		}
		text = text[:i] + text[j+2:]
	}
	var out []string
	for _, f := range strings.Fields(text) {
		if !strings.HasPrefix(f, "#") {
			out = append(out, f)
		}
	}
	return strings.Join(out, " ")
}

func today() time.Time {
	d, _ := time.ParseInLocation("2006-01-02", time.Now().In(paris).Format("2006-01-02"), paris)
	return d
}

// next returns the date of the next given weekday, strictly after today.
func next(day time.Weekday) string {
	t := today()
	n := (int(day) - int(t.Weekday()) + 7) % 7
	if n == 0 {
		n = 7
	}
	return t.AddDate(0, 0, n).Format("2006-01-02")
}

var frenchDays = map[time.Weekday]string{time.Monday: "lundi", time.Tuesday: "mardi", time.Wednesday: "mercredi",
	time.Thursday: "jeudi", time.Friday: "vendredi", time.Saturday: "samedi", time.Sunday: "dimanche"}

func noWrites(w *world) []string {
	if len(w.changes) > 0 {
		return []string{fmt.Sprintf("wrote %d changes to answer a question", len(w.changes))}
	}
	return nil
}

func sourced(w *world) []string {
	for _, c := range w.changes {
		if c.source == "" || c.source == "api" {
			return []string{"a change has no source"}
		}
	}
	return nil
}

func noNewEntities(w *world) []string {
	if n := len(w.created("project", "person")); n > 0 {
		return []string{fmt.Sprintf("created %d projects or people", n)}
	}
	return nil
}

func expect(ok bool, failure string, args ...any) []string {
	if ok {
		return nil
	}
	return []string{fmt.Sprintf(failure, args...)}
}

var scenarios = []scenario{
	{
		name:   "read-open-tasks",
		prompt: "Quelles sont mes tâches ouvertes sur le projet Northwind ? Liste leurs intitulés.",
		prepare: func(w *world) map[string]any {
			return map[string]any{"tasks": w.search(engine.Query{Concerns: []string{w.one("Northwind")}, Types: []string{"task"}, Status: "open"})}
		},
		check: func(w *world, b map[string]any) []string {
			f := noWrites(w)
			for _, t := range b["tasks"].([]engine.Node) {
				f = append(f, expect(mentions(w.answer, t.Text), "missed task %q", refless(t.Text))...)
			}
			return f
		},
	},
	{
		name:   "read-decisions",
		prompt: "Quelles décisions ont été prises sur Globex ? De la plus récente à la plus ancienne.",
		prepare: func(w *world) map[string]any {
			return map[string]any{"decisions": w.search(engine.Query{Concerns: []string{w.one("Globex")}, Types: []string{"decision"}})}
		},
		check: func(w *world, b map[string]any) []string {
			f := noWrites(w)
			for _, d := range b["decisions"].([]engine.Node) {
				f = append(f, expect(mentions(w.answer, d.Text), "missed decision %q", refless(d.Text))...)
			}
			return f
		},
	},
	{
		name:   "read-next-meeting",
		prompt: "C'est quand mon prochain rendez-vous lié à Northwind, et à quelle heure ?",
		prepare: func(w *world) map[string]any {
			// The next one depends on the time of the run.
			now := time.Now().In(paris).Format("2006-01-02T15:04")
			for _, m := range w.search(engine.Query{Concerns: []string{w.one("Northwind")}, Types: []string{"meeting", "event"}, From: today().Format("2006-01-02"), Scheduled: true, Sort: "date_asc"}) {
				if m.Start > now {
					return map[string]any{"meeting": m}
				}
			}
			return map[string]any{}
		},
		check: func(w *world, b map[string]any) []string {
			m, ok := b["meeting"].(engine.Node)
			if !ok {
				return []string{"the sample has no upcoming meeting"}
			}
			hour := strings.TrimLeft(m.Start[11:13], "0")
			return append(noWrites(w), expect(mentions(w.answer, m.Text) && strings.Contains(w.answer, hour), "did not give %q at %s", refless(m.Text), m.Start)...)
		},
	},
	{
		name:   "read-overdue",
		prompt: "Qu'est-ce que j'ai en retard ?",
		prepare: func(w *world) map[string]any {
			yesterday := today().AddDate(0, 0, -1).Format("2006-01-02")
			return map[string]any{"late": w.search(engine.Query{Types: []string{"task"}, Status: "open", To: yesterday, Scheduled: true})}
		},
		check: func(w *world, b map[string]any) []string {
			f := noWrites(w)
			late := b["late"].([]engine.Node)
			f = append(f, expect(len(late) > 0, "the sample has no overdue task")...)
			for _, t := range late {
				f = append(f, expect(mentions(w.answer, t.Text), "missed overdue task %q", refless(t.Text))...)
			}
			return f
		},
	},
	{
		name:    "read-why",
		prompt:  "Pourquoi est-ce qu'on est passé à l'API v2 plutôt qu'à l'export CSV sur Globex ?",
		prepare: func(w *world) map[string]any { return nil },
		check: func(w *world, _ map[string]any) []string {
			return append(noWrites(w), expect(strings.Contains(fold(w.answer), "format"), "did not find the supplier's format change")...)
		},
	},
	{
		name: "write-mail-triage",
		// The deadline names the weekday three days ahead, never today.
		promptf: func() string {
			return fmt.Sprintf(`Je viens de recevoir ce mail, traite-le dans vrac :

De : Bruno
Objet : Lot 2 et comité

Bonjour Yan, tu peux m'envoyer la synthèse de l'atelier notifications avant %s ? Par ailleurs le comité de pilotage Northwind est décalé à 14h, même jour.
Merci, Bruno`, frenchDays[today().AddDate(0, 0, 3).Weekday()])
		},
		prepare: func(w *world) map[string]any {
			return map[string]any{
				"task":    w.find("synthese atelier", engine.Query{Types: []string{"task"}}).ID,
				"meeting": w.find("comite pilotage", engine.Query{Types: []string{"meeting"}}).ID,
			}
		},
		check: func(w *world, b map[string]any) []string {
			task, meeting := w.node(b["task"].(string)), w.node(b["meeting"].(string))
			f := append(sourced(w), noNewEntities(w)...)
			// "Before <day>": that day or the day before are fair readings.
			deadline := today().AddDate(0, 0, 3)
			f = append(f, expect(task.Due == deadline.Format("2006-01-02") || task.Due == deadline.AddDate(0, 0, -1).Format("2006-01-02"), "the existing task's due date is %q, not before %s", task.Due, deadline.Format("2006-01-02"))...)
			f = append(f, expect(strings.HasSuffix(meeting.Start, "T14:00"), "the existing meeting starts at %q, not 14:00", meeting.Start)...)
			for _, n := range w.created("task", "meeting") {
				if mentions(n.Text, "synthese atelier") || mentions(n.Text, "comite pilotage") {
					f = append(f, fmt.Sprintf("created a duplicate %q", refless(n.Text)))
				}
			}
			return f
		},
	},
	{
		name:   "write-meeting-notes",
		prompt: `Compte rendu de ma réunion de ce matin avec Alice sur Globex : la bascule vers l'API v2 est repoussée d'une semaine. Alice prépare un plan de retour arrière. De mon côté je dois mettre à jour la doc d'import avant mercredi.`,
		prepare: func(w *world) map[string]any {
			cutover := w.find("bascule", engine.Query{Types: []string{"event"}})
			return map[string]any{"cutover": cutover.ID, "start": cutover.Start, "alice": w.one("Alice"), "globex": w.one("Globex")}
		},
		check: func(w *world, b map[string]any) []string {
			f := append(sourced(w), noNewEntities(w)...)
			globex, alice := b["globex"].(string), b["alice"].(string)
			meetings := w.created("meeting")
			f = append(f, expect(len(meetings) == 1, "created %d meetings, not 1", len(meetings))...)
			if len(meetings) == 1 {
				m := meetings[0]
				f = append(f, expect(concerns(w, m, globex) && concerns(w, m, alice), "the meeting does not concern Globex and Alice")...)
			}
			decided := false
			for _, d := range w.created("decision") {
				decided = decided || (concerns(w, d, globex) && (strings.Contains(fold(d.Text), "semaine") || strings.Contains(fold(d.Text), "repouss")))
			}
			f = append(f, expect(decided, "no decision about the delay concerning Globex")...)
			mine := false
			for _, t := range w.created("task") {
				mine = mine || (concerns(w, t, globex) && strings.Contains(fold(t.Text), "doc") && t.Due == next(time.Wednesday))
			}
			f = append(f, expect(mine, "no task to update the import documentation due %s", next(time.Wednesday))...)
			cutover := w.node(b["cutover"].(string))
			old, _ := time.Parse("2006-01-02", b["start"].(string)[:10])
			want := old.AddDate(0, 0, 7).Format("2006-01-02")
			f = append(f, expect(strings.HasPrefix(cutover.Start, want), "the cutover event starts %q, not a week later (%s)", cutover.Start, want)...)
			return f
		},
	},
	{
		name:   "write-task-done",
		prompt: "C'est fait, j'ai envoyé le cahier des charges des imports à Alice.",
		prepare: func(w *world) map[string]any {
			return map[string]any{"task": w.find("cahier charges imports", engine.Query{Types: []string{"task"}}).ID}
		},
		check: func(w *world, b map[string]any) []string {
			task := w.node(b["task"].(string))
			f := expect(task.Status == "done", "the task is %q, not done", task.Status)
			return append(f, expect(len(w.created()) == 0, "created %d nodes", len(w.created()))...)
		},
	},
	{
		name:   "write-decision-evolves",
		prompt: "Finalement, sur Globex, on garde l'export CSV en secours deux mois au lieu d'un.",
		prepare: func(w *world) map[string]any {
			d := w.find("export csv secours", engine.Query{Types: []string{"decision"}})
			return map[string]any{"decision": d.ID, "revision": d.Revision}
		},
		check: func(w *world, b map[string]any) []string {
			d := w.node(b["decision"].(string))
			a := fold(d.Text)
			f := expect(strings.Contains(a, "deux mois") || strings.Contains(a, "2 mois"), "the decision says %q", refless(d.Text))
			f = append(f, expect(slices.Contains(d.Types, "decision"), "the decision lost its type")...)
			return append(f, expect(len(w.created("decision")) == 0, "created a new decision instead of updating it")...)
		},
	},
	{
		name:    "write-new-prospect",
		prompt:  "Note que j'ai un premier appel demain à 11h avec Bertrand Martin, de la société Acme, un nouveau prospect. Tu peux créer ce qui n'existe pas encore.",
		prepare: func(w *world) map[string]any { return nil },
		check: func(w *world, _ map[string]any) []string {
			var f []string
			people, projects := w.created("person"), w.created("project")
			f = append(f, expect(len(people) == 1 && strings.Contains(fold(people[0].Text), "bertrand"), "people created: %d", len(people))...)
			f = append(f, expect(len(projects) == 1 && strings.Contains(fold(projects[0].Text), "acme"), "projects created: %d", len(projects))...)
			tomorrow := today().AddDate(0, 0, 1).Format("2006-01-02")
			call := false
			for _, m := range w.created("meeting", "event") {
				call = call || m.Start == tomorrow+"T11:00" && len(people) == 1 && concerns(w, m, people[0].ID)
			}
			return append(f, expect(call, "no call tomorrow at 11:00 concerning Bertrand Martin")...)
		},
	},
}
