package core

import (
	"encoding/base64"
	"strings"
	"time"

	"github.com/djm56/cairn-memory/internal/store"
)

// pageSize returns the page size a paged call uses. Zero means def, the call's
// default. A negative size or one above MaxLimit is invalid (CONTRACT §1.3).
func pageSize(limit, def int) (int, error) {
	switch {
	case limit == 0:
		return def, nil
	case limit < 0 || limit > MaxLimit:
		return 0, Invalid("limit must be between 1 and %d", MaxLimit)
	}
	return limit, nil
}

// encodeTaskCursor returns the opaque cursor that resumes a task list below
// key. The cursor is the key's time and id, base64url encoded, so a client
// treats it as a token.
func encodeTaskCursor(key store.TaskKey) string {
	raw := key.UpdatedAt.UTC().Format(time.RFC3339Nano) + "|" + key.ID
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// decodeTaskCursor reads a cursor from encodeTaskCursor. An empty cursor is the
// first page and returns nil. Anything else that does not decode is invalid.
func decodeTaskCursor(cursor string) (*store.TaskKey, error) {
	if cursor == "" {
		return nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, Invalid("cursor is not valid")
	}
	ts, id, ok := strings.Cut(string(raw), "|")
	if !ok {
		return nil, Invalid("cursor is not valid")
	}
	at, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return nil, Invalid("cursor is not valid")
	}
	return &store.TaskKey{UpdatedAt: at, ID: id}, nil
}
