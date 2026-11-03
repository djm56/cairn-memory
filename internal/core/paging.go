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

// encodeTimeKey returns the opaque cursor that resumes a list below the position
// (at, id): a time and an id, base64url encoded, so a client treats it as a token.
// The task list and the directive browse page this way.
func encodeTimeKey(at time.Time, id string) string {
	raw := at.UTC().Format(time.RFC3339Nano) + "|" + id
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// decodeTimeKey reads a cursor from encodeTimeKey. An empty cursor is the first page,
// and reports ok false with no error. Anything else that does not decode is invalid.
func decodeTimeKey(cursor string) (at time.Time, id string, ok bool, err error) {
	if cursor == "" {
		return time.Time{}, "", false, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, "", false, Invalid("cursor is not valid")
	}
	ts, id, found := strings.Cut(string(raw), "|")
	if !found {
		return time.Time{}, "", false, Invalid("cursor is not valid")
	}
	at, err = time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return time.Time{}, "", false, Invalid("cursor is not valid")
	}
	return at, id, true, nil
}

// encodeTaskCursor returns the opaque cursor that resumes a task list below key.
func encodeTaskCursor(key store.TaskKey) string {
	return encodeTimeKey(key.UpdatedAt, key.ID)
}

// decodeTaskCursor reads a cursor from encodeTaskCursor. An empty cursor is the
// first page and returns nil. Anything else that does not decode is invalid.
func decodeTaskCursor(cursor string) (*store.TaskKey, error) {
	at, id, ok, err := decodeTimeKey(cursor)
	if err != nil || !ok {
		return nil, err
	}
	return &store.TaskKey{UpdatedAt: at, ID: id}, nil
}

// encodeDirectiveCursor returns the opaque cursor that resumes a directive browse
// below key.
func encodeDirectiveCursor(key store.DirectiveKey) string {
	return encodeTimeKey(key.At, key.Code)
}

// decodeDirectiveCursor reads a cursor from encodeDirectiveCursor. An empty cursor is
// the first page and returns nil. Anything else that does not decode is invalid.
func decodeDirectiveCursor(cursor string) (*store.DirectiveKey, error) {
	at, code, ok, err := decodeTimeKey(cursor)
	if err != nil || !ok {
		return nil, err
	}
	return &store.DirectiveKey{At: at, Code: code}, nil
}

// encodeNameCursor returns the opaque cursor that resumes a project list below name.
func encodeNameCursor(name string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(name))
}

// decodeNameCursor reads a cursor from encodeNameCursor. An empty cursor is the first
// page and returns nil. A cursor that does not decode, or whose name is not a project
// name, is invalid.
func decodeNameCursor(cursor string) (*string, error) {
	if cursor == "" {
		return nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || !ValidProjectName(string(raw)) {
		return nil, Invalid("cursor is not valid")
	}
	name := string(raw)
	return &name, nil
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
