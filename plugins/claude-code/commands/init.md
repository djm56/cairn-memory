---
description: Set up this project for Cairn. Runs cairn init with the arguments given.
argument-hint: --project <name> --server <url>
allowed-tools: Bash(cairn-launch init) Bash(cairn-launch init *)
---

Run this one command with the Bash tool, passing the arguments exactly as given, and show its output:

cairn-launch init $ARGUMENTS

`cairn-launch` runs the cairn binary and writes `.cairn.toml` in the project. Do nothing else, and do not change any other file.
