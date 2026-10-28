// Package core holds Cairn's contract and, from phase 2, its rules.
//
// Service is the contract every backend satisfies: the local SQLite
// implementation, the HTTP client, and any later one. Its Agent half is the
// MCP tool surface (docs/DESIGN.md §4); its Operator half is the decisions
// only an operator may make, reached from the web UI and the CLI, never from
// an MCP tool. docs/CONTRACT.md describes each call, and the suite in
// internal/conformance checks the behaviour.
//
// Every rule (status transitions, the close policy, step positions,
// operator-only changes) is enforced behind this interface and nowhere else.
package core
