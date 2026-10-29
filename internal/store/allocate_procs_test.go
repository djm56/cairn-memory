package store

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// helperDBEnv names the database file. Setting it turns a test binary into the
// allocation helper that TestAllocateTaskIDAcrossProcesses starts.
const helperDBEnv = "CAIRN_TEST_ALLOC_DB"

// allocPerProcess is how many task ids each process allocates.
const allocPerProcess = 50

// allocDay is the UTC day the ids are allocated for.
var allocDay = time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)

// allocProject is the project the helper processes insert their tasks into.
const allocProject = "acme-api"

// TestAllocateTaskIDAcrossProcesses checks issue #11: two separate OS processes
// that open the same database file at once allocate distinct task ids. The
// processes are copies of this test binary, started with
// TestHelperProcessAllocate. Each one opens the file, reports ready on its
// stdout, and waits on its stdin. Once both are ready the parent sends go to
// each, so the allocations overlap in practice: both helpers are blocked at the
// same point when the go lines are written. Nothing enforces the overlap. The test
// fails unless both processes succeed and all the ids are distinct.
func TestAllocateTaskIDAcrossProcesses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cairn.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	seedProjects(t, s, allocProject)

	const procs = 2
	children := make([]*allocChild, 0, procs)
	defer func() {
		for _, c := range children {
			c.kill()
		}
	}()
	for i := 0; i < procs; i++ {
		c, err := startAllocChild(path)
		if err != nil {
			t.Fatalf("start helper %d: %v", i, err)
		}
		children = append(children, c)
	}

	for i, c := range children {
		if err := c.readLine("ready"); err != nil {
			t.Fatalf("helper %d: %v", i, err)
		}
	}
	for i, c := range children {
		if _, err := io.WriteString(c.stdin, "go\n"); err != nil {
			// The helper died before it read go. Its output says why.
			_, rerr := c.collect()
			t.Fatalf("helper %d: send go: %v; helper: %v", i, err, rerr)
		}
	}

	seen := map[string]int{}
	for i, c := range children {
		ids, err := c.collect()
		if err != nil {
			t.Fatalf("helper %d: %v", i, err)
		}
		if len(ids) != allocPerProcess {
			t.Errorf("helper %d allocated %d ids, want %d", i, len(ids), allocPerProcess)
		}
		for _, id := range ids {
			seen[id]++
		}
	}
	for id, n := range seen {
		if n > 1 {
			t.Errorf("task id %s allocated %d times across processes", id, n)
		}
	}
	if want := procs * allocPerProcess; len(seen) != want {
		t.Errorf("distinct task ids = %d, want %d", len(seen), want)
	}
}

// TestHelperProcessAllocate is not a test of its own. TestAllocateTaskIDAcrossProcesses
// runs it in a child process, with helperDBEnv set. Run normally it skips.
func TestHelperProcessAllocate(t *testing.T) {
	path := os.Getenv(helperDBEnv)
	if path == "" {
		t.Skip("helper process for TestAllocateTaskIDAcrossProcesses")
	}
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	fmt.Fprintln(os.Stdout, "ready")
	in := bufio.NewReader(os.Stdin)
	if _, err := in.ReadString('\n'); err != nil {
		t.Fatalf("wait for go: %v", err)
	}

	ctx := context.Background()
	for i := 0; i < allocPerProcess; i++ {
		var id string
		err := s.WithTx(ctx, func(q Querier) error {
			var err error
			id, err = AllocateTaskID(ctx, q, allocDay)
			if err != nil {
				return err
			}
			return InsertTask(ctx, q, newTask(id, allocProject, allocDay))
		})
		if err != nil {
			t.Fatalf("allocate %d: %v", i, err)
		}
		fmt.Fprintln(os.Stdout, "id", id)
	}
	fmt.Fprintln(os.Stdout, "done")
}

// allocChild is one helper process and its pipes.
type allocChild struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	stderr *bytes.Buffer
	timer  *time.Timer

	// reaped runs cmd.Wait exactly once. waitErr is its result.
	reaped  sync.Once
	waitErr error
}

// startAllocChild starts a copy of the test binary that runs only
// TestHelperProcessAllocate against path. A watchdog kills it after a minute, so
// a hang fails the test rather than stalling the run.
func startAllocChild(path string) (*allocChild, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(self, "-test.run=^TestHelperProcessAllocate$")
	cmd.Env = append(os.Environ(), helperDBEnv+"="+path)
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	c := &allocChild{cmd: cmd, stdin: in, stdout: bufio.NewReader(out), stderr: stderr}
	c.timer = time.AfterFunc(time.Minute, c.kill)
	return c, nil
}

// readLine reads one line from the child and fails unless it is want. On a read
// error it kills and reaps the child first, so the stderr it reports is complete.
func (c *allocChild) readLine(want string) error {
	line, err := c.stdout.ReadString('\n')
	if err != nil {
		c.kill()
		return fmt.Errorf("read %q: %v (stderr: %s)", want, err, strings.TrimSpace(c.stderr.String()))
	}
	if got := strings.TrimSpace(line); got != want {
		return fmt.Errorf("got line %q, want %q", got, want)
	}
	return nil
}

// collect reads the child's ids up to its done line, then waits for it to exit.
// It fails if the child exits with an error, or ends without done. The lines
// that are not ids are the test's own failure messages, so they go in the error.
// On a read error it kills and reaps the child before it reads stderr.
func (c *allocChild) collect() ([]string, error) {
	var ids, other []string
	for {
		line, err := c.stdout.ReadString('\n')
		if err != nil {
			c.kill()
			return nil, fmt.Errorf("after %d ids: %v; child said %q (stderr: %s)",
				len(ids), err, other, strings.TrimSpace(c.stderr.String()))
		}
		line = strings.TrimSpace(line)
		if line == "done" {
			break
		}
		if id, ok := strings.CutPrefix(line, "id "); ok {
			ids = append(ids, id)
		} else if line != "" {
			other = append(other, line)
		}
	}
	// The child said done and should exit on its own, so this only waits. A
	// kill here could turn a clean exit into a false failure.
	if err := c.wait(); err != nil {
		return nil, fmt.Errorf("exit: %v; child said %q (stderr: %s)",
			err, other, strings.TrimSpace(c.stderr.String()))
	}
	return ids, nil
}

// kill stops the child if it is still running, then reaps it. It is safe to
// call more than once, and after collect.
func (c *allocChild) kill() {
	if c.cmd.Process != nil {
		c.cmd.Process.Kill()
	}
	c.wait()
}

// wait stops the watchdog and runs cmd.Wait, once. Wait also waits for exec to
// finish copying stderr, so stderr is complete once wait has returned. Later
// calls return the first result.
func (c *allocChild) wait() error {
	c.reaped.Do(func() {
		c.timer.Stop()
		c.waitErr = c.cmd.Wait()
	})
	return c.waitErr
}
