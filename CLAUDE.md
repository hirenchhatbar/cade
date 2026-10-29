# CLAUDE.md

## Rules

@.claude/rules/code-quality.md
@.claude/rules/make-docker.md
@.claude/rules/docs.md
@.claude/rules/settings-dialog.md
@.claude/rules/cli.md

## Docs

@docs/DIRECTORY_STRUCTURE.md
@docs/TECH_STACK.md

## Keep file current

This file failure log, not wishlist. Every line below exist cause went
wrong once.

What mistake made, get corrected, or discover something about codebase not
written down:

- Add one line failure log below, imperative mood, describe correct
  behavior.
- Keep specific repo. General advice belong nowhere.
- Fix be workflow not rule? Put in .claude/skills/ and link from here.

Keep file under 500 lines. Loaded every session, long context make you less
reliable, not more. Section outgrow usefulness, move relevant directory
<DIRECTORY>/CLAUDE.md or skill.

## Failure log

Mistakes made before, must not repeat. Check before acting; append new entry
when caught doing one.

## graphify

This project has a knowledge graph at graphify-out/ with god nodes, community structure, and cross-file relationships.

Rules:
- When graphify-out/graph.json exists, run `graphify query "<question>"` before your first grep, search or source-file read for a codebase question, and put this rule in every subagent prompt that explores code. Read raw files only for lines the graph or an earlier read already located. Use `graphify path "<A>" "<B>"` for relationships and `graphify explain "<concept>"` for focused concepts. These return a scoped subgraph, usually much smaller than GRAPH_REPORT.md or raw grep output.
- If graphify-out/wiki/index.md exists, use it for broad navigation instead of raw source browsing.
- Read graphify-out/GRAPH_REPORT.md only for broad architecture review or when query/path/explain do not surface enough context.
- After modifying code, run `graphify update .` to keep the graph current (AST-only, no API cost).
- A committed graphify-out/graph.json is rewritten whole on every build, so a large diff to it in a pull request is expected. Do not flag it as churn or suggest skipping the graph rebuild or its hooks.
