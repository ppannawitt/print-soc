# Print @ SoC release readiness

The Mac app targets macOS 13 or later on Apple Silicon and Intel. Development builds are ad-hoc signed and explicitly labeled; they are not public release artifacts.

Public release gates:

- Go race tests, static checks, frontend regression tests, native PDF/bridge tests, and dependency vulnerability scan pass.
- Visual and accessibility checks pass for every tab in light and dark appearance at minimum window size and Retina resolution.
- Fresh installation and upgrade from 0.2 preserve settings, existing Keychain entries, and history on real Apple Silicon and Intel Macs, including macOS 13.
- With a real SoC account and explicit control of the test document, confirm direct and jump-host printing, queue reading, and owned-job cancellation. Confirm printed orientation, scaling, n-up, copies, duplex, and banner behavior.
- Release assets contain no credentials, user settings, history, or selected documents. Third-party notices and SHA-256 checksums are included.
- Developer ID signing, hardened runtime, Apple notarization, ticket stapling, and Gatekeeper validation succeed for app and DMG.

Developer ID credentials are not available yet. Signing and notarization cannot be completed until the release maintainer configures them. CI and local fixtures cannot substitute for hardware compatibility, VoiceOver review, or a real printer smoke test. No claim of universal device safety is made.

Build a review artifact with `MODE=development ./scripts/build-macos-app.sh`. Build a production artifact only after the above gates with `MODE=production DEVELOPER_ID='Developer ID Application: …' NOTARY_PROFILE='…' ./scripts/build-macos-app.sh`. Store certificates and notarization credentials in Keychain; never put them in source control. Production mode fails without them.

Connections and PDF processing happen directly on the user's Mac. There is no hosted backend or telemetry. The app remains an independent student project, not an official NUS application.

## Validation record — 8 October 2026

Verified locally on the development Apple Silicon Mac:

- `go test -race ./...` and `go vet ./...` pass.
- Frontend fixtures cover delayed connection failure after navigation, non-overlapping queue polling, explicit retry, stale queue/account replies, trust cancellation, and immutable print options.
- Native fixtures cover mixed page sizes and intrinsic rotations, ordered selection, A4/A3, both orientations, fit/fill/custom scaling, every supported n-up layout, copies, rendering content, preview/output agreement, locked/malformed/oversized files, invalid handles, temporary permissions and cleanup, duplicate submission rejection, and abandoned-session recovery without deleting live sessions.
- Native validation rejects malformed request types, raw PDF paths, unknown actions, and untrusted origins. The URL/frame guard is tested at helper level; full hostile WKFrame injection has not been exercised.
- Universal arm64/x86_64 packaging and strict ad-hoc signature checks pass. The bundle includes dependency notices and passes the artifact allowlist/private-key-content scan.
- `govulncheck` reports no reachable or imported-package findings after updating `golang.org/x/crypto` to 0.56.0. It reports module advisory GO-2026-5932 for the unmaintained OpenPGP package, which this app does not import or call. There is no fixed module version for that advisory; retain this finding in future release reviews.

Still unverified: final full visual/VoiceOver/keyboard review of every tab in both appearances, minimum-size and Retina checks, macOS 13, physical Intel installation and upgrade, real Keychain authentication/clearing flows, a controlled direct/jump-host SoC print/queue/cancel smoke test, and Developer ID/notarization/Gatekeeper acceptance. The final green development app was launched, and its accessibility tree confirms the top banner is removed and the print controls and native menus are exposed. The flat Account screen was visually checked in light appearance, including larger section headings and the separator between account and key settings. Complete visual review is still pending; the UI automation tool had intermittent bundle selection and stale-element failures. These remain release blockers; a successful cross-compile does not establish runtime compatibility on Intel.

Local PDF snapshots intentionally contain document contents in protected temporary files. Normal release/shutdown removes them; recovery retries abandoned session cleanup on the next launch. Abrupt termination can leave those private files until recovery. Remote cleanup failures are reported and require manual follow-up; no guarantee is made that an unreachable server has removed an upload.

## Follow-up regression fixes — 8 October 2026

The printer renderer's return/newline error is fixed and tested against the full embedded catalog, searching, and restricted-printer filtering. The directory initially includes restricted entries with access labels; they remain unavailable for student print submission. The public-key visibility control is removed.

Private-key parsing now uses the SSH signer returned by the parser directly, used for local public-key export and jump-host login. Fixtures reproduce the former unsupported-signer failure and verify encrypted/unencrypted Ed25519 public-key export, encrypted/unencrypted Ed25519/RSA/ECDSA signing, missing and incorrect passphrase rejection, leftover passphrases with unencrypted keys, malformed/oversized key rejection, and actual key-only authentication against a local SSH fixture. Local key unlock errors are distinguished from SoC password authentication. No real SoC password or enrollment request was sent during this verification. Enrollment is manual in v1.

## V1 scope and publication

Automatic SSH-key registration has been removed from the frontend, native bridge, and transport implementation. Key creation and public-key copying remain local, and users register keys manually with SoC. Existing saved keys and settings remain readable. Regression checks cover local-only key creation/export, abandoned export after navigation, and rejection of the removed bridge action.

The GitHub Pages website has download, printing, and manual SSH-key guide pages. It leaves the download disabled until a stable production release contains the universal DMG, checksum, and validation record. Production packaging also rejects an incomplete or outdated manual validation record. The GitHub production workflow requires signing/notarization secrets and all recorded checks; it never falls back to a development artifact.

V1 source and documentation are ready for repository review. Public app release is still blocked by missing Developer ID credentials and the unverified real-device, accessibility, Keychain, and SoC smoke checks described above. The discontinued key-service integration is outside v1 and no longer a release gate.
