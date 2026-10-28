package store

import (
	"database/sql"
	"time"
)

// timeLayout is the stored form of every timestamp: UTC, fixed width, so that
// text comparison in SQL (keyset paging, ORDER BY) matches time order. A value
// of 0 is never stored; every time column is set by the caller.
const timeLayout = "2006-01-02T15:04:05.000000000Z"

// formatTime returns t in the stored form, converted to UTC.
func formatTime(t time.Time) string {
	return t.UTC().Format(timeLayout)
}

// parseTime reads a stored timestamp. It returns an error for text that is not
// in the stored form.
func parseTime(s string) (time.Time, error) {
	return time.Parse(timeLayout, s)
}

// timeOrNil returns the stored form of a nullable timestamp, or nil (SQL NULL).
func timeOrNil(t *time.Time) any {
	if t == nil {
		return nil
	}
	return formatTime(*t)
}

// nullTime reads a nullable stored timestamp. A NULL column gives a nil pointer.
func nullTime(ns sql.NullString) (*time.Time, error) {
	if !ns.Valid {
		return nil, nil
	}
	t, err := parseTime(ns.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// textOrNil stores an empty string as SQL NULL, for nullable text columns. A
// row type reads NULL back as "", so the two never differ in Go.
func textOrNil(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// nullString reads a nullable text column. NULL becomes "".
func nullString(ns sql.NullString) string {
	return ns.String
}

// rowScanner is the one method of *sql.Row and *sql.Rows the scan helpers use.
type rowScanner interface {
	Scan(dest ...any) error
}
