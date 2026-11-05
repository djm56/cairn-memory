// Package cliservice runs the conformance suite through the cairn binary (issue #88). It is test code
// only: the adapter exists to show that an agent reaching core through the CLI sees the behaviour the
// suite checks.
//
// The agent calls of core.Service run `cairn ... --json` as child processes against an in-process
// loopback server (api.Handler over a real core). The operator and web calls have no CLI verb, so the
// adapter does not run them: they are the HTTP client's, which the embedded *client.Client supplies, and
// they reach the same server. Those calls are:
//
//	LearningDecide, DirectiveRatify, DirectiveRetire, DirectiveReaffirm, ProjectSettingsSet,
//	ProjectList, TaskBrowse, DirectiveBrowse, Dashboard
//
// The agent calls the CLI runs are:
//
//	TaskCurrent, TaskStart, TaskGet, TaskList, TaskUpdate, TaskClose, StepPlan, StepAdd, StepUpdate,
//	EventLog, LearningAdd, LearningList, DirectiveList, Recall, Search
//
// Three things about the CLI shape the mapping, and each is a gap the suite can show:
//
//   - A failed call writes its error to stdout as one JSON object (the --json form), and exits with the
//     code of its kind (cli.ExitCodeFor). The adapter reads the kind, the message and the missing list
//     back into core's error, and checks that the exit code is the one the table gives that kind. A usage
//     error exits 2 with no object. The adapter reads it as core's KindInvalid only when its message names
//     a flag the call set; see usageNamesFlag.
//   - The CLI calls as an agent. It names itself cairn-cli, and it sends no session. The adapter does not
//     carry the caller's Client or Session, and it refuses a caller whose role is not agent, since
//     running that call as an agent would change its meaning.
//   - Some request shapes have no flag: an empty step reference, since an absent --after, --step or --step
//     means the default and not a reference, a learning list over all projects, a lone "-" as a text value
//     (the CLI reads it as standard input), and a nil step list in step plan. The adapter refuses each of
//     these rather than sending a different request. A step named by id is sent as --after-id, --step-id.
package cliservice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/api"
	"github.com/djm56/cairn-memory/internal/cli"
	"github.com/djm56/cairn-memory/internal/client"
	"github.com/djm56/cairn-memory/internal/core"
)

// cairnBin is the path of the cairn binary that TestMain builds once for the package run.
var cairnBin string

// testKey is the API key the CLI sends. The in-process server names the caller from the headers and
// does not check the key, so any value serves. It has no spaces or control characters.
const testKey = "conformance-key"

// TestMain builds the cairn binary into a temporary directory, once for the whole package run, and
// removes the directory when the run ends. The child processes all run that one binary.
func TestMain(m *testing.M) {
	code := func() int {
		dir, err := os.MkdirTemp("", "cairn-cli-*")
		if err != nil {
			fmt.Fprintf(os.Stderr, "cliservice: make a temporary directory: %v\n", err)
			return 1
		}
		defer os.RemoveAll(dir)
		cairnBin = filepath.Join(dir, "cairn")
		out, err := exec.Command(goTool(), "build", "-o", cairnBin, "../../../cmd/cairn").CombinedOutput()
		if err != nil {
			fmt.Fprintf(os.Stderr, "cliservice: go build cmd/cairn: %v\n%s", err, out)
			return 1
		}
		return m.Run()
	}()
	os.Exit(code)
}

// goTool returns the go command to build with: the one on PATH, else the one in GOROOT.
func goTool() string {
	if p, err := exec.LookPath("go"); err == nil {
		return p
	}
	return filepath.Join(runtime.GOROOT(), "bin", "go")
}

// Service is a core.Service whose agent calls run the cairn binary. The embedded client supplies the
// operator and web calls over HTTP, against the same server; see the package comment for the list.
type Service struct {
	*client.Client
	url string // the in-process server the binary calls
	dir string // the working directory of each child: it holds no .cairn.toml, so no file is found
}

var _ core.Service = (*Service)(nil)

// New returns a Service over svc. It serves svc on a loopback server for the life of the test, and
// every agent call runs the cairn binary against that server.
func New(t *testing.T, svc core.Service) *Service {
	t.Helper()
	srv := httptest.NewServer(api.Handler(svc, api.UnauthenticatedHeaderCaller, nil))
	t.Cleanup(srv.Close)
	if !strings.HasPrefix(srv.URL, "http://127.0.0.1:") {
		t.Fatalf("test server at %s is not on loopback", srv.URL)
	}
	hc, err := client.NewWithKey(srv.URL, testKey, nil)
	if err != nil {
		t.Fatalf("client.NewWithKey: %v", err)
	}
	return &Service{Client: hc, url: srv.URL, dir: t.TempDir()}
}

// argv collects the arguments of one cairn call. A value the CLI cannot carry sets err, and the call
// is not made: a request is never sent in a different form.
type argv struct {
	args []string
	err  error
}

// newArgv starts a call with its words, and adds --json, which every verb the adapter runs takes.
func newArgv(words ...string) *argv {
	return &argv{args: append(append([]string{}, words...), "--json")}
}

// add appends words as they are.
func (a *argv) add(words ...string) { a.args = append(a.args, words...) }

// flag appends --name value for a flag that is not a text flag.
func (a *argv) flag(name, value string) {
	a.args = append(a.args, "--"+name, value)
}

// text appends --name value for a text flag. The CLI reads a lone dash on a text flag as standard
// input, so a dash cannot be the value, and that is refused.
func (a *argv) text(name, value string) {
	if value == "-" {
		a.inexpressible("--%s: the CLI reads a lone dash as standard input, so the value cannot be passed", name)
		return
	}
	a.args = append(a.args, "--"+name, value)
}

// agent adds --agent with the calling agent of ctx. An empty agent is left off, so the CLI refuses the
// write as a usage error, the way it refuses a write with no agent.
func (a *argv) agent(ctx context.Context) {
	if c, _ := core.CallerFrom(ctx); c.Agent != "" {
		a.flag("agent", c.Agent)
	}
}

// inexpressible records the first request the CLI has no flag for.
func (a *argv) inexpressible(format string, args ...any) {
	if a.err == nil {
		a.err = fmt.Errorf("cliservice: the CLI cannot carry this request: "+format, args...)
	}
}

// call runs a and decodes the answer as R. stdin, when not nil, is the standard input of the call, for
// the --json-input forms. The caller of ctx must be an agent: the CLI calls as one, and any other role
// would change the call's meaning, so it is refused here.
func call[R any](s *Service, ctx context.Context, a *argv, stdin []byte) (*R, error) {
	if a.err != nil {
		return nil, a.err
	}
	c, _ := core.CallerFrom(ctx)
	if c.Role != "" && c.Role != core.RoleAgent {
		return nil, fmt.Errorf("cliservice: the CLI calls as an agent and cannot carry role %q", c.Role)
	}
	// The CLI names itself cairn-cli and sends no session, so the caller's Client and Session are not
	// carried. A case that checks them fails, and is listed as a gap.
	env := []string{"HOME=" + s.dir, "CAIRN_SERVER=" + s.url, "CAIRN_API_KEY=" + testKey}
	if c.Project != "" {
		env = append(env, "CAIRN_PROJECT="+c.Project)
	}
	// The caller's agent goes in CAIRN_AGENT as well as in --agent where a verb takes one. Every verb checks
	// the variable, reads included, so a malformed agent is refused on a read as core refuses it. A recall
	// with no --agent takes the variable as its role, as core takes the caller's agent.
	if c.Agent != "" {
		env = append(env, "CAIRN_AGENT="+c.Agent)
	}
	cmd := exec.Command(cairnBin, a.args...)
	cmd.Dir = s.dir
	cmd.Env = env
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			return nil, fmt.Errorf("cliservice: run cairn: %w", err)
		}
		return nil, failure(exit.ExitCode(), a.args, stdout.Bytes(), stderr.String())
	}
	var out R
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		return nil, fmt.Errorf("cliservice: the CLI's answer is not the call's result: %w", err)
	}
	return &out, nil
}

// failure is the error of a call the CLI did not complete. A failed call answers with its error object on
// stdout, and that object's kind and missing list are core's: the error is the object's core.Error. Its exit
// code must be the one cli.ExitCodeFor gives the kind, so a table that drifts from the CLI fails here.
// Exit 2 is the CLI's usage class, which it reports before any call and with no object: see usageNamesFlag.
// A Go panic also exits 2, and the CLI prints it as a panic, so a panic is not read as a usage error.
func failure(code int, argv []string, stdout []byte, stderr string) error {
	msg := strings.TrimSuffix(stderr, "\n")
	if strings.HasPrefix(stderr, "panic:") || strings.Contains(stderr, "\ngoroutine ") {
		return fmt.Errorf("cliservice: the CLI crashed: %s", msg)
	}
	if code == cli.ExitUsage {
		if usageNamesFlag(argv, stderr) {
			return &core.Error{Kind: core.KindInvalid, Message: msg}
		}
		return fmt.Errorf("cliservice: the CLI refused its arguments with a usage error, which carries no kind: %s", msg)
	}
	var body api.ErrorBody
	if err := json.Unmarshal(stdout, &body); err != nil || body.Error == nil || body.Error.Kind == "" {
		return fmt.Errorf("cliservice: the CLI exit %d with no error object: %s", code, msg)
	}
	if want := cli.ExitCodeFor(body.Error.Kind); want != code {
		return fmt.Errorf("cliservice: the CLI exit %d for kind %q, which the exit table gives %d", code, body.Error.Kind, want)
	}
	return body.Error
}

// callerNameMarks are the words of the CLI's refusals of the calling agent's name: "no agent name" when the
// call has none, and "the name from" the flag or the variable when the name is refused. The caller's name is
// a value the case sets through the caller, so these refusals are about a value the call set, as a flag is.
var callerNameMarks = []string{"no agent name", "the name from "}

// usageNamesFlag reports whether a usage error is about a value the call set: its message names one of the
// flags in argv, or it is one of the caller's name refusals (callerNameMarks). The adapter counts such a
// refusal as core's KindInvalid, since the CLI refuses the value before the call, as core refuses it as
// invalid. A usage error that names neither is not counted, so a case cannot pass on a request the adapter
// built wrong. The flag package's own parse errors end in "for the flags" and are never counted: they name
// the argument the adapter mis-built.
func usageNamesFlag(argv []string, stderr string) bool {
	if strings.Contains(stderr, "for the flags") {
		return false
	}
	for _, mark := range callerNameMarks {
		if strings.Contains(stderr, mark) {
			return true
		}
	}
	for _, word := range argv {
		if strings.HasPrefix(word, "--") && word != "--json" && strings.Contains(stderr, word) {
			return true
		}
	}
	return false
}

// startInput is the --json-input body of task start: the criteria, the key files and the plan.
type startInput struct {
	Criteria []string         `json:"criteria"`
	KeyFiles []core.TaskFile  `json:"key_files"`
	Steps    []core.StepInput `json:"steps"`
}

// closeInput is the --json-input body of task close: the criteria results, the lessons, the mistakes
// and the deliverables. A nil list is sent as null, which the CLI reads as absent, and an empty list is
// sent as [], which it reads as none.
type closeInput struct {
	Criteria     []core.CriterionOutcome `json:"criteria"`
	Lessons      []core.Lesson           `json:"lessons"`
	Mistakes     []core.Mistake          `json:"mistakes"`
	Deliverables []core.TaskFile         `json:"deliverables"`
}

// TaskCurrent runs `cairn task current`, with --agent when the caller has one.
func (s *Service) TaskCurrent(ctx context.Context) (*core.TaskCurrent, error) {
	a := newArgv("task", "current")
	a.agent(ctx)
	return call[core.TaskCurrent](s, ctx, a, nil)
}

// TaskStart runs `cairn task start` with the brief as flags and the structured fields on standard input.
func (s *Service) TaskStart(ctx context.Context, req core.TaskStartRequest) (*core.TaskStartResult, error) {
	a := newArgv("task", "start")
	a.agent(ctx)
	a.text("title", req.Title)
	a.text("goal", req.Goal)
	if req.Context != "" {
		a.text("context", req.Context)
	}
	if req.Constraints != "" {
		a.text("constraints", req.Constraints)
	}
	if req.Priority != "" {
		a.flag("priority", string(req.Priority))
	}
	if req.Queue {
		a.add("--queue")
	}
	a.add("--json-input")
	body, err := json.Marshal(startInput{Criteria: req.Criteria, KeyFiles: req.KeyFiles, Steps: req.Steps})
	if err != nil {
		return nil, err
	}
	return call[core.TaskStartResult](s, ctx, a, body)
}

// TaskGet runs `cairn task get`. A step is named by number, with --step, or by id, with --step-id. A
// reference that gives both is sent as both, so the CLI refuses it as a usage error that names the flags.
// An empty reference has no flag, since an absent --step means the task, so it is refused.
func (s *Service) TaskGet(ctx context.Context, req core.TaskGetRequest) (*core.TaskDetail, error) {
	a := newArgv("task", "get", req.ID)
	if req.Section != "" {
		a.flag("section", string(req.Section))
	}
	if req.Limit != 0 {
		a.flag("limit", strconv.Itoa(req.Limit))
	}
	if req.Cursor != "" {
		a.flag("cursor", req.Cursor)
	}
	if req.Step != nil {
		if req.Step.Number == nil && req.Step.ID == nil {
			a.inexpressible("task get: an empty step reference has no flag, since an absent --step means the task")
		}
		if req.Step.Number != nil {
			a.flag("step", strconv.Itoa(*req.Step.Number))
		}
		if req.Step.ID != nil {
			a.flag("step-id", strconv.FormatInt(*req.Step.ID, 10))
		}
	}
	return call[core.TaskDetail](s, ctx, a, nil)
}

// TaskList runs `cairn task list`.
func (s *Service) TaskList(ctx context.Context, req core.TaskListRequest) (*core.TaskList, error) {
	a := newArgv("task", "list")
	for _, st := range req.Status {
		a.flag("status", string(st))
	}
	if req.Limit != 0 {
		a.flag("limit", strconv.Itoa(req.Limit))
	}
	if req.Cursor != "" {
		a.flag("cursor", req.Cursor)
	}
	return call[core.TaskList](s, ctx, a, nil)
}

// TaskUpdate runs `cairn task update`. A field the request leaves nil is not passed, so the CLI keeps
// it. The criteria and key files are sent on standard input, where a nil list is absent and an empty
// one replaces the list with none.
func (s *Service) TaskUpdate(ctx context.Context, req core.TaskUpdateRequest) (*core.Task, error) {
	a := newArgv("task", "update", req.ID)
	a.agent(ctx)
	if req.Status != nil {
		a.flag("status", string(*req.Status))
	}
	if req.Reason != "" {
		a.text("reason", req.Reason)
	}
	if req.Priority != nil {
		a.flag("priority", string(*req.Priority))
	}
	optional := []struct {
		name  string
		value *string
	}{
		{"owner", req.Owner}, {"next-action", req.NextAction}, {"blockers", req.Blockers},
		{"title", req.Title}, {"goal", req.Goal}, {"context", req.Context}, {"constraints", req.Constraints},
	}
	for _, o := range optional {
		if o.value != nil {
			a.text(o.name, *o.value)
		}
	}
	body := map[string]any{}
	if req.Criteria != nil {
		body["criteria"] = nonNil(*req.Criteria)
	}
	if req.KeyFiles != nil {
		body["key_files"] = nonNil(*req.KeyFiles)
	}
	var stdin []byte
	if len(body) > 0 {
		a.add("--json-input")
		var err error
		if stdin, err = json.Marshal(body); err != nil {
			return nil, err
		}
	}
	return call[core.Task](s, ctx, a, stdin)
}

// nonNil returns s, or an empty slice when s is nil. A pointer to a nil list means "replace with none",
// and an empty slice sends that as [], where null would read as absent.
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// TaskClose runs `cairn task close`, with the lists on standard input.
func (s *Service) TaskClose(ctx context.Context, req core.TaskCloseRequest) (*core.Task, error) {
	a := newArgv("task", "close", req.ID)
	a.agent(ctx)
	a.flag("outcome", string(req.Outcome))
	a.text("summary", req.Summary)
	if req.NoLessonsReason != "" {
		a.text("no-lessons-reason", req.NoLessonsReason)
	}
	a.add("--json-input")
	body, err := json.Marshal(closeInput{Criteria: req.Criteria, Lessons: req.Lessons, Mistakes: req.Mistakes, Deliverables: req.Deliverables})
	if err != nil {
		return nil, err
	}
	return call[core.Task](s, ctx, a, body)
}

// StepPlan runs `cairn step plan` with the new steps on standard input. The CLI refuses a plan with no
// steps list, so a nil list reaches it as null and is refused there.
func (s *Service) StepPlan(ctx context.Context, req core.StepPlanRequest) (*core.StepList, error) {
	a := newArgv("step", "plan", req.TaskID)
	a.agent(ctx)
	a.add("--json-input")
	body, err := json.Marshal(struct {
		Steps []core.StepInput `json:"steps"`
	}{req.Steps})
	if err != nil {
		return nil, err
	}
	return call[core.StepList](s, ctx, a, body)
}

// StepAdd runs `cairn step add`. --after takes a step number and --after-id a step id; a reference with both
// is sent as both, so the CLI refuses it as a usage error that names the flags. An empty reference has no
// flag, since an absent --after means the step goes last, so it is refused. Number 0 is "first", as the CLI reads it.
func (s *Service) StepAdd(ctx context.Context, req core.StepAddRequest) (*core.Step, error) {
	a := newArgv("step", "add", req.TaskID)
	a.agent(ctx)
	a.text("title", req.Title)
	if req.After != nil {
		if req.After.Number == nil && req.After.ID == nil {
			a.inexpressible("step add: an empty after reference has no flag, since an absent --after means the step goes last")
		}
		if req.After.Number != nil {
			a.flag("after", strconv.Itoa(*req.After.Number))
		}
		if req.After.ID != nil {
			a.flag("after-id", strconv.FormatInt(*req.After.ID, 10))
		}
	}
	if req.Agent != "" {
		a.flag("assignee", req.Agent)
	}
	return call[core.Step](s, ctx, a, nil)
}

// StepUpdate runs `cairn step update`. The step is the number as the second positional, or the id as
// --step-id. A reference that gives both, or neither, is passed as given, so the CLI refuses it as a
// usage error, as core refuses it as invalid.
func (s *Service) StepUpdate(ctx context.Context, req core.StepUpdateRequest) (*core.Step, error) {
	a := newArgv("step", "update", req.TaskID)
	if req.Step.Number != nil {
		a.add(strconv.Itoa(*req.Step.Number))
	}
	if req.Step.ID != nil {
		a.flag("step-id", strconv.FormatInt(*req.Step.ID, 10))
	}
	a.agent(ctx)
	a.flag("action", string(req.Action))
	if req.Summary != "" {
		a.text("summary", req.Summary)
	}
	if req.Agent != nil {
		a.flag("assignee", *req.Agent)
	}
	return call[core.Step](s, ctx, a, nil)
}

// EventLog runs `cairn event log`. The step is by number or by id; a reference with neither is refused,
// since the CLI would read it as an event on the task.
func (s *Service) EventLog(ctx context.Context, req core.EventLogRequest) (*core.Event, error) {
	a := newArgv("event", "log", req.TaskID)
	a.agent(ctx)
	a.flag("kind", string(req.Kind))
	a.text("text", req.Text)
	if req.Step != nil {
		if req.Step.Number == nil && req.Step.ID == nil {
			a.inexpressible("an empty step reference")
		}
		if req.Step.Number != nil {
			a.flag("step", strconv.Itoa(*req.Step.Number))
		}
		if req.Step.ID != nil {
			a.flag("step-id", strconv.FormatInt(*req.Step.ID, 10))
		}
	}
	if len(req.Data) > 0 {
		a.text("data", string(req.Data))
	}
	return call[core.Event](s, ctx, a, nil)
}

// LearningAdd runs `cairn learning add`.
func (s *Service) LearningAdd(ctx context.Context, req core.LearningAddRequest) (*core.Learning, error) {
	a := newArgv("learning", "add")
	a.agent(ctx)
	a.flag("kind", string(req.Kind))
	a.text("title", req.Title)
	a.text("body", req.Body)
	if req.TaskID != "" {
		a.flag("task-id", req.TaskID)
	}
	if req.Target != "" {
		a.text("target", req.Target)
	}
	if req.Rationale != "" {
		a.text("rationale", req.Rationale)
	}
	if req.Example != "" {
		a.text("example", req.Example)
	}
	return call[core.Learning](s, ctx, a, nil)
}

// LearningList runs `cairn learning list`. The CLI lists one project at a time, so a request for all
// projects is refused.
func (s *Service) LearningList(ctx context.Context, req core.LearningListRequest) (*core.LearningList, error) {
	if req.AllProjects {
		return nil, fmt.Errorf("cliservice: the CLI cannot carry this request: learning list lists one project; all_projects is not offered")
	}
	a := newArgv("learning", "list")
	for _, st := range req.Status {
		a.flag("status", string(st))
	}
	for _, k := range req.Kind {
		a.flag("kind", string(k))
	}
	if req.Target != "" {
		a.flag("target", req.Target)
	}
	if req.Limit != 0 {
		a.flag("limit", strconv.Itoa(req.Limit))
	}
	if req.Cursor != "" {
		a.flag("cursor", req.Cursor)
	}
	return call[core.LearningList](s, ctx, a, nil)
}

// DirectiveList runs `cairn directive list`, with --agent when the request names a role.
func (s *Service) DirectiveList(ctx context.Context, req core.DirectiveListRequest) (*core.DirectiveList, error) {
	a := newArgv("directive", "list")
	if req.Agent != "" {
		a.flag("agent", req.Agent)
	}
	return call[core.DirectiveList](s, ctx, a, nil)
}

// Recall runs `cairn recall`. The agent is the one the request names, as the CLI reads it.
func (s *Service) Recall(ctx context.Context, req core.RecallRequest) (*core.RecallResult, error) {
	a := newArgv("recall")
	a.text("goal", req.Goal)
	if req.Agent != "" {
		a.flag("agent", req.Agent)
	}
	if req.Limit != 0 {
		a.flag("limit", strconv.Itoa(req.Limit))
	}
	return call[core.RecallResult](s, ctx, a, nil)
}

// Search runs `cairn search`.
func (s *Service) Search(ctx context.Context, req core.SearchRequest) (*core.SearchResult, error) {
	a := newArgv("search")
	a.text("query", req.Query)
	for _, k := range req.Kinds {
		a.flag("kind", string(k))
	}
	if req.AllProjects {
		a.add("--all-projects")
	}
	if req.Limit != 0 {
		a.flag("limit", strconv.Itoa(req.Limit))
	}
	if req.Cursor != "" {
		a.flag("cursor", req.Cursor)
	}
	return call[core.SearchResult](s, ctx, a, nil)
}
