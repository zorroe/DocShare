# Repository Guidelines

## Project Structure & Module Organization

DocShare is a Windows-focused Go application with two entry points: `backend/main.go` builds the server, while `desktop/` contains the Wails v2 shell. Shared code lives under `internal/`: HTTP handlers and the embedded vanilla frontend are in `internal/api/`, document indexing and persistence are in `internal/store/`, and Windows integration is split across `config/`, `autostart/`, and `tray/`. Sample content belongs in `docs/`. Go tests sit beside their packages; the Edge/Puppeteer smoke suite is in `test/`. Treat `release/` and `desktop/build/` as generated output.

## Build, Test, and Development Commands

- `go run ./backend -dir docs -addr 127.0.0.1:8080` starts the server against the sample documents.
- `go test ./...` runs all Go tests.
- `powershell -ExecutionPolicy Bypass -File test\run.ps1` rebuilds the server, starts it on a random port, and runs the UI smoke suite. It requires Node.js and Microsoft Edge; dependencies install automatically.
- `.\build.bat` creates `release\DocShare-Server.exe`.
- `.\build_desktop.ps1` validates versions and builds the portable app plus the optional NSIS installer.

Use Go 1.25, matching `go.mod`. WebView2 is required for the desktop app.

## Coding Style & Naming Conventions

Format Go changes with `gofmt`; use tabs as emitted by the formatter, short lowercase package names, exported `PascalCase` identifiers, and unexported `camelCase` identifiers. Keep platform-specific implementations in `*_windows.go` and portable alternatives in `*_other.go`. Frontend JavaScript uses two-space indentation, semicolons, `camelCase` functions, and uppercase constants. Preserve the dependency-free HTML/CSS/JS architecture and existing Chinese user-facing copy unless a feature requires otherwise.

## Testing Guidelines

Name Go tests `TestBehavior` and use `t.TempDir()` for filesystem cases. Add focused regression coverage near the changed package. UI behavior belongs in `test/ui-test.js`; keep tests deterministic and wait for observable page state rather than fixed delays. No numeric coverage threshold is enforced, but both `go test ./...` and the UI suite should pass before release-facing changes.

## Commit & Pull Request Guidelines

History uses concise, scoped prefixes such as `修复:`, `新增:`, `CI:`, and `docs:`; release commits use `vX.Y.Z:`. Follow that pattern and explain the user-visible outcome. Keep commits single-purpose. Pull requests should summarize behavior, list verification commands, link relevant issues, and include screenshots for UI changes. Call out Windows, WebView2, installer, or configuration impacts explicitly.

## Security & Generated Files

Never commit passwords, tokens, runtime `data/`, logs, executables, or temporary test artifacts. Preserve path-traversal, authentication, blacklist, and HTML-sanitization protections when changing file or HTTP handling.
