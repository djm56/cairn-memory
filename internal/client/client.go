// Package client is a core.Service that sends each call to a Cairn server over
// HTTP, in the wire form of internal/api (DESIGN.md §5.4). It holds no rules.
// The server applies them, and an error comes back as the core.Error the
// server returned, with the same kind, message and missing list.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/djm56/cairn-memory/internal/api"
	"github.com/djm56/cairn-memory/internal/core"
)

// Client is a core.Service over HTTP. Build one with New; the zero value has no
// server to call.
type Client struct {
	base string
	hc   *http.Client
}

var _ core.Service = (*Client)(nil)

// New returns a Client for the server at base, such as http://127.0.0.1:8787.
// base must be an absolute http or https URL with a host, and no query or
// fragment. hc is the HTTP client the calls use; nil means http.DefaultClient.
func New(base string, hc *http.Client) (*Client, error) {
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("client: base %q is not an http or https URL with a host", base)
	}
	if hc == nil {
		hc = http.DefaultClient
	}
	return &Client{base: strings.TrimRight(base, "/"), hc: hc}, nil
}

// TaskCurrent sends the task_current call.
func (c *Client) TaskCurrent(ctx context.Context) (*core.TaskCurrent, error) {
	return post[core.TaskCurrent](ctx, c, api.RouteTaskCurrent, struct{}{})
}

// TaskStart sends the task_start call.
func (c *Client) TaskStart(ctx context.Context, req core.TaskStartRequest) (*core.TaskStartResult, error) {
	return post[core.TaskStartResult](ctx, c, api.RouteTaskStart, req)
}

// TaskGet sends the task_get call.
func (c *Client) TaskGet(ctx context.Context, req core.TaskGetRequest) (*core.TaskDetail, error) {
	return post[core.TaskDetail](ctx, c, api.RouteTaskGet, req)
}

// TaskList sends the task_list call.
func (c *Client) TaskList(ctx context.Context, req core.TaskListRequest) (*core.TaskList, error) {
	return post[core.TaskList](ctx, c, api.RouteTaskList, req)
}

// TaskUpdate sends the task_update call.
func (c *Client) TaskUpdate(ctx context.Context, req core.TaskUpdateRequest) (*core.Task, error) {
	return post[core.Task](ctx, c, api.RouteTaskUpdate, req)
}

// TaskClose sends the task_close call.
func (c *Client) TaskClose(ctx context.Context, req core.TaskCloseRequest) (*core.Task, error) {
	return post[core.Task](ctx, c, api.RouteTaskClose, req)
}

// StepPlan sends the step_plan call.
func (c *Client) StepPlan(ctx context.Context, req core.StepPlanRequest) (*core.StepList, error) {
	return post[core.StepList](ctx, c, api.RouteStepPlan, req)
}

// StepAdd sends the step_add call.
func (c *Client) StepAdd(ctx context.Context, req core.StepAddRequest) (*core.Step, error) {
	return post[core.Step](ctx, c, api.RouteStepAdd, req)
}

// StepUpdate sends the step_update call.
func (c *Client) StepUpdate(ctx context.Context, req core.StepUpdateRequest) (*core.Step, error) {
	return post[core.Step](ctx, c, api.RouteStepUpdate, req)
}

// EventLog sends the event_log call.
func (c *Client) EventLog(ctx context.Context, req core.EventLogRequest) (*core.Event, error) {
	return post[core.Event](ctx, c, api.RouteEventLog, req)
}

// LearningAdd sends the learning_add call.
func (c *Client) LearningAdd(ctx context.Context, req core.LearningAddRequest) (*core.Learning, error) {
	return post[core.Learning](ctx, c, api.RouteLearningAdd, req)
}

// LearningList sends the learning_list call.
func (c *Client) LearningList(ctx context.Context, req core.LearningListRequest) (*core.LearningList, error) {
	return post[core.LearningList](ctx, c, api.RouteLearningList, req)
}

// DirectiveList sends the directive_list call.
func (c *Client) DirectiveList(ctx context.Context, req core.DirectiveListRequest) (*core.DirectiveList, error) {
	return post[core.DirectiveList](ctx, c, api.RouteDirectiveList, req)
}

// Recall sends the recall call.
func (c *Client) Recall(ctx context.Context, req core.RecallRequest) (*core.RecallResult, error) {
	return post[core.RecallResult](ctx, c, api.RouteRecall, req)
}

// Search sends the search call.
func (c *Client) Search(ctx context.Context, req core.SearchRequest) (*core.SearchResult, error) {
	return post[core.SearchResult](ctx, c, api.RouteSearch, req)
}

// LearningDecide sends the learning_decide operator call.
func (c *Client) LearningDecide(ctx context.Context, req core.LearningDecideRequest) (*core.Learning, error) {
	return post[core.Learning](ctx, c, api.RouteLearningDecide, req)
}

// DirectiveRatify sends the directive_ratify operator call.
func (c *Client) DirectiveRatify(ctx context.Context, req core.DirectiveRatifyRequest) (*core.Directive, error) {
	return post[core.Directive](ctx, c, api.RouteDirectiveRatify, req)
}

// DirectiveRetire sends the directive_retire operator call.
func (c *Client) DirectiveRetire(ctx context.Context, req core.DirectiveRetireRequest) (*core.Directive, error) {
	return post[core.Directive](ctx, c, api.RouteDirectiveRetire, req)
}

// ProjectSettingsSet sends the project_settings_set operator call.
func (c *Client) ProjectSettingsSet(ctx context.Context, req core.ProjectSettingsRequest) (*core.Project, error) {
	return post[core.Project](ctx, c, api.RouteProjectSettingsSet, req)
}

// post sends one call: req as the JSON body of route, with the caller that ctx
// carries in the Cairn headers. It returns the result the server answers with,
// or the error the call failed with.
func post[Res any](ctx context.Context, c *Client, route string, req any) (*Res, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("client: encode %s: %w", route, err)
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+api.Prefix+route, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("client: %s: %w", route, err)
	}
	hreq.Header.Set("Content-Type", "application/json")
	setCaller(ctx, hreq.Header)
	resp, err := c.hc.Do(hreq)
	if err != nil {
		return nil, fmt.Errorf("client: %s: %w", route, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, failure(route, resp)
	}
	var out Res
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("client: %s: the answer is not the call's result: %w", route, err)
	}
	return &out, nil
}

// setCaller writes the caller that ctx carries into h, as the Cairn headers. A
// field the caller leaves empty is not sent, so the server sees it as absent. A
// context with no caller sends no headers, and the server then finds no project.
func setCaller(ctx context.Context, h http.Header) {
	caller, ok := core.CallerFrom(ctx)
	if !ok {
		return
	}
	set := func(name, value string) {
		if value != "" {
			h.Set(name, value)
		}
	}
	set(api.HeaderProject, caller.Project)
	set(api.HeaderAgent, caller.Agent)
	set(api.HeaderSession, caller.Session)
	set(api.HeaderClient, caller.Client)
	set(api.HeaderRole, string(caller.Role))
}

// failure reads the error of an answer that is not a success. When the answer
// carries a core error kind, the error is the core.Error it names. Anything else,
// such as an internal failure or an answer that is not the wire form, is a
// plain error that names the route and the status, and is not a core.Error.
func failure(route string, resp *http.Response) error {
	var body api.ErrorBody
	if err := json.NewDecoder(io.LimitReader(resp.Body, api.MaxBodyBytes)).Decode(&body); err == nil && body.Error != nil && isCoreKind(body.Error.Kind) {
		return body.Error
	}
	return fmt.Errorf("client: %s: the server answered HTTP %d", route, resp.StatusCode)
}

// isCoreKind reports whether kind is one that core returns for a failure the
// caller can act on. The wire-only kind internal is not.
func isCoreKind(kind core.Kind) bool {
	switch kind {
	case core.KindInvalid, core.KindNotFound, core.KindRefused, core.KindOperatorOnly:
		return true
	}
	return false
}
