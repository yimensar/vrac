package store

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
)

var inlineCode = regexp.MustCompile("`[^`]*`")
var tagPattern = regexp.MustCompile(`(?:^|[\s(])#([\pL\pN][\pL\pN_/-]*)`)

// ExtractTags deliberately ignores fenced and inline code, headings and URLs.
func ExtractTags(content string) []string {
	seen := map[string]bool{}
	fence := ""
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			marker := trimmed[:3]
			if fence == "" {
				fence = marker
			} else if fence == marker {
				fence = ""
			}
			continue
		}
		if fence != "" {
			continue
		}
		line = inlineCode.ReplaceAllString(line, "")
		for _, m := range tagPattern.FindAllStringSubmatch(line, -1) {
			tag := strings.ToLower(strings.TrimRight(m[1], "/-"))
			if tag != "" {
				seen[tag] = true
				parts := strings.Split(tag, "/")
				for i := 1; i < len(parts); i++ {
					seen[strings.Join(parts[:i], "/")] = true
				}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for t := range seen {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}
func Title(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		fields := strings.Fields(line)
		onlyTags := len(fields) > 0
		for _, field := range fields {
			if !strings.HasPrefix(field, "#") || len(field) < 2 {
				onlyTags = false
			}
		}
		if onlyTags {
			continue
		}
		line = strings.TrimSpace(strings.TrimLeft(line, "#>- "))
		for _, marker := range []string{"**", "__", "*", "_", "`"} {
			if strings.HasPrefix(line, marker) && strings.HasSuffix(line, marker) && len(line) > len(marker)*2 {
				line = strings.TrimSuffix(strings.TrimPrefix(line, marker), marker)
			}
		}
		if line == "" || strings.HasPrefix(line, "```") {
			continue
		}
		r := []rune(line)
		if len(r) > 130 {
			line = string(r[:130]) + "…"
		}
		return line
	}
	return "Untitled"
}
func words(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}
