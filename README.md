# Print @ SoC

Print @ SoC is an independent student printing app for NUS School of Computing. The Mac app uses Go, Cocoa, WebKit, PDFKit, and Core Graphics. Connections run directly from the Mac to SoC over SSH/SFTP; there is no hosted backend or telemetry.

## Mac app

V1 targets macOS 13 or later on Apple Silicon and Intel. The universal development DMG is `dist/Print-at-SoC-Universal-Development.dmg`. It is ad-hoc signed, explicitly labeled Development, and not notarized. Public distribution is blocked until the [release gates](docs/RELEASE-READINESS.md) are complete.

Every tab uses compact flat rows, green accents, system typography, separators, and system-aware light/dark appearance. The repeated top banner and sidebar connection label are removed. Notifications use native macOS alert sheets. Print keeps document preview beside the settings and the Print button in a fixed footer. File → Open PDF (⌘O), File → Print (⌘P), Account Settings (⌘,), and About are native menu commands.

The printer directory includes search, locations, capabilities, and restricted access details. Jobs retains history and print settings. Queues retains the last result while refreshing, uses native macOS alerts for connection failures, and stops automatic refresh after failure. A delayed connection response cannot navigate the app. Account remains accessible for credential changes and manual SSH-key setup.

## Printing

Choose a readable, unlocked PDF up to 1 GB. Options include all pages, ranges such as `1-3, 5`, thumbnail selection, 1–99 copies, portrait/landscape, auto-rotation, fit/fill/custom percentage scaling, and 1/2/4/6/9/16 pages per sheet. Paper, colour, sides, and banner behavior follow the catalog; unavailable queue combinations are disabled rather than substituted.

PDF preparation is local. Opening a document creates a private snapshot so preview and preparation use the same bytes. Preview and submitted PDF share the same vector drawing calculation, including a 6.35 mm content margin. Copies are sent to the spooler rather than duplicating PDF pages. Before sending, confirmation shows the document, pages, copies, paper, layout, printer, location, and exact queue.

The app uploads the prepared PDF into a private temporary directory on SoC, submits it with `lpr`, and attempts remote cleanup. Submitted means the server accepted the command; it does not confirm physical printing. An uncertain result remains in history and never triggers automatic resubmission. Cancellation requires a matching recorded operation and a fresh ownership check against the server queue.

## Account and privacy

An active SoC account with Unix access is required. Help includes account setup, VPN, and manual SSH-key registration guides. The app can create a key locally and copy its public key, but never registers it automatically or changes SoC’s registered-key list. The app tries the Unix server directly, then the jump host when necessary. Network, missing key, rejected key, incorrect key passphrase, rejected password, and changed host identity errors remain distinct.

Passwords and key passphrases use macOS Keychain when saving succeeds. Vault failures are reported and credentials remain session-only. Settings and SQLite history are local; existing settings, Keychain entries, and history remain compatible. Host identities require explicit verification before first trust; a changed identity blocks the connection.

Embedded UI resources use a private app origin with a restrictive content security policy. Remote navigation, frames, and popups are blocked. Privileged requests require the trusted main frame and validated types. Document handles resolve to native-approved files; frontend requests cannot supply arbitrary PDF paths. Temporary directories and PDFs are private from creation and recovered after an interrupted app session. Diagnostics remain local.

## Build and verify

Building requires macOS, Go 1.26 or later, Apple command-line tools, Node.js 18 or later for frontend tests, and Python 3 for the artifact scanner. Running the built app does not require those tools.

```sh
go test -race ./...
go vet ./...
./scripts/test-macos.sh
go install golang.org/x/vuln/cmd/govulncheck@v1.8.0
govulncheck ./cmd/socprint-macos
MODE=development ./scripts/build-macos-app.sh
```

The build creates an arm64/x86_64 universal app and DMG, SHA-256 checksum, original green cloud/printer icon, and dependency notices. An artifact allowlist rejects bundled user data, unexpected files, symlinks, and private-key contents. CI runs the automated checks and uploads development artifacts; it does not publish releases.

Production mode requires an available Developer ID Application identity and notarization Keychain profile. It signs with hardened runtime, notarizes and staples the app and DMG, and validates signatures and Gatekeeper. Missing credentials fail the production build.

```sh
MODE=production DEVELOPER_ID='Developer ID Application: …' NOTARY_PROFILE='…' ./scripts/build-macos-app.sh
```

Complete the documented device, accessibility, installation/upgrade, and controlled SoC printing checks before production packaging and public distribution. Automated fixtures do not prove safety on every device.

## Project layout

- `cmd/socprint-macos`: Cocoa/WebKit app, native PDF engine, embedded frontend
- `internal/transport`: SSH/SFTP, host verification, printing, queue ownership, cancellation, recovery
- `internal/catalog`: printer capabilities and student queue validation
- `internal/credentials`, `internal/config`, `internal/store`: credential vault, settings, local history
- `tests` and `scripts/test-macos.sh`: frontend and native PDF/bridge regression fixtures
- `scripts/build-macos-app.sh`: universal development/production packaging

The existing terminal implementation under `cmd/socprint` and `internal/tui` is retained. Windows/Linux redesign and release, local printer support, and App Store distribution are outside this Mac release.

## Website and v1 publication

The [website](https://ppannawitt.github.io/print-soc/) contains the Mac download page, [printing guide](https://ppannawitt.github.io/print-soc/printing.html), and [manual SSH-key guide](https://ppannawitt.github.io/print-soc/ssh-keys.html). GitHub Pages publishes the static `site/` folder. The public download activates only when a verified production release is available; development builds are not promoted.

See [GitHub release setup](docs/GITHUB-RELEASE.md) for Pages configuration, signing secrets, manual validation, and the production release workflow. Version 1.0.0 is prepared as a development candidate; the outstanding release gates still block a public app release.

Source is under the [MIT license](LICENSE). The cloud/printer logo is original artwork and does not reproduce the SoC student club logo.
