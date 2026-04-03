## Summary

<!-- One sentence describing what this PR does -->

## Motivation

<!-- Why is this change needed? Link issues with "Closes #123" or "Fixes #123" -->

## Changes

<!-- Bullet list of what changed and why -->

## Testing

<!-- How did you test this? Which test cases were added or updated? -->
- [ ] `make test` passes
- [ ] `make lint` passes
- [ ] Manual testing steps described below (if applicable)

## Checklist

- [ ] Code follows the [architecture rules](../docs/STRUCTURE.md) (no cross-layer violations)
- [ ] No status string literals outside `internal/domain/enums.go`
- [ ] No state transitions outside `internal/queue/state_machine.go`
- [ ] No raw SQL outside `internal/infra/db/`
- [ ] No secrets, keys, or env vars appear in log output
- [ ] `context.Context` threaded through all new I/O operations
- [ ] New public behaviour documented (if applicable)
