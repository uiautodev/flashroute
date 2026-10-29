# AGENTS

## Testing is mandatory

Every change must be verified before it is considered done.

1. Backend changes: add or update Go unit tests and run `go test ./...`.
2. Frontend changes: run `cd frontend && npm run build`.
3. User-facing changes: verify the actual flow with Playwright (`playwright-cli`) against a temporary backend and the Vite dev server.
   - Use a temporary `FLASHROUTE_DATA_DIR` and non-default ports.
   - Create the required data through the API, then drive the UI.
   - Check the page result and the browser console for errors.
   - Stop the dev servers and delete temporary data/browser sessions afterwards.
4. Fix regressions found by tests before reporting completion.

## Code quality

- Keep code small, direct and readable.
- Prefer clear data structures and typed models over ad-hoc maps.
- Reuse existing helpers, components and converters; do not duplicate logic across files.
- Avoid dead configuration and unused code.
