package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/djm56/cairn-memory/internal/core"
	"github.com/djm56/cairn-memory/internal/identity"
)

// maxInputBytes caps what a text flag or --json-input reads from standard input. It is the
// server's cap on a request body (api.MaxBodyBytes), so a larger input could not be sent.
const maxInputBytes = 1 << 20

// agentHelp is the help of the --agent flag of every verb that takes one. It names the
// variable, so the fallback is written where a person reads the flags. The variable is
// identity.EnvAgent: a write needs an agent, so a write with neither the flag nor the variable
// is a usage error. A read may run with none, and the reads that take an agent use it as the
// caller's role (CONTRACT §1.1).
const agentHelp = "the calling agent's name, such as hicks; left out, the " + identity.EnvAgent + " variable is used"

// command is the entry point of a sub-command: its opener, its arguments, the standard input a
// write may read, and the two output streams. Task, Step and Event have this shape.
type command func(open Opener, args []string, stdin io.Reader, stdout, stderr io.Writer) int

// agentName returns the calling agent: the --agent flag when it was given, else EnvAgent. A
// name of only spaces is no name, as CONTRACT §1.1 says. A write (required) with no name is a
// usage error that names both sources, and ok is false. A name the wire form cannot carry, one
// with a control character or a space at either end, is a usage error too, and the message names
// the source it came from, so the fix is at the place it was set. A read with no name is not
// refused; it runs with no agent.
func agentName(fl *flag.FlagSet, flagValue string, required bool, verb string, stderr io.Writer) (name string, ok bool) {
	name = flagValue
	source := "--agent"
	if !given(fl, "agent") {
		name = os.Getenv(identity.EnvAgent)
		source = identity.EnvAgent
	}
	if strings.TrimSpace(name) == "" {
		if required {
			fmt.Fprintf(stderr, "%s: no agent name: give --agent <name>, or set %s to one; a name of only spaces is no name\n", verb, identity.EnvAgent)
			return "", false
		}
		return "", true
	}
	if err := core.CheckCaller(core.Caller{Agent: name}); err != nil {
		fmt.Fprintf(stderr, "%s: the name from %s is refused: %s\n", verb, source, Reason(err))
		return "", false
	}
	return name, true
}

// withAgent returns ctx with the caller's agent set to name. The rest of the caller is kept, so
// the project and the client are still those the open gave.
func withAgent(ctx context.Context, name string) context.Context {
	c, _ := core.CallerFrom(ctx)
	c.Agent = name
	return core.WithCaller(ctx, c)
}

// usageErr prints a usage error of a command to stderr and returns the usage exit code.
func usageErr(stderr io.Writer, verb string, err error) int {
	fmt.Fprintf(stderr, "%s: %v\n", verb, err)
	return 2
}

// inputs hands out standard input to the flags that read it. Standard input is one stream, so
// a second flag that asks for it is a usage error, and the message names the first.
type inputs struct {
	stdin io.Reader
	taken string
}

// take records that the flag name reads standard input. It fails when another flag has read it.
func (in *inputs) take(name string) error {
	if in.taken != "" {
		return fmt.Errorf("--%s and --%s both read standard input: give one of them a value", in.taken, name)
	}
	in.taken = name
	return nil
}

// text returns the value of a free-text flag: the value itself, or the whole of standard input
// when the value is "-". One line break at the end of the input is dropped, so a piped line is
// not kept with its newline.
func (in *inputs) text(name, value string) (string, error) {
	if value != "-" {
		return value, nil
	}
	if err := in.take(name); err != nil {
		return "", err
	}
	raw, err := io.ReadAll(io.LimitReader(in.stdin, maxInputBytes+1))
	if err != nil {
		return "", fmt.Errorf("--%s: read standard input: %w", name, err)
	}
	if len(raw) > maxInputBytes {
		return "", fmt.Errorf("--%s: standard input is larger than %d bytes", name, maxInputBytes)
	}
	return strings.TrimSuffix(strings.TrimSuffix(string(raw), "\n"), "\r"), nil
}

// optText is the text of a flag that changes its field only when the flag was given: nil when
// it was left out, so the field is not sent, and a pointer to the text when it was.
func (in *inputs) optText(fl *flag.FlagSet, name, value string) (*string, error) {
	if !given(fl, name) {
		return nil, nil
	}
	v, err := in.text(name, value)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// decode reads standard input as one JSON object into v, for --json-input. A key that v does
// not name is refused, so a misspelt field is not dropped without a word, and a second value
// after the object is refused too.
func (in *inputs) decode(name string, v any) error {
	if err := in.take(name); err != nil {
		return err
	}
	raw, err := io.ReadAll(io.LimitReader(in.stdin, maxInputBytes+1))
	if err != nil {
		return fmt.Errorf("--%s: read standard input: %w", name, err)
	}
	if len(raw) > maxInputBytes {
		return fmt.Errorf("--%s: standard input is larger than %d bytes", name, maxInputBytes)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("--%s: standard input is not the JSON object this call takes: %w", name, err)
	}
	if err := dec.Decode(new(json.RawMessage)); err != io.EOF {
		return fmt.Errorf("--%s: standard input holds more than one JSON value", name)
	}
	return nil
}
