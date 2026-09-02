# Task 2 report

## Changed files

- Removed the tracked `web/classic/` frontend (457 files).
- Removed classic build, embed, CI, release, formatter, and lint references.
- Changed the web router and embedded assets to expose only `web/dist`.
- Kept `theme.frontend` database migration/read compatibility and documented that it is normalized to `default` without runtime frontend switching.
- Updated the OpenAPI descriptions for the compatibility key.
- Added regression coverage for unconditional legacy-route rewriting and web index serving.

## Trash method

Validated the exact path `C:\Users\Administrator\codex-1\newapi\web\classic`, then ran:

```powershell
Add-Type -AssemblyName Microsoft.VisualBasic
[Microsoft.VisualBasic.FileIO.FileSystem]::DeleteDirectory(
  'C:\Users\Administrator\codex-1\newapi\web\classic',
  [Microsoft.VisualBasic.FileIO.UIOption]::OnlyErrorDialogs,
  [Microsoft.VisualBasic.FileIO.RecycleOption]::SendToRecycleBin
)
```

Output: `TRASH_RESULT=RecycleOption.SendToRecycleBin`; `SOURCE_EXISTS=False` after completion. The directory contained 110,960 files on disk and 457 tracked files.

## Checks

- Reference scan: no build/embed/CI/release/router classic references remain outside task documentation and project policy text.
- `CLASSIC_SOURCE_EXISTS=False` after the Recycle Bin move.
- WSL: `go test ./common -run TestThemeAwarePathAlwaysRewritesLegacyConsoleRoutes` — PASS.
- WSL: `go test ./router -run TestSetWebRouterServesIndexPageForWebAndWorkbenchRoutes` — PASS.
- `git diff --check` and `git diff --cached --check` — PASS.
- `bun run typecheck` — PASS.
- `bun run lint` — PASS (existing repository warnings only; exit 0).
- `bun run build` — PASS.
- Broad WSL `go test ./common ./router` was also run but remains red on unrelated baseline assertions: image-task cache retention expects 12h but gets 72h; referral admin router tests receive 401 rather than expected 403/200; public image-task OpenAPI field-count test expects 128 but gets 1.

## Remaining compatibility references

`theme.frontend` remains in the migration, option validation, compatibility tests, and legacy config load hook so existing installations are readable. It is normalized to `default`, omitted from active settings, and cannot select a frontend. Generic legacy protocol comments and unrelated legacy-route tests remain unchanged.
