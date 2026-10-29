package core

import (
	"encoding/base64"
	"strconv"
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

// encodeCursor returns the opaque cursor that resumes a list at position v. The
// position is a row id for a keyset list (events, learnings) or an offset for
// search. Base64url keeps it a token the client does not read.
func encodeCursor(v int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(v, 10)))
}

// decodeCursor reads a cursor from encodeCursor. An empty cursor is the first
// page and returns nil. A cursor that does not decode, or a negative position,
// is invalid.
func decodeCursor(cursor string) (*int64, error) {
	if cursor == "" {
		return nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, Invalid("cursor is not valid")
	}
	v, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil || v < 0 {
		return nil, Invalid("cursor is not valid")
	}
	return &v, nil
}
