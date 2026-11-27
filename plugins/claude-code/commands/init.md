---
description: Set up this project for Cairnbook. Runs cairnbook init with the arguments given.
argument-hint: --project <name> --server <url>
allowed-tools: Bash(cairnbook-launch init) Bash(cairnbook-launch init *)
---

Run this one command with the Bash tool, passing the arguments exactly as given, and show its output:

cairnbook-launch init $ARGUMENTS

`cairnbook-launch` runs the cairnbook binary and writes `.cairnbook.toml` in the project. Do nothing else, and do not change any other file.
