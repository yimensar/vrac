package engine

import (
	"context"
	"strings"
)

// Capture writes one line, typed without assistance (a shortcut, a widget),
// to the end of today's journal. Unlike Create it never rejects a name: a
// #type or [[name]] that is unknown or ambiguous is kept as plain words and
// listed in Result.Unresolved, so nothing typed is lost and nothing is
// created implicitly.
func (e *Engine) Capture(ctx context.Context, actor Actor, requestID, text string) (Result, error) {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	if text == "" {
		return Result{}, invalid("text is required")
	}
	return e.mutate(ctx, actor, requestID, struct{ Capture string }{text}, func(s *txState) ([]int64, error) {
		p, err := parseInput(text)
		if err != nil {
			return nil, err
		}
		var b strings.Builder
		var plain []Unresolved
		last := 0
		seen := map[string]bool{}
		for _, t := range p.Tokens {
			s.unresolved, s.reported = nil, map[string]bool{}
			if t.Ref >= 0 {
				err = s.checkRef(p.Refs[t.Ref])
			} else {
				_, _, err = s.typeID(t.Type, true)
			}
			if err != nil {
				return nil, err
			}
			if len(s.unresolved) == 0 {
				continue
			}
			for _, u := range s.unresolved {
				if key := u.Kind + "\x00" + nameKey(u.Name); !seen[key] {
					seen[key] = true
					plain = append(plain, u)
				}
			}
			b.WriteString(text[last:t.Start])
			b.WriteString(plainWords(text[t.Start:t.End]))
			last = t.End
		}
		b.WriteString(text[last:])
		s.unresolved, s.reported, s.plain = nil, map[string]bool{}, plain
		return s.create(CreateRequest{Nodes: []Draft{{Input: b.String()}}})
	})
}

// plainWords is the readable text of a #type or [[name]] token.
func plainWords(tok string) string {
	if inner, ok := strings.CutPrefix(tok, "[["); ok {
		inner = strings.TrimSpace(strings.TrimSuffix(inner, "]]"))
		if bar := strings.LastIndexByte(inner, '|'); bar >= 0 && isID(strings.TrimSpace(inner[bar+1:])) {
			inner = strings.TrimSpace(inner[:bar])
		}
		return inner
	}
	return strings.TrimPrefix(tok, "#")
}
