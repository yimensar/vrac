package mcp

import (
	"bytes"
	"encoding/json"
	"net/http"
)

// The MCP Go SDK models standard Tool fields, while ChatGPT also consumes the
// extension securitySchemes at the top level. Keep one policy declaration in
// Tool.Meta, and mirror it into JSON catalog responses at the transport edge.
// Normal tool calls pass through unchanged. Streamable HTTP POST uses JSON mode.
func catalogMetadata(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			next.ServeHTTP(w, r)
			return
		}
		recorded := &bufferedResponse{header: make(http.Header)}
		next.ServeHTTP(recorded, r)
		body := recorded.body.Bytes()
		if bytes.Contains(body, []byte(`"inputSchema"`)) {
			var response any
			if json.Unmarshal(body, &response) == nil && mirrorPolicies(response) {
				if encoded, err := json.Marshal(response); err == nil {
					body = encoded
					recorded.header.Del("Content-Length")
				}
			}
		}
		for key, values := range recorded.header {
			w.Header()[key] = values
		}
		status := recorded.status
		if status == 0 {
			status = 200
		}
		w.WriteHeader(status)
		_, _ = w.Write(body)
	})
}

type bufferedResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (r *bufferedResponse) Header() http.Header { return r.header }
func (r *bufferedResponse) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
}
func (r *bufferedResponse) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = 200
	}
	return r.body.Write(b)
}
func mirrorPolicies(value any) bool {
	changed := false
	switch v := value.(type) {
	case map[string]any:
		if _, tool := v["inputSchema"]; tool {
			if meta, ok := v["_meta"].(map[string]any); ok {
				if policy, ok := meta["securitySchemes"]; ok {
					v["securitySchemes"] = policy
					changed = true
				}
			}
		}
		for _, child := range v {
			if mirrorPolicies(child) {
				changed = true
			}
		}
	case []any:
		for _, child := range v {
			if mirrorPolicies(child) {
				changed = true
			}
		}
	}
	return changed
}
