package engine

import (
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

var referencePattern = regexp.MustCompile(`\[\[([^\]\n]+)\]\]`)
var tagPattern = regexp.MustCompile(`(^|[\t \n])#([\p{L}\p{N}_-]+)`)

func CanonicalTag(tag string) (string, error) {
	tag = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(tag), "#"))
	if tag == "" || utf8.RuneCountInString(tag) > 64 {
		return "", fmt.Errorf("%w: tags must contain 1 to 64 characters", ErrInvalid)
	}
	for _, r := range tag {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-' {
			return "", fmt.Errorf("%w: invalid tag %q", ErrInvalid, tag)
		}
	}
	return tag, nil
}
func contentInput(text string, tags []string) (string, []string, error) {
	if !utf8.ValidString(text) || len(text) > 1024*1024 {
		return "", nil, fmt.Errorf("%w: text must be valid UTF-8 and at most 1 MiB", ErrInvalid)
	}
	refs := referencePattern.FindAllStringIndex(text, -1)
	matches := tagPattern.FindAllStringSubmatchIndex(text, -1)
	var out strings.Builder
	last := 0
	for _, m := range matches {
		inside := false
		for _, r := range refs {
			if m[0] >= r[0] && m[0] < r[1] {
				inside = true
				break
			}
		}
		if inside {
			continue
		}
		tags = append(tags, text[m[4]:m[5]])
		out.WriteString(text[last:m[0]])
		out.WriteString(text[m[2]:m[3]])
		last = m[1]
	}
	out.WriteString(text[last:])
	text = strings.TrimSpace(out.String())
	if text == "" {
		return "", nil, fmt.Errorf("%w: node text must not be empty", ErrInvalid)
	}
	set := map[string]bool{}
	for _, tag := range tags {
		v, err := CanonicalTag(tag)
		if err != nil {
			return "", nil, err
		}
		set[v] = true
	}
	if len(set) > 64 {
		return "", nil, fmt.Errorf("%w: at most 64 tags per node", ErrInvalid)
	}
	canonical := make([]string, 0, len(set))
	for tag := range set {
		canonical = append(canonical, tag)
	}
	sort.Strings(canonical)
	return text, canonical, nil
}
func (e *Engine) setContent(tx *sql.Tx, id, text string, tags, referenceIDs []string) error {
	old := map[string]string{}
	rows, err := tx.Query("SELECT token,target_id FROM node_references WHERE source_id=?", id)
	if err != nil {
		return err
	}
	for rows.Next() {
		var token, target string
		if err = rows.Scan(&token, &target); err != nil {
			rows.Close()
			return err
		}
		old[token] = target
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	resolved := map[string]string{}
	for _, match := range referencePattern.FindAllStringSubmatch(text, -1) {
		token := strings.TrimSpace(match[1])
		if token == "" {
			return fmt.Errorf("%w: empty reference", ErrInvalid)
		}
		if target, ok := old[token]; ok {
			resolved[token] = target
			continue
		}
		identity := token
		if p := strings.IndexByte(token, '|'); p >= 0 {
			identity = strings.TrimSpace(token[:p])
		}
		var target string
		err = tx.QueryRow("SELECT id FROM nodes WHERE id=? AND deleted=0", identity).Scan(&target)
		if err == nil {
			resolved[token] = target
			continue
		}
		if err != sql.ErrNoRows {
			return err
		}
		if identity != token {
			return fmt.Errorf("%w: reference target %s", ErrNotFound, identity)
		}
		err = tx.QueryRow("SELECT id FROM nodes WHERE parent_id IS NULL AND system IS NULL AND deleted=0 AND text=? ORDER BY position,id LIMIT 1", token).Scan(&target)
		if err == sql.ErrNoRows {
			n, err := e.createConcept(tx, token)
			if err != nil {
				return err
			}
			target = n.ID
		} else if err != nil {
			return err
		}
		resolved[token] = target
	}
	if referenceIDs == nil {
		for token, target := range old {
			if strings.HasPrefix(token, "@") {
				resolved[token] = target
			}
		}
	}
	for _, target := range referenceIDs {
		resolved["@"+target] = target
	}
	for _, target := range resolved {
		var ok int
		err = tx.QueryRow("SELECT 1 FROM nodes WHERE id=? AND deleted=0", target).Scan(&ok)
		if err == sql.ErrNoRows {
			return fmt.Errorf("%w: reference target %s", ErrNotFound, target)
		}
		if err != nil {
			return err
		}
	}
	if _, err = tx.Exec("DELETE FROM node_tags WHERE node_id=?", id); err != nil {
		return err
	}
	for _, tag := range tags {
		if _, err = tx.Exec("INSERT INTO node_tags(node_id,tag) VALUES(?,?)", id, tag); err != nil {
			return err
		}
	}
	if _, err = tx.Exec("DELETE FROM node_references WHERE source_id=?", id); err != nil {
		return err
	}
	for token, target := range resolved {
		if _, err = tx.Exec("INSERT INTO node_references(source_id,token,target_id) VALUES(?,?,?)", id, token, target); err != nil {
			return err
		}
	}
	return nil
}

// A referenced subject label is literal text, not another capture command.
func (e *Engine) createConcept(tx *sql.Tx, label string) (Node, error) {
	if len(label) > 1024*1024 || strings.TrimSpace(label) == "" {
		return Node{}, fmt.Errorf("%w: invalid subject label", ErrInvalid)
	}
	if validDate(label) == nil {
		return e.ensureDay(tx, label)
	}
	id, err := newID()
	if err != nil {
		return Node{}, err
	}
	pos, err := positionFor(tx, "", Placement{}, "")
	if err != nil {
		return Node{}, err
	}
	at := now()
	if _, err = tx.Exec("INSERT INTO nodes(id,parent_id,position,text,revision,created_at,updated_at) VALUES(?,NULL,?,?,1,?,?)", id, pos, label, at, at); err != nil {
		return Node{}, err
	}
	n, err := getNode(tx, id, false)
	if err != nil {
		return n, err
	}
	if err = e.recordRevision(tx, n, "Created subject"); err != nil {
		return n, err
	}
	return n, nil
}
