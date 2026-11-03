package model

import "encoding/json"

type Attachment struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	Type     string `json:"type"`
	Size     int64  `json:"size"`
	URL      string `json:"url"`
}
type Relation struct {
	TargetID string `json:"targetId"`
	Type     string `json:"type"`
}
type Note struct {
	ID          string          `json:"id"`
	Content     string          `json:"content"`
	Title       string          `json:"title"`
	Tags        []string        `json:"tags"`
	CreatedAt   string          `json:"createdAt"`
	UpdatedAt   string          `json:"updatedAt"`
	State       string          `json:"state"`
	Pinned      bool            `json:"pinned"`
	Version     int64           `json:"version"`
	Source      string          `json:"source"`
	SourceID    string          `json:"sourceId,omitempty"`
	Attachments []Attachment    `json:"attachments"`
	Relations   []Relation      `json:"relations"`
	RawSource   json.RawMessage `json:"-"`
	Snippet     string          `json:"snippet,omitempty"`
	MatchType   string          `json:"matchType,omitempty"`
}
type Query struct {
	Pinned bool
	Q      string
	Tag    string
	State  string
	Limit  int
	Cursor string
	Sort   string
}
type Page struct {
	Notes      []Note  `json:"notes"`
	Total      int     `json:"total"`
	NextCursor string  `json:"nextCursor,omitempty"`
	TookMs     float64 `json:"tookMs"`
	Mode       string  `json:"mode"`
}
type Tag struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}
type Stats struct {
	Active      int `json:"active"`
	Archived    int `json:"archived"`
	Tags        int `json:"tags"`
	Attachments int `json:"attachments"`
}
