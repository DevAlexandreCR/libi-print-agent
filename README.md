# libi-print-agent

Small Windows tray agent that receives print jobs from the LiBi API and
writes them as RAW ESC/POS to a named Windows printer through the spooler.
See `openspec/changes/add-print-agent/design.md` (D1-D12) in the main
`libi` repo for the full design; this README only covers building,
testing, releasing and on-site diagnostics for this subproject.

Windows-only at runtime (winspool, DPAPI, Task Scheduler autostart); the
module builds and tests on macOS/Linux/Windows, and the `.exe` itself is
always cross-compiled for `GOOS=windows GOARCH=amd64`.

## Building locally

This Mac (and most dev machines) has no local Go toolchain installed on
purpose. `scripts/build.sh` uses the local `go` if present, otherwise falls
back to the official `golang` Docker image automatically:

```bash
make tidy           # go mod tidy
make vet             # go vet ./... (native OS/arch)
make test            # go test ./...
make vet-windows      # go vet ./... with GOOS=windows (type-checks windows-only files)
make build-windows    # cross-compile dist/libi-print-agent.exe
make clean            # rm -rf dist
```

`build-windows` accepts optional build-time overrides via env vars (used by
the release workflow, see below); unset, a local build is `version=dev`,
`defaultAPIBase=""`, `requireSignature=true`:

- `LIBI_PRINT_AGENT_VERSION`
- `LIBI_PRINT_AGENT_API_BASE`
- `LIBI_PRINT_AGENT_REQUIRE_SIGNATURE`

Never run `go` commands directly against this repo without CGO
considerations in mind: the binary is built with `CGO_ENABLED=0` (default
in `scripts/build.sh`) so cross-compilation from macOS/Linux works without
a C toolchain.

## CI

`.github/workflows/ci.yml` runs on every push and pull request, on
`ubuntu-latest`:

1. `gofmt` check (fails if any file needs reformatting).
2. `go vet ./...` and `go test ./...` (native, Linux).
3. `go vet ./...` with `GOOS=windows` (type-checks the Windows-only build
   tags — DPAPI, winspool — that the steps above never touch).
4. Cross-compiled, **unsigned** Windows build, uploaded as a build
   artifact for inspection. This build is never published; it exists to
   catch a broken Windows-only build before a release tag is pushed.

## Releasing

`.github/workflows/release.yml` runs on `windows-latest` when a tag
matching `v*.*.*` (e.g. `v1.2.0`) is pushed:

1. Runs `go test ./...`.
2. Builds `dist/libi-print-agent.exe` for `windows/amd64` with
   `-X main.version=<tag without the leading v>` and
   `-X main.defaultAPIBase=<the LIBI_PRINT_AGENT_API_BASE repo variable,
   defaulting to https://api.libibot.com/api>`.
3. Signs `dist/libi-print-agent.exe` with `signtool sign` using an RFC3161
   timestamp, then verifies with `signtool verify /pa` — **only if** the
   signing secrets below are configured (see "Unsigned v0.x test builds").
4. Computes the SHA-256 of the (signed, or unsigned test) exe, and
   publishes a GitHub Release with three files: `libi-print-agent-
   <version>.exe`, the stable-named `libi-print-agent.exe` (always the
   newest release — see "Stable download URL"), and `SHA256SUMS`.
5. Prints the values below, plus the stable download URL, in the job
   summary.

### To cut a release

1. Push a tag: `git tag v0.0.2 && git push origin v0.0.2`.
2. Wait for the `release` workflow to finish; open its job summary.
3. Set on the API server's environment (see the main repo's
   `libi-api/.env` / deployment config):
   - `PRINT_AGENT_LATEST_VERSION`
   - `PRINT_AGENT_LATEST_URL`
   - `PRINT_AGENT_LATEST_SHA256`

   These are served by `GET /print-agents/version` and are what a paired
   agent's self-update check compares against.
4. Set `VITE_PRINT_AGENT_DOWNLOAD_URL` on `libi-web` to the stable
   download URL below (the merchant-facing settings page links it for the
   initial install; it defaults to that same URL when unset, so this step
   is only needed to override it).

### Stable download URL

Every release also publishes the same exe under a fixed, version-less
name, so a bookmark or an env var never has to change on a new release:

```
https://github.com/DevAlexandreCR/libi-print-agent/releases/latest/download/libi-print-agent.exe
```

`/releases/latest/` only resolves to a release that is **not** marked
draft or prerelease — the workflow always publishes a normal release, so
this keeps working for every tag, signed or not.

### Unsigned v0.x test builds

`WINDOWS_CERT_PFX_BASE64` / `WINDOWS_CERT_PASSWORD` are optional while the
repo is still on `v0.x` tags:

- **Secrets present** → the release is signed and verified as described
  above, built with `requireSignature=true`. This is required for every
  `v1.0.0+` tag; the workflow **fails the job and publishes nothing** for
  a 1.x+ tag if the secrets are missing (design.md D12 — a code-signing
  certificate is a rollout prerequisite before any merchant install).
- **Secrets absent and the tag is `v0.x`** → the workflow skips signing,
  builds with `LIBI_PRINT_AGENT_REQUIRE_SIGNATURE=false` (the agent's own
  self-update then skips Authenticode verification too — it still always
  verifies the sha256 first), and marks the release notes and job summary
  **"UNSIGNED TEST BUILD"**. Use these only for internal testing, never
  for a merchant install (see SmartScreen below).

### Signing prerequisites

- `WINDOWS_CERT_PFX_BASE64` — the code-signing certificate, PFX format,
  base64-encoded (`base64 -w0 cert.pfx` or `[Convert]::ToBase64String(...)`
  on Windows), stored as a GitHub Actions **secret** on this repo.
- `WINDOWS_CERT_PASSWORD` — the PFX's password, also a secret.
- `LIBI_PRINT_AGENT_API_BASE` — the production API base URL baked into the
  build, stored as a GitHub Actions **repo variable** (not a secret; it is
  not sensitive). Optional — defaults to `https://api.libibot.com/api`.

### Code signing setup

A code-signing certificate is a rollout prerequisite (design.md D12)
before the first merchant install, and clears Windows SmartScreen
reputation over time (see below). Any OV or EV certificate works for
this — OV is cheaper and sufficient; EV/cloud-HSM signing just clears
SmartScreen reputation faster.

Any OV/EV certificate issued since 2023 (CA/Browser Forum baseline
requirements) must be held on a hardware token or an HSM and **cannot be
exported as a `.pfx`** — so for a newly purchased cert, the PFX +
`signtool` step in `release.yml` will not apply; use a cloud signing
service instead:

1. Buy an OV (or EV) code-signing certificate from a CA that offers cloud
   / HSM-backed signing — e.g. **SSL.com eSigner** — and complete their
   business-identity validation (this takes real time; start it before
   it blocks a release).
2. Add `ES_USERNAME`, `ES_PASSWORD`, `ES_CREDENTIAL_ID`, `ES_TOTP_SECRET`
   as GitHub Actions secrets on this repo (from the SSL.com eSigner
   account setup).
3. In `release.yml`, delete the "Decode signing certificate" and "Sign
   and verify exe" steps and uncomment the commented-out
   `sslcom/esigner-codesign` step right below them (it is left in place,
   pre-wired to those same secrets, specifically so this swap doesn't
   require re-deriving the step).
4. Push a `v1.0.0+` tag — the workflow will now sign every release, and
   nothing below `v1.0.0` needs to stay unsigned once this is done.

Azure Trusted Signing is an equivalent alternative; swap in its own
action/CLI the same way.

### SmartScreen

A signed release exe still needs to build up Microsoft SmartScreen
reputation; it is not instant even when correctly signed. An **unsigned**
build (e.g. the CI artifact) will be flagged by SmartScreen — this is
expected and such builds are for internal testing only, never for a
merchant install. To run one anyway for testing: on the SmartScreen
warning dialog, click **"Más información"** and then **"Ejecutar de todas
formas"**.

## On-site diagnostics

Hidden CLI flags for support and for the D11 discovery checklist (run
before writing any RAW-printing code against a new merchant PC):

```bash
libi-print-agent.exe --list-printers        # print the Windows printer inventory and exit
libi-print-agent.exe --print-test "<printer name>"   # send a RAW ESC/POS test ticket and exit
libi-print-agent.exe -pair <code> -api <api base url> # pre-authenticate before the normal startup sequence (testing/support only)
```

`--list-printers` and `--print-test` work without a paired agent config;
use them to confirm a printer is reachable via RAW spooler printing (cuts,
does not open the cash drawer while the POS is open) before pairing the
agent for real.
