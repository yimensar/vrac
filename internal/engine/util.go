package engine

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"time"
)

func encodeCursor(v any) string {
	b, _ := json.Marshal(v)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(s string, v any) error {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || json.Unmarshal(b, v) != nil {
		return invalid("malformed cursor")
	}
	return nil
}

func validDate(s string) error {
	if _, err := time.Parse(dateLayout, s); err != nil {
		return invalid("date %q is not YYYY-MM-DD", s)
	}
	return nil
}

// validMoment accepts YYYY-MM-DD or YYYY-MM-DDTHH:MM local times.
func validMoment(s string) error {
	if _, err := time.Parse(dateLayout, s); err == nil {
		return nil
	}
	if _, err := time.Parse("2006-01-02T15:04", s); err == nil {
		return nil
	}
	return invalid("time %q is not YYYY-MM-DD or YYYY-MM-DDTHH:MM", s)
}

func hashJSON(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
