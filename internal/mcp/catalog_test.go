package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCatalogMirrorsChatGPTSecurityPolicies(t *testing.T) {
	h := catalogMetadata(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"search_notes","inputSchema":{"type":"object"},"_meta":{"securitySchemes":[{"type":"oauth2","scopes":["notes:read"]}]}}]}}`))
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "http://localhost/mcp", strings.NewReader(`{}`)))
	var decoded struct {
		Result struct {
			Tools []map[string]any `json:"tools"`
		} `json:"result"`
	}
	if json.Unmarshal(w.Body.Bytes(), &decoded) != nil {
		t.Fatal("invalid catalog JSON")
	}
	if _, ok := decoded.Result.Tools[0]["securitySchemes"]; !ok {
		t.Fatal("missing top-level securitySchemes")
	}
}
