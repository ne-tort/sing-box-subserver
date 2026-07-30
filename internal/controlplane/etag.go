//go:build with_controlplane

package controlplane

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
)

// okJSONETag writes {ok,data} with ETag: "sha256:<hex>". If If-None-Match matches, returns 304.
func okJSONETag(w http.ResponseWriter, r *http.Request, data any) {
	body, err := json.Marshal(map[string]any{"ok": true, "data": data})
	if err != nil {
		failJSON(w, 500, "internal", err.Error())
		return
	}
	sum := sha256.Sum256(body)
	etag := `"sha256:` + hex.EncodeToString(sum[:]) + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "private, must-revalidate")
	if etagMatch(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func etagMatch(header, etag string) bool {
	header = strings.TrimSpace(header)
	if header == "" {
		return false
	}
	// Allow weak validators and comma-separated lists.
	for _, part := range strings.Split(header, ",") {
		p := strings.TrimSpace(part)
		p = strings.TrimPrefix(p, "W/")
		if p == etag || p == "*" {
			return true
		}
	}
	return false
}
