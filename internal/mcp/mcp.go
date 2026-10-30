// Package mcp is the MCP tool surface of Cairn (DESIGN.md §4): 13 tools plus the
// read tools recall and search, each one call of a core.Service. The package
// holds no rules. It maps each tool's arguments to one request type, and each
// result or error back, as CONTRACT §1 asks of the adapter. The Service it is
// given is, in production, an internal/client that reaches a server over HTTP,
// so this package never touches the store (DESIGN.md §5.1).
//
// The tools speak the MCP protocol through github.com/modelcontextprotocol/go-sdk.
// A call's arguments are decoded here, not by the SDK, so that a repeated key in
// event data reaches core and is refused there (CONTRACT §4). The SDK's typed
// handlers would rebuild the arguments from a map and keep only the last of two
// equal keys.
//
// Identity (CONTRACT §1.1). Every call runs in one project, the Project given to
// New. Each write call takes an agent argument, which is the caller's agent, and
// every call sends the client name the MCP client gave at initialisation. The
// step tools name the step's assignee "assignee", because "agent" is the caller
// there; see the tool descriptions and DESIGN.md §4.
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"runtime/debug"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/djm56/cairn-memory/internal/core"
)

// Name is the name the server gives itself to MCP clients.
const Name = "cairn"

// diagnostics receives what the adapter tells the operator and not the model: the
// stack of a panic in a tool (step 10 review S5). It is stderr, since stdout carries
// the protocol. A test replaces it.
var diagnostics io.Writer = os.Stderr

// Project is the project every call of the server runs in. A failure to resolve
// it is kept, not raised: each call is then refused with that error, as DESIGN.md
// §2.1 asks. There is no default project.
type Project struct {
	// Name is the project, already checked by core.ValidProjectName.
	Name string
	// Err, when set, refuses every call with its message.
	Err error
}

// Server is an MCP server with the Cairn tools registered. Build one with New,
// then run it on a transport.
type Server struct {
	srv     *mcp.Server
	project Project
}

// New returns the Cairn MCP server over svc, in project. version is the version
// the server reports to clients. Every tool is registered before New returns.
func New(svc core.Service, project Project, version string) *Server {
	s := &Server{
		srv:     mcp.NewServer(&mcp.Implementation{Name: Name, Version: version}, nil),
		project: project,
	}
	register(s, svc)
	return s
}

// Run serves the tools on t until the client disconnects or ctx is done. It
// returns the error that ended the session, or nil when the client went away
// cleanly.
func (s *Server) Run(ctx context.Context, t mcp.Transport) error {
	return s.srv.Run(ctx, t)
}

// noInput is the input of a tool that takes no arguments.
type noInput struct{}

// addTool registers one tool named name. Its input schema is inferred from In.
// Its handler decodes the arguments into In, refusing a field In does not name,
// and calls fn with a context that carries the caller. For a write, the caller's
// agent is the "agent" argument; a read carries no agent. A failure of any kind
// becomes a tool error result, never a protocol error, so the model reads it.
func addTool[In any](s *Server, name, description string, write bool, fn func(ctx context.Context, in In) (any, error)) {
	schema, err := jsonschema.For[In](forOptions)
	if err != nil {
		// The input types are fixed in this package, so a schema that does not infer
		// is a programming error, found by the first test run.
		panic(fmt.Sprintf("mcp: the input schema of %s: %v", name, err))
	}
	tool := &mcp.Tool{Name: name, Description: description, InputSchema: schema}
	s.srv.AddTool(tool, func(ctx context.Context, req *mcp.CallToolRequest) (res *mcp.CallToolResult, err error) {
		// The SDK does not recover a panic in a handler (step 8 review S2; SDK
		// v1.8.0 has no recover in its non-test code, as far as its source shows).
		// A panic becomes an internal tool error, so one bad field does not end the
		// session. The cause is in the message, as for other internal errors.
		defer func() {
			if p := recover(); p != nil {
				// The model gets the cause. The stack goes to diagnostics, for the
				// operator (step 10 review S5), since the model cannot act on it.
				fmt.Fprintf(diagnostics, "cairn mcp: the %s tool panicked: %v\n%s", name, p, debug.Stack())
				res, err = failure(fmt.Errorf("the tool failed: %v", p)), nil
			}
		}()
		if s.project.Err != nil {
			return failure(withRestart(s.project.Err)), nil
		}
		in, err := decodeArgs[In](req.Params.Arguments)
		if err != nil {
			return failure(err), nil
		}
		caller := core.Caller{Project: s.project.Name, Client: clientName(req), Role: core.RoleAgent}
		if write {
			caller.Agent = agentArg(req.Params.Arguments)
		}
		out, err := fn(core.WithCaller(ctx, caller), in)
		if err != nil {
			return failure(withAgentArgument(err, write, caller.Agent)), nil
		}
		return answer(out), nil
	})
}

// withRestart adds to the refusal for an unresolved project that the session must
// be restarted after the fix (step 8 review S9). The project is read once, when
// the session starts, so a fix made later is not seen until then. The kind and the
// missing list are kept; the error is copied, not changed, since core's ErrNoProject
// is shared.
func withRestart(err error) error {
	var ce *core.Error
	if !errors.As(err, &ce) {
		return err
	}
	return &core.Error{Kind: ce.Kind, Message: ce.Message + ", then restart this session: the project is read once, when it starts", Missing: ce.Missing}
}

// withAgentArgument names the agent argument in the refusal of a write with no
// agent (step 8 review S6). Core refuses the call, and its message does not say
// which argument to give; on this surface the argument is agent. Only the text
// changes: the refusal and its kind are core's. Core checks the caller before any
// other field, so a blank agent on a write is always the first fault, and the
// suffix is added to the refusal the call gets, of kind invalid.
func withAgentArgument(err error, write bool, agent string) error {
	var ce *core.Error
	if !write || strings.TrimSpace(agent) != "" || !errors.As(err, &ce) || ce.Kind != core.KindInvalid {
		return err
	}
	return &core.Error{Kind: ce.Kind, Message: ce.Message + ": pass your name as the agent argument", Missing: ce.Missing}
}

// forOptions overrides the schema of the one type the inference cannot describe
// well. event data is a JSON object, which encoding/json would describe as a byte
// array.
var forOptions = &jsonschema.ForOptions{
	TypeSchemas: map[reflect.Type]*jsonschema.Schema{
		reflect.TypeFor[json.RawMessage](): {Type: "object", Description: "a JSON object"},
	},
}

// decodeArgs reads the arguments of a call into In. It refuses a field that In
// does not name, and a second JSON value after the object, as the HTTP form does
// (DESIGN.md §5.4). Absent or empty arguments are the zero input, so a tool with
// no arguments takes none. A failure is core.Invalid, and names what to fix.
func decodeArgs[In any](raw json.RawMessage) (In, error) {
	var in In
	if len(raw) == 0 {
		return in, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return in, core.Invalid("the arguments are not the tool's input: %v", err)
	}
	if err := dec.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		return in, core.Invalid("the arguments hold one JSON object: remove what follows it")
	}
	return in, nil
}

// agentArg returns the "agent" argument of a write call, or "" when there is none.
// The arguments were decoded already, so a malformed object cannot reach here; a
// missing or blank agent is core's to refuse (CONTRACT §1.1).
func agentArg(raw json.RawMessage) string {
	var who struct {
		Agent string `json:"agent"`
	}
	_ = json.Unmarshal(raw, &who)
	return who.Agent
}

// clientName returns the name the MCP client gave at initialisation, such as
// claude-code, or "" when it gave none.
func clientName(req *mcp.CallToolRequest) string {
	if info := req.ClientInfo(); info != nil {
		return info.Name
	}
	return ""
}
