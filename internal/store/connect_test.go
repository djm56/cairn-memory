package store

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// codedError stands in for the driver's error. Like *sqlite.Error, it reports a
// SQLite result code through Code.
type codedError struct {
	code int
}

func (e codedError) Error() string { return fmt.Sprintf("sqlite result code %d", e.code) }

// Code returns the result code, as the driver's error does.
func (e codedError) Code() int { return e.code }

// TestIsBusy checks the classifier on its own: the primary and an extended BUSY
// code count, other codes do not, and wrapping does not hide the code.
func TestIsBusy(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"SQLITE_BUSY", codedError{5}, true},
		{"SQLITE_BUSY_SNAPSHOT is an extended BUSY code", codedError{261}, true},
		{"SQLITE_ERROR", codedError{1}, false},
		{"plain error with no code", errors.New("boom"), false},
		{"nil", nil, false},
		{"wrapped SQLITE_BUSY", fmt.Errorf("open: %w", codedError{5}), true},
		{"wrapped SQLITE_ERROR", fmt.Errorf("open: %w", codedError{1}), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isBusy(tc.err); got != tc.want {
				t.Errorf("isBusy(%v) = %t, want %t", tc.err, got, tc.want)
			}
		})
	}
}

// scenarioDriver is a database/sql driver whose Open fails on a schedule named
// by the DSN, so connect can be tested with no real database. A DSN is
// "<scenario>#<n>". The scenario is "busy:K" (the first K opens return BUSY, then
// they succeed), "busy" (every open returns BUSY), "plain" (every open returns a
// non-BUSY error) or "ok". The "#<n>" suffix makes each test's DSN its own, so
// its open count starts at zero.
type scenarioDriver struct {
	mu    sync.Mutex
	opens map[string]int
}

// scenarios is the driver registered as "store-scenario" for the tests below.
var scenarios = &scenarioDriver{opens: map[string]int{}}

// dsnSeq numbers the DSNs the tests hand out.
var dsnSeq atomic.Int64

func init() {
	sql.Register("store-scenario", scenarios)
}

// Open implements driver.Driver. It counts the open against dsn and fails it
// as the scenario says. An unknown scenario succeeds.
func (d *scenarioDriver) Open(dsn string) (driver.Conn, error) {
	d.mu.Lock()
	d.opens[dsn]++
	n := d.opens[dsn]
	d.mu.Unlock()

	scenario, _, _ := strings.Cut(dsn, "#")
	switch {
	case scenario == "plain":
		return nil, errors.New("not a busy error")
	case scenario == "busy":
		return nil, codedError{5}
	case strings.HasPrefix(scenario, "busy:"):
		k, err := strconv.Atoi(strings.TrimPrefix(scenario, "busy:"))
		if err != nil {
			return nil, fmt.Errorf("scenario %q: %w", scenario, err)
		}
		if n <= k {
			return nil, codedError{5}
		}
	}
	return scenarioConn{}, nil
}

// count returns how many times Open was called for dsn.
func (d *scenarioDriver) count(dsn string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.opens[dsn]
}

// scenarioConn is the connection a successful Open returns. connect only pings,
// so none of its methods is ever used.
type scenarioConn struct{}

// Prepare is not used by connect.
func (scenarioConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("scenario: Prepare not implemented")
}

// Close does nothing; the scenario holds no resource.
func (scenarioConn) Close() error { return nil }

// Begin is not used by connect.
func (scenarioConn) Begin() (driver.Tx, error) {
	return nil, errors.New("scenario: Begin not implemented")
}

// newScenarioDB returns a pool on the scenario driver, with a DSN unique to this
// call, and the DSN so the test can read its open count.
func newScenarioDB(t *testing.T, scenario string) (*sql.DB, string) {
	t.Helper()
	dsn := fmt.Sprintf("%s#%d", scenario, dsnSeq.Add(1))
	db, err := sql.Open("store-scenario", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db, dsn
}

// TestConnectRetriesBusy checks that connect keeps trying through BUSY refusals
// and succeeds on the first attempt that is not refused.
func TestConnectRetriesBusy(t *testing.T) {
	db, dsn := newScenarioDB(t, "busy:2")
	if err := connect(db); err != nil {
		t.Fatalf("connect: %v, want success after two BUSY refusals", err)
	}
	if got := scenarios.count(dsn); got != 3 {
		t.Errorf("opens = %d, want 3", got)
	}
}

// TestConnectGivesUpOnPersistentBusy checks that a BUSY which outlasts the
// retries is returned, and that connect made exactly one attempt per retry.
func TestConnectGivesUpOnPersistentBusy(t *testing.T) {
	db, dsn := newScenarioDB(t, "busy")
	err := connect(db)
	if !isBusy(err) {
		t.Fatalf("connect error = %v, want SQLITE_BUSY returned", err)
	}
	if got := scenarios.count(dsn); got != connectRetries {
		t.Errorf("opens = %d, want %d (one per attempt)", got, connectRetries)
	}
}

// TestConnectReturnsOtherErrorAtOnce checks that an error which is not BUSY is
// returned on the first attempt, with no retry.
func TestConnectReturnsOtherErrorAtOnce(t *testing.T) {
	db, dsn := newScenarioDB(t, "plain")
	err := connect(db)
	if err == nil || isBusy(err) {
		t.Fatalf("connect error = %v, want a non-BUSY error", err)
	}
	if got := scenarios.count(dsn); got != 1 {
		t.Errorf("opens = %d, want 1", got)
	}
}
