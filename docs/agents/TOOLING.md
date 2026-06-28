# Tooling

## Skills

- Repo-local canonical skills live under `.agents/skills/*/SKILL.md`
- For feature-scoped work, start with the current feature's canonical front matter `skills`, falling back to the legacy `SPEC.md` `## SKILLS` table only when front matter is absent
- Keep the selected skill set minimal and actionable

## Command Capability Discovery

- Use `kit capabilities` when choosing among Kit commands and the mutation, network, write, or git behavior is not already obvious.
- Use `kit capabilities <command> --json` for one command path, including nested paths such as `rules add` or `skill mine`.
- Use `kit capabilities --search <term> --json` for compact filtered discovery, and `kit capabilities --full --json` only when hidden or deprecated compatibility commands matter.
- Treat `kit capabilities` itself as read-only: it does not require a Kit project root and does not load project config, write files, call the network, run subprocesses, or mutate git.
- In downstream Kit-managed projects, load `docs/references/rules/kit-capabilities-usage.md` when command discovery affects the task.
- Downstream projects should use `kit capabilities` for command discovery; do not maintain Kit's internal command catalog from a downstream project.

## Dispatch

- Use `kit dispatch` when broad work must be turned into safe multi-lane execution
- Use subagents when the work cleanly separates into low-overlap lanes after discovery
- Keep broad or noisy discovery in RLM first; use dispatch or direct subagent execution only after the relevant workstreams are narrow enough to predict overlap
- Predict overlap conservatively before parallelizing
- Keep the main agent responsible for synthesis, integration, validation, and communication

## Review Loop

- Use `kit pr fix` as the default PR review repair entrypoint when current PR review feedback should be fixed locally.
- With no `--pr`, `kit pr fix` lists open pull requests in the current repository and asks which one to repair.
- Use `kit pr fix --pr <target>` when the PR is known; accepted targets match dispatch PR intake: URL, Markdown link, `owner/repo#number`, or current-repo number.
- `kit pr fix` wraps the `kit loop review --pr` repair path, keeps staging/commit/push outside the repair loop, and asks the delegated agent to resolve verified fixed/no-op review threads.
- Use `kit loop review` when changed code should be locally reviewed and repaired by the configured loop agent until the final response reports at least 95% correctness and ends with `done`.
- Without `--pr`, `kit loop review` reviews current-branch changes relative to `origin/main`, falling back to local `main`, plus staged and unstaged changes.
- Use `kit loop review --pr <target>` when current unresolved CodeRabbit PR feedback should be opportunistically folded into the repair loop while local review starts immediately.
- Use `kit loop review --pr <target> --watch` or `--wait-for-coderabbit` only when finalization should block for CodeRabbit completion.
- Use `kit dispatch --loop --pr <target>` when current unresolved CodeRabbit PR review feedback should become a human-reviewed dispatch prompt instead of an agent repair loop.
- Use `kit dispatch --pr <target> --coderabbit` only when you need raw unresolved CodeRabbit review-thread intake without review-loop watch, classification, or summary behavior.
- Treat `kit loop review` as local repair only: it may edit files through the configured agent and write `.kit/loops` evidence, but it must not stage, commit, push, post PR comments, or resolve review threads.
- In `kit pr fix`, after fixes or no-op decisions are complete and validation has run, ask the delegated agent to resolve all matching current unresolved review threads on the PR, including human reviewer and CodeRabbit feedback, with `kit dispatch --pr <target> --resolve --yes`.
- Resolve only feedback verified as fixed or intentionally no-op; do not resolve unfixed, uncertain, stale, or unrelated feedback.
- `kit dispatch --pr <target> --resolve --yes` is an explicit GitHub mutation and must not be run speculatively.

## Project Directory

- Work in the existing project directory by default
- Do not create or use git worktrees for agent work
- If the current branch or dirty state is unsuitable, stop and ask the user how to proceed instead of creating an alternate checkout

## Secondary Global Inputs

- `~/.claude/CLAUDE.md`
- `${CODEX_HOME}/AGENTS.md`
- `${CODEX_HOME}/instructions.md`
- `${CODEX_HOME}/skills/*/SKILL.md`

- Treat these as secondary context after repo-local docs
- Do not use `.claude/skills` as canonical discovery input
