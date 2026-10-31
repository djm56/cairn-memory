// Package api serves a core.Service over HTTP. It is the wire form of the
// service (DESIGN.md §5.4): each handler decodes a request, calls the service,
// and encodes the result or the error. A handler holds no rules; the rules live
// in core. M1 builds the handlers only. No binary mounts them on a listener
// (decision D19), so they are reached in tests through an httptest server.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"

	"github.com/djm56/cairn-memory/internal/core"
)

// Prefix is the path every call lives under. A breaking change to the wire
// form takes a new prefix (DESIGN.md §5.4).
const Prefix = "/v1"

// The routes, one per call, relative to Prefix. The agent calls are the MCP
// tool surface (DESIGN.md §4). The operator calls (CONTRACT §8) are never MCP
// tools, and sit under their own prefix so a server can gate them apart.
const (
	RouteTaskCurrent        = "/agent/task_current"
	RouteTaskStart          = "/agent/task_start"
	RouteTaskGet            = "/agent/task_get"
	RouteTaskList           = "/agent/task_list"
	RouteTaskUpdate         = "/agent/task_update"
	RouteTaskClose          = "/agent/task_close"
	RouteStepPlan           = "/agent/step_plan"
	RouteStepAdd            = "/agent/step_add"
	RouteStepUpdate         = "/agent/step_update"
	RouteEventLog           = "/agent/event_log"
	RouteLearningAdd        = "/agent/learning_add"
	RouteLearningList       = "/agent/learning_list"
	RouteDirectiveList      = "/agent/directive_list"
	RouteRecall             = "/agent/recall"
	RouteSearch             = "/agent/search"
	RouteLearningDecide     = "/operator/learning_decide"
	RouteDirectiveRatify    = "/operator/directive_ratify"
	RouteDirectiveRetire    = "/operator/directive_retire"
	RouteProjectSettingsSet = "/operator/project_settings_set"
)

// The headers that carry the caller (CONTRACT §1.1). None is required by the
// wire form; a call with no project is invalid, whatever the headers say.
const (
	HeaderProject = "X-Cairn-Project"
	HeaderAgent   = "X-Cairn-Agent"
	HeaderSession = "X-Cairn-Session"
	HeaderClient  = "X-Cairn-Client"
	// HeaderRole is "operator" for an operator call. Any other value, or none,
	// is an agent.
	HeaderRole = "X-Cairn-Role"
)

// MaxBodyBytes caps a request body. A larger body is refused with 413.
const MaxBodyBytes = 1 << 20

// KindInternal is the error kind of a failure that is not a core.Error. Its
// message is always internalMessage, so the caller never sees the cause. The
// cause goes to the FailureHook that Handler was given (DESIGN.md §5.4).
const KindInternal core.Kind = "internal"

// KindUnsupportedMediaType is the error kind of a body whose Content-Type is not
// application/json. It is answered 415, and is a wire kind like KindInternal: no
// core call returns it (CONTRACT §1.2).
const KindUnsupportedMediaType core.Kind = "unsupported_media_type"

// KindNoRoute is the error kind of a path that is no call. It is answered 404. A
// missing entity is not_found, and a method other than POST is 405 (DESIGN §5.4).
const KindNoRoute core.Kind = "no_route"

const internalMessage = "internal error"

// ErrorBody is the body of every error answer:
// {"error": {"kind": ..., "message": ..., "missing": [...]}}. The client reads
// it back into the core.Error it came from.
type ErrorBody struct {
	Error *core.Error `json:"error"`
}

// CallerFunc derives the caller of a request. The role must come from the
// server's own knowledge of the request, never from what the client sends. M2
// derives it from the API key's scope (CONTRACT §1.1).
type CallerFunc func(r *http.Request) core.Caller

// FailureHook receives the cause of every internal failure: an error that is
// not a core.Error, and a panic in a call, which arrives as an error naming the
// panic value. It is for the server's own record and is never sent to the
// caller. A nil hook discards the cause.
type FailureHook func(r *http.Request, err error)

// UnauthenticatedHeaderCaller returns the caller that the request's Cairn
// headers name, and trusts the role header as it is sent. No key checks it, so
// it is for in-process tests only. It must never be the caller of a listener:
// M2 replaces it with a resolver that takes the role from the API key's scope
// (CONTRACT §1.1 M1 exception, DESIGN.md §5.4). A missing or unknown role is an
// agent.
func UnauthenticatedHeaderCaller(r *http.Request) core.Caller {
	role := core.RoleAgent
	if r.Header.Get(HeaderRole) == string(core.RoleOperator) {
		role = core.RoleOperator
	}
	return core.Caller{
		Project: r.Header.Get(HeaderProject),
		Agent:   r.Header.Get(HeaderAgent),
		Session: r.Header.Get(HeaderSession),
		Client:  r.Header.Get(HeaderClient),
		Role:    role,
	}
}

// Handler returns the handler for every call of svc, under Prefix. caller
// derives the caller of each request. It must not be nil: there is no default
// caller, because a default identity is a silent one (CONTRACT §1.1). onFailure
// receives the cause of each internal failure and may be nil.
func Handler(svc core.Service, caller CallerFunc, onFailure FailureHook) http.Handler {
	if caller == nil {
		panic("api: Handler needs a CallerFunc, and there is no default")
	}
	mux := http.NewServeMux()
	handle := func(route string, h http.Handler) { mux.Handle(Prefix+route, h) }

	handle(RouteTaskCurrent, call(caller, onFailure, func(ctx context.Context, _ struct{}) (*core.TaskCurrent, error) {
		return svc.TaskCurrent(ctx)
	}))
	handle(RouteTaskStart, call(caller, onFailure, svc.TaskStart))
	handle(RouteTaskGet, call(caller, onFailure, svc.TaskGet))
	handle(RouteTaskList, call(caller, onFailure, svc.TaskList))
	handle(RouteTaskUpdate, call(caller, onFailure, svc.TaskUpdate))
	handle(RouteTaskClose, call(caller, onFailure, svc.TaskClose))
	handle(RouteStepPlan, call(caller, onFailure, svc.StepPlan))
	handle(RouteStepAdd, call(caller, onFailure, svc.StepAdd))
	handle(RouteStepUpdate, call(caller, onFailure, svc.StepUpdate))
	handle(RouteEventLog, call(caller, onFailure, svc.EventLog))
	handle(RouteLearningAdd, call(caller, onFailure, svc.LearningAdd))
	handle(RouteLearningList, call(caller, onFailure, svc.LearningList))
	handle(RouteDirectiveList, call(caller, onFailure, svc.DirectiveList))
	handle(RouteRecall, call(caller, onFailure, svc.Recall))
	handle(RouteSearch, call(caller, onFailure, svc.Search))

	handle(RouteLearningDecide, call(caller, onFailure, svc.LearningDecide))
	handle(RouteDirectiveRatify, call(caller, onFailure, svc.DirectiveRatify))
	handle(RouteDirectiveRetire, call(caller, onFailure, svc.DirectiveRetire))
	handle(RouteProjectSettingsSet, call(caller, onFailure, svc.ProjectSettingsSet))

	mux.Handle("/", http.HandlerFunc(notFound))
	return mux
}

// call returns the handler of one call. It decodes the request body into Req,
// calls fn with the caller in the context, and writes the result or the error.
// Only POST is a call; any other method is refused. A panic in fn is recovered
// and answered as an internal failure.
func call[Req any, Res any](caller CallerFunc, onFailure FailureHook, fn func(context.Context, Req) (*Res, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer recoverInternal(w, r, onFailure)
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeError(w, http.StatusMethodNotAllowed, &core.Error{Kind: core.KindInvalid, Message: "a call is a POST with a JSON body"})
			return
		}
		var req Req
		if status, bad := decode(w, r, &req); bad != nil {
			writeError(w, status, bad)
			return
		}
		res, err := fn(core.WithCaller(r.Context(), caller(r)), req)
		if err != nil {
			writeFailure(w, r, onFailure, err)
			return
		}
		writeJSON(w, http.StatusOK, res)
	})
}

// recoverInternal is deferred by call. A panic becomes an internal failure: the
// panic value goes to onFailure, and the caller gets the same answer as any other
// internal failure. http.ErrAbortHandler is passed on, since net/http uses it to
// abort a response on purpose.
func recoverInternal(w http.ResponseWriter, r *http.Request, onFailure FailureHook) {
	p := recover()
	if p == nil {
		return
	}
	if p == http.ErrAbortHandler {
		panic(p)
	}
	report(onFailure, r, fmt.Errorf("panic: %v", p))
	writeError(w, http.StatusInternalServerError, &core.Error{Kind: KindInternal, Message: internalMessage})
}

// decode reads the one JSON object of a request body into v. An empty body is
// the zero request, and needs no Content-Type. A body with content must be
// application/json (checkMediaType). A key written in the wrong case or given
// twice is refused (checkKeys). An unknown field, a value of the wrong type, a
// second value and a body over MaxBodyBytes are refused too, each with the status
// it earns. The status is returned with the error, and is zero when the body is
// read.
func decode(w http.ResponseWriter, r *http.Request, v any) (int, *core.Error) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		return http.StatusRequestEntityTooLarge, &core.Error{Kind: core.KindInvalid, Message: fmt.Sprintf("the body is larger than %d bytes", MaxBodyBytes)}
	case err != nil:
		return http.StatusBadRequest, &core.Error{Kind: core.KindInvalid, Message: "the body could not be read: " + err.Error()}
	}
	// Only the JSON white space counts as no body.
	if len(bytes.Trim(body, " \t\r\n")) == 0 {
		return 0, nil
	}
	if bad := checkMediaType(r); bad != nil {
		return http.StatusUnsupportedMediaType, bad
	}
	if bad := checkKeys(body, v); bad != nil {
		return http.StatusBadRequest, bad
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return http.StatusBadRequest, &core.Error{Kind: core.KindInvalid, Message: "the body is not the call's request: " + err.Error()}
	}
	if err := dec.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		return http.StatusBadRequest, &core.Error{Kind: core.KindInvalid, Message: "the body holds one JSON object: remove what follows it"}
	}
	return 0, nil
}

// checkMediaType refuses a request whose Content-Type is not application/json, or
// that has none. Parameters such as charset are accepted. Refusing a missing type
// as well keeps a form post sent as text/plain out of the decoder (issue #109).
func checkMediaType(r *http.Request) *core.Error {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err == nil && media == "application/json" {
		return nil
	}
	return &core.Error{Kind: KindUnsupportedMediaType, Message: "the body must be JSON: send the header Content-Type: application/json"}
}

// writeFailure writes err as the answer to a call. A core.Error keeps its kind
// and message. Any other error is an internal failure: its cause goes to
// onFailure, and the caller gets the fixed internal answer.
func writeFailure(w http.ResponseWriter, r *http.Request, onFailure FailureHook, err error) {
	var ce *core.Error
	if errors.As(err, &ce) {
		writeError(w, statusOf(ce.Kind), ce)
		return
	}
	report(onFailure, r, err)
	writeError(w, http.StatusInternalServerError, &core.Error{Kind: KindInternal, Message: internalMessage})
}

// report hands the cause of an internal failure to onFailure, when there is one.
func report(onFailure FailureHook, r *http.Request, err error) {
	if onFailure != nil {
		onFailure(r, err)
	}
}

// statusOf returns the HTTP status for an error kind (DESIGN.md §5.4). A kind
// the wire form does not know is a server failure.
func statusOf(kind core.Kind) int {
	switch kind {
	case core.KindInvalid:
		return http.StatusBadRequest
	case core.KindNotFound:
		return http.StatusNotFound
	case core.KindRefused:
		return http.StatusConflict
	case core.KindOperatorOnly:
		return http.StatusForbidden
	}
	return http.StatusInternalServerError
}

// notFound answers a path that is no call. A call of the wrong method is not a
// missing call, and is answered by call. The message points at the base URL,
// since a mis-set base is the usual reason for a path to miss.
func notFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, &core.Error{
		Kind: KindNoRoute,
		Message: "no call at " + r.URL.Path + ": check the server base URL. Calls are POST to " +
			Prefix + "/agent/<call> and " + Prefix + "/operator/<call>",
	})
}

// writeError writes e as the error body of an answer with status.
func writeError(w http.ResponseWriter, status int, e *core.Error) {
	writeJSON(w, status, ErrorBody{Error: e})
}

// writeJSON writes v as the JSON body of an answer with status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// The status is already sent. A failed write has no one left to tell.
	_ = json.NewEncoder(w).Encode(v)
}
