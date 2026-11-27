package main

import "github.com/djm56/cairn-memory/internal/cli"

// The text rules live in internal/cli, where the CLI sub-commands use them too. These
// names keep the hook, status and export code as it was: each is a call of the shared rule.

// maxLineRunes is cli.MaxLineRunes: the cap on a server-derived line.
const maxLineRunes = cli.MaxLineRunes

// plain returns a server-derived text as one printable line, capped at maxLineRunes (cli.Plain).
func plain(s string) string { return cli.Plain(s) }

// foldLine returns s as one line of printable text, with no cap (cli.FoldLine).
func foldLine(s string) string { return cli.FoldLine(s) }

// blockText returns a text that keeps its line breaks, for a section body (cli.BlockText).
func blockText(s string) string { return cli.BlockText(s) }
