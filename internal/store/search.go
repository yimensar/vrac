package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/yimensar/vrac/internal/model"
)

type cursor struct {
	ID        string  `json:"id"`
	CreatedAt string  `json:"createdAt"`
	Pinned    bool    `json:"pinned"`
	Score     float64 `json:"score"`
	Query     string  `json:"query"`
}

var stopwords = map[string]bool{}

func init() {
	for _, s := range strings.Fields("a ai au aux avec ce ces comment dans de des du en est et faire fait il je la le les leur ma mais mes mon ne nos notre nous on ou par pas plus pour pourquoi que quel quelle quelles quels qui sa se ses son sont sur ta te tes toi ton tu un une vos votre vous was what when where which who why how the an to of for in is are and or from with can do does find about") {
		stopwords[s] = true
	}
}

var queryAliases = map[string][]string{
	"authentification": {"authentication", "auth"}, "authentication": {"authentification", "auth"}, "auth": {"authentication", "authentification"},
	"deploiement": {"deployment", "deploy"}, "deployments": {"deploiement", "deployment", "deploy"}, "deployment": {"deploiement", "deploy"}, "deploy": {"deploiement", "deployment"},
	"sauvegarde": {"backup"}, "sauvegardes": {"backup", "backups"}, "backup": {"sauvegarde", "sauvegardes"}, "backups": {"backup", "sauvegarde"},
	"restauration": {"restore", "restoration"}, "restore": {"restauration", "restoration"},
	"erreur": {"error"}, "error": {"erreur"}, "recherche": {"search"}, "search": {"recherche"},
	"connexion": {"connection", "login"}, "connection": {"connexion", "login"},
	"facturation": {"billing", "invoice"}, "billing": {"facturation", "invoice"},
}

func normalize(s string) string {
	return strings.NewReplacer("é", "e", "è", "e", "ê", "e", "ë", "e", "à", "a", "â", "a", "ä", "a", "î", "i", "ï", "i", "ô", "o", "ö", "o", "ù", "u", "û", "u", "ü", "u", "ç", "c").Replace(strings.ToLower(s))
}
func ftsQuery(q string, broad bool) (string, bool) {
	terms := []string{}
	seen := map[string]bool{}
	expanded := false
	for _, t := range words(normalize(q)) {
		if stopwords[t] || len([]rune(t)) < 2 || seen[t] {
			continue
		}
		seen[t] = true
		group := []string{`"` + strings.ReplaceAll(t, `"`, `""`) + `"*`}
		if aliases := queryAliases[t]; len(aliases) > 0 {
			expanded = true
			for _, a := range aliases {
				group = append(group, `"`+a+`"*`)
			}
		}
		terms = append(terms, "("+strings.Join(group, " OR ")+")")
		if len(terms) == 16 {
			break
		}
	}
	if len(terms) == 0 {
		for _, t := range words(q) {
			if t != "" {
				terms = append(terms, `"`+strings.ReplaceAll(t, `"`, `""`)+`"`)
			}
		}
	}
	join := " AND "
	if broad {
		join = " OR "
	}
	return strings.Join(terms, join), expanded
}
func (s *Store) List(ctx context.Context, q model.Query) (model.Page, error) {
	start := time.Now()
	page := model.Page{Notes: []model.Note{}, Mode: "timeline"}
	if q.Limit <= 0 {
		q.Limit = 50
	}
	if q.Limit > 200 {
		q.Limit = 200
	}
	if q.State == "" {
		q.State = "active"
	}
	if q.Sort == "" {
		q.Sort = "recent"
	}
	if q.State != "active" && q.State != "archived" && q.State != "deleted" {
		return page, fmt.Errorf("%w: unknown state", ErrInvalid)
	}
	if q.Sort != "recent" && q.Sort != "oldest" {
		return page, fmt.Errorf("%w: unknown sort", ErrInvalid)
	}
	if len(q.Q) > 1000 || len(q.Tag) > 200 {
		return page, fmt.Errorf("%w: query too long", ErrInvalid)
	}
	q.Q = strings.TrimSpace(q.Q)
	signature := hash(q.Q + "\x00" + q.Tag + "\x00" + q.State + "\x00" + q.Sort + fmt.Sprint(q.Pinned))
	var after cursor
	if q.Cursor != "" {
		if len(q.Cursor) > 2048 {
			return page, fmt.Errorf("%w: invalid cursor", ErrInvalid)
		}
		b, err := base64.RawURLEncoding.DecodeString(q.Cursor)
		if err != nil || json.Unmarshal(b, &after) != nil || after.Query != signature {
			return page, fmt.Errorf("%w: cursor does not belong to this query", ErrInvalid)
		}
	}
	where := "n.state=?"
	args := []any{q.State}
	if q.Pinned {
		where += " AND n.pinned=1"
	}
	if q.Tag != "" {
		where += " AND EXISTS(SELECT 1 FROM note_tags t WHERE t.note_id=n.id AND t.tag=? COLLATE NOCASE)"
		args = append(args, strings.ToLower(q.Tag))
	}
	prefix := ""
	from := "notes n"
	score := "0.0"
	fts := ""
	var expanded bool
	if q.Q != "" {
		fts, expanded = ftsQuery(q.Q, false)
		page.Mode = "fulltext"
		if expanded {
			page.Mode = "fulltext-expanded"
		}
		if fts == "" {
			page.TookMs = float64(time.Since(start).Microseconds()) / 1000
			return page, nil
		}
		prefix = "WITH matches AS MATERIALIZED (SELECT rowid,bm25(notes_fts,6.0,1.0,3.0) AS score FROM notes_fts WHERE notes_fts MATCH ?) "
		from = "matches m JOIN notes n ON n.rowid=m.rowid"
		args = append([]any{fts}, args...)
		score = "m.score"
	}
	countSQL := "SELECT count(*) FROM notes n WHERE " + where
	if q.Q != "" {
		// Counting needs only matching row IDs. Do not materialize BM25
		// scores here: the page query computes each score once below.
		countSQL = "SELECT count(*) FROM notes_fts JOIN notes n ON n.rowid=notes_fts.rowid WHERE notes_fts MATCH ? AND " + where
	}
	if err := s.db.QueryRowContext(ctx, countSQL, args...).Scan(&page.Total); err != nil {
		return page, err
	}
	// All significant terms are preferred. Broad matching is explicitly labeled,
	// and only used when their conjunction produces no results.
	if q.Q != "" && page.Total == 0 {
		broad, _ := ftsQuery(q.Q, true)
		if broad != fts {
			args[0] = broad
			page.Mode = "fulltext-broad"
			if err := s.db.QueryRowContext(ctx, countSQL, args...).Scan(&page.Total); err != nil {
				return page, err
			}
		}
	}
	cursorWhere := ""
	queryArgs := append([]any{}, args...)
	order := "n.pinned DESC,n.created_sort DESC,n.id DESC"
	if q.Q != "" {
		order = "m.score ASC,n.created_sort DESC,n.id DESC"
		if q.Cursor != "" {
			cursorWhere = " AND (m.score>? OR (m.score=? AND (n.created_sort<? OR (n.created_sort=? AND n.id<?))))"
			queryArgs = append(queryArgs, after.Score, after.Score, sortTime(after.CreatedAt), sortTime(after.CreatedAt), after.ID)
		}
	} else {
		op := "<"
		if q.Sort == "oldest" {
			order = "n.pinned DESC,n.created_sort ASC,n.id ASC"
			op = ">"
		}
		if q.Cursor != "" {
			cursorWhere = " AND (n.pinned<? OR (n.pinned=? AND (n.created_sort" + op + "? OR (n.created_sort=? AND n.id" + op + "?))))"
			queryArgs = append(queryArgs, after.Pinned, after.Pinned, sortTime(after.CreatedAt), sortTime(after.CreatedAt), after.ID)
		}
	}
	queryArgs = append(queryArgs, q.Limit+1)
	rows, err := s.db.QueryContext(ctx, prefix+"SELECT "+strings.Replace(prefixedColumns("n."), "n.raw_source", "NULL", 1)+","+score+" FROM "+from+" WHERE "+where+cursorWhere+" ORDER BY "+order+" LIMIT ?", queryArgs...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	type scoredNote struct {
		n     model.Note
		score float64
	}
	found := []scoredNote{}
	for rows.Next() {
		var n model.Note
		var tags, attachments, relations string
		var raw []byte
		var rank float64
		if err = rows.Scan(&n.ID, &n.Content, &n.Title, &tags, &n.CreatedAt, &n.UpdatedAt, &n.State, &n.Pinned, &n.Version, &n.Source, &n.SourceID, &attachments, &relations, &raw, &rank); err != nil {
			return page, err
		}
		if err = json.Unmarshal([]byte(tags), &n.Tags); err != nil {
			return page, err
		}
		if err = json.Unmarshal([]byte(attachments), &n.Attachments); err != nil {
			return page, err
		}
		if err = json.Unmarshal([]byte(relations), &n.Relations); err != nil {
			return page, err
		}
		n.RawSource = raw
		if q.Q != "" {
			n.MatchType = page.Mode
			if strings.Contains(normalize(n.Content), normalize(q.Q)) {
				n.MatchType = "exact"
			}
			n.Snippet = makeSnippet(n.Content, q.Q)
		}
		found = append(found, scoredNote{n, rank})
	}
	if err = rows.Err(); err != nil {
		return page, err
	}
	if len(found) > q.Limit {
		last := found[q.Limit-1]
		after = cursor{ID: last.n.ID, CreatedAt: last.n.CreatedAt, Pinned: last.n.Pinned, Score: last.score, Query: signature}
		b, _ := json.Marshal(after)
		page.NextCursor = base64.RawURLEncoding.EncodeToString(b)
		found = found[:q.Limit]
	}
	for _, n := range found {
		page.Notes = append(page.Notes, n.n)
	}
	page.TookMs = float64(time.Since(start).Microseconds()) / 1000
	return page, nil
}
func makeSnippet(content, q string) string {
	r := []rune(content)
	norm := []rune(normalize(content))
	idx := 0
	for _, term := range words(normalize(q)) {
		if stopwords[term] {
			continue
		}
		if n := strings.Index(string(norm), term); n >= 0 {
			idx = len([]rune(string(norm)[:n]))
			break
		}
	}
	start := idx - 70
	if start < 0 {
		start = 0
	}
	end := start + 260
	if end > len(r) {
		end = len(r)
	}
	snippet := strings.ReplaceAll(string(r[start:end]), "\n", " ")
	if start > 0 {
		snippet = "…" + snippet
	}
	if end < len(r) {
		snippet += "…"
	}
	return snippet
}
