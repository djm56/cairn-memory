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

// KindUnauthenticated is the error kind of a request whose API key is missing,
// malformed, unknown or revoked. It is answered 401, and is a wire kind: no core
// call returns it (CONTRACT §1.2).
const KindUnauthenticated core.Kind = "unauthenticated"

// KindReadOnly is the error kind of a write made with a viewer key. It is answered
// 403, and is a wire kind: core never sees the refused write. Core's operator_only
// is the answer for an operator call without the operator role (CONTRACT §1.2).
const KindReadOnly core.Kind = "read_only"

const internalMessage = "internal error"

// unauthenticatedMessage is the message of every 401. It says what to send and
// never echoes the header, so a hostile value does not come back in the answer.
const unauthenticatedMessage = "the request needs a valid API key: send the header Authorization: Bearer <key>. A key that is missing, malformed, unknown or revoked is refused the same way"

// readOnlyMessage is the message of a viewer's refused write.
const readOnlyMessage = "this key is read-only: a viewer key may read, not write"

// ErrorBody is the body of every error answer:
// {"error": {"kind": ..., "message": ..., "missing": [...]}}. The client reads
// it back into the core.Error it came from.
type ErrorBody struct {
	Error *core.Error `json:"error"`
}

// access says whether a route reads or writes (DESIGN.md §5.4). A viewer key is
// refused on every write route, and passes every read route.
type access int

const (
	// accessUnmarked is the zero value. A route with it is not mounted: see accessOf.
	accessUnmarked access = iota
	accessRead
	accessWrite
)

// routeAccess is the route table's marks: one entry for each route constant above.
// A route with no entry, or with the zero value, panics at Handler, and the test
// TestEveryRouteConstHasAMark fails on a constant that has no entry, so a route
// added without a mark is caught before it is mounted.
var routeAccess = map[string]access{
	RouteTaskCurrent:        accessRead,
	RouteTaskStart:          accessWrite,
	RouteTaskGet:            accessRead,
	RouteTaskList:           accessRead,
	RouteTaskUpdate:         accessWrite,
	RouteTaskClose:          accessWrite,
	RouteStepPlan:           accessWrite,
	RouteStepAdd:            accessWrite,
	RouteStepUpdate:         accessWrite,
	RouteEventLog:           accessWrite,
	RouteLearningAdd:        accessWrite,
	RouteLearningList:       accessRead,
	RouteDirectiveList:      accessRead,
	RouteRecall:             accessRead,
	RouteSearch:             accessRead,
	RouteLearningDecide:     accessWrite,
	RouteDirectiveRatify:    accessWrite,
	RouteDirectiveRetire:    accessWrite,
	RouteProjectSettingsSet: accessWrite,
}

// accessOf returns the mark of route. A route with no mark is a fault in the
// route table, and Handler panics at mount rather than serve it unguarded.
func accessOf(route string) access {
	a, ok := routeAccess[route]
	if !ok || a == accessUnmarked {
		panic("api: route " + route + " has no read or write mark: add it to routeAccess")
	}
	return a
}

// operatorRoutes are the calls that only an operator key may make (CONTRACT §8).
// Each is also a write route in routeAccess. A wire call refuses any other role
// before it reads the body, so a request of the wrong shape from an agent key is
// still operator_only (CONTRACT §1.2). Core checks the role again on its side.
var operatorRoutes = map[string]bool{
	RouteLearningDecide:     true,
	RouteDirectiveRatify:    true,
	RouteDirectiveRetire:    true,
	RouteProjectSettingsSet: true,
}

// routeSpec is what call needs to know about a route before it reads a request.
type routeSpec struct {
	access   access
	operator bool
}

// specOf returns the spec of route. It panics on a route with no read or write
// mark, as accessOf does, so a route is never mounted without its gates.
func specOf(route string) routeSpec {
	return routeSpec{access: accessOf(route), operator: operatorRoutes[route]}
}

// operatorOnlyMessage is the message of an agent key's refused operator call. It is
// the message core gives for the same refusal, so the two answers read the same.
const operatorOnlyMessage = "this call is an operator decision, and only an operator may make it"

// CallerFunc derives the caller of a request. The role must come from the
// server's own knowledge of the request, never from what the client sends: the
// key resolver (KeyCaller) takes it from the API key's scope (CONTRACT §1.1). A
// CallerFunc that fails returns the error, and the call answers it: a core.Error
// with its kind (KindUnauthenticated gives 401), and any other error as internal.
type CallerFunc func(r *http.Request) (core.Caller, error)

// FailureHook receives the cause of every internal failure: an error that is
// not a core.Error, and a panic in a call, which arrives as an error naming the
// panic value. It is for the server's own record and is never sent to the
// caller. A nil hook discards the cause.
type FailureHook func(r *http.Request, err error)

// UnauthenticatedHeaderCaller returns the caller that the request's Cairn
// headers name, and trusts the role header as it is sent. No key checks it, so
// it is for in-process tests only. Non-test code may not name it: the reach
// guard TestNoNonTestUseOfUnauthenticatedHeaderCaller fails on any use outside
// a test (CONTRACT §1.1 M1 exception, DESIGN.md §5.4). A missing or unknown role
// is an agent. It never returns an error.
func UnauthenticatedHeaderCaller(r *http.Request) (core.Caller, error) {
	role := core.RoleAgent
	switch r.Header.Get(HeaderRole) {
	case string(core.RoleOperator):
		role = core.RoleOperator
	case string(core.RoleViewer):
		// A viewer role is honoured so the conformance suite can express a viewer over
		// the client (CONTRACT §9 item 44). The key resolver takes the role from the key.
		role = core.RoleViewer
	}
	return core.Caller{
		Project: r.Header.Get(HeaderProject),
		Agent:   r.Header.Get(HeaderAgent),
		Session: r.Header.Get(HeaderSession),
		Client:  r.Header.Get(HeaderClient),
		Role:    role,
	}, nil
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

	handle(RouteTaskCurrent, call(caller, onFailure, specOf(RouteTaskCurrent), func(ctx context.Context, _ struct{}) (*core.TaskCurrent, error) {
		return svc.TaskCurrent(ctx)
	}))
	handle(RouteTaskStart, call(caller, onFailure, specOf(RouteTaskStart), svc.TaskStart))
	handle(RouteTaskGet, call(caller, onFailure, specOf(RouteTaskGet), svc.TaskGet))
	handle(RouteTaskList, call(caller, onFailure, specOf(RouteTaskList), svc.TaskList))
	handle(RouteTaskUpdate, call(caller, onFailure, specOf(RouteTaskUpdate), svc.TaskUpdate))
	handle(RouteTaskClose, call(caller, onFailure, specOf(RouteTaskClose), svc.TaskClose))
	handle(RouteStepPlan, call(caller, onFailure, specOf(RouteStepPlan), svc.StepPlan))
	handle(RouteStepAdd, call(caller, onFailure, specOf(RouteStepAdd), svc.StepAdd))
	handle(RouteStepUpdate, call(caller, onFailure, specOf(RouteStepUpdate), svc.StepUpdate))
	handle(RouteEventLog, call(caller, onFailure, specOf(RouteEventLog), svc.EventLog))
	handle(RouteLearningAdd, call(caller, onFailure, specOf(RouteLearningAdd), svc.LearningAdd))
	handle(RouteLearningList, call(caller, onFailure, specOf(RouteLearningList), svc.LearningList))
	handle(RouteDirectiveList, call(caller, onFailure, specOf(RouteDirectiveList), svc.DirectiveList))
	handle(RouteRecall, call(caller, onFailure, specOf(RouteRecall), svc.Recall))
	handle(RouteSearch, call(caller, onFailure, specOf(RouteSearch), svc.Search))

	handle(RouteLearningDecide, call(caller, onFailure, specOf(RouteLearningDecide), svc.LearningDecide))
	handle(RouteDirectiveRatify, call(caller, onFailure, specOf(RouteDirectiveRatify), svc.DirectiveRatify))
	handle(RouteDirectiveRetire, call(caller, onFailure, specOf(RouteDirectiveRetire), svc.DirectiveRetire))
	handle(RouteProjectSettingsSet, call(caller, onFailure, specOf(RouteProjectSettingsSet), svc.ProjectSettingsSet))

	mux.Handle("/", http.HandlerFunc(notFound))
	return mux
}

// call returns the handler of one call. It decodes the request body into Req,
// calls fn with the caller in the context, and writes the result or the error.
// Only POST is a call; any other method is refused. A panic in fn is recovered
// and answered as an internal failure.
//
// The order is the one CONTRACT §1.2 gives: the method, then the caller, then the
// role gates, then the request shape. So a request with no valid key is refused
// before its body is read. A viewer's write is refused after the caller, with
// KindReadOnly. On an operator route, a caller that is not an operator is refused
// with KindOperatorOnly. Both are answered before the body is read, so a request of
// the wrong shape from a lower role gets the role's refusal, not a 400 or 415.
func call[Req any, Res any](caller CallerFunc, onFailure FailureHook, spec routeSpec, fn func(context.Context, Req) (*Res, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer recoverInternal(w, r, onFailure)
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeError(w, http.StatusMethodNotAllowed, &core.Error{Kind: core.KindInvalid, Message: "a call is a POST with a JSON body"})
			return
		}
		c, err := caller(r)
		if err != nil {
			writeFailure(w, r, onFailure, err)
			return
		}
		if spec.access == accessWrite && c.Role == core.RoleViewer {
			writeError(w, http.StatusForbidden, &core.Error{Kind: KindReadOnly, Message: readOnlyMessage})
			return
		}
		if spec.operator && c.Role != core.RoleOperator {
			writeError(w, http.StatusForbidden, &core.Error{Kind: core.KindOperatorOnly, Message: operatorOnlyMessage})
			return
		}
		var req Req
		if status, bad := decode(w, r, &req); bad != nil {
			writeError(w, status, bad)
			return
		}
		res, err := fn(core.WithCaller(r.Context(), c), req)
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
	case core.KindOperatorOnly, KindReadOnly:
		return http.StatusForbidden
	case KindUnauthenticated:
		return http.StatusUnauthorized
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

// writeError writes e as the error body of an answer with status. A 401 names the
// scheme the server takes, as RFC 7235 §3.1 requires, so a client knows to send a
// Bearer key.
func writeError(w http.ResponseWriter, status int, e *core.Error) {
	if status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", "Bearer")
	}
	writeJSON(w, status, ErrorBody{Error: e})
}

// writeJSON writes v as the JSON body of an answer with status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// The status is already sent. A failed write has no one left to tell.
	_ = json.NewEncoder(w).Encode(v)
}
