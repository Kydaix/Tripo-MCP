# Tripo-MCP

Native Windows CLI and stdio MCP client for the owner's Tripo Studio subscription.

- Answer the owner in French. Code and comments are in English.
- One Go executable; a temporary loopback page receives a manually transferred session from any normal browser. No browser control or extension.
- Never use the public Tripo API or its separate API credits as a fallback.
- Studio endpoints are undocumented. Validate payload changes against the current Studio application, not third-party code.
- Never copy code from Ador-able/tripo-studio-mcp; its license does not allow reuse.
- No passwords, session tokens, signed download URLs or account identifiers in logs, fixtures, commits or tool responses. Windows DPAPI protects persisted sessions.
- Human sign-in, manual session transfer and CAPTCHA stay with the user. Never reintroduce a controlled-browser login or attempt to avoid human checks. A manually transferred Studio session cookie may renew access tokens through Studio's own whoami endpoint. Expired or revoked primary sessions require a new manual transfer. Never read browser credential stores.
- Ask before live generation or other credit-consuming tests. Read-only tests are fine.
- Never automatically repeat a generation after an uncertain response.
- Run `go vet ./...` and `go test ./...` before publishing. Build Windows amd64 and arm64 releases with SHA256SUMS.txt.
- Keep ai-setup's installation integration in its own repository.
