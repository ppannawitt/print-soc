# Print @ SoC release readiness

The Mac app targets macOS 13 or later on Apple Silicon and Intel. Development builds are ad-hoc signed and explicitly labeled; they are not public release artifacts.

Signed production release gates:

- Go race tests, static checks, frontend regression tests, native PDF/bridge tests, and dependency vulnerability scan pass.
- Visual and accessibility checks pass for every tab in light and dark appearance at minimum window size and Retina resolution.
- Fresh installation and upgrade from 0.2 preserve settings, existing Keychain entries, and history on real Apple Silicon and Intel Macs, including macOS 13.
- With a real SoC account and explicit control of the test document, confirm direct and jump-host printing, queue reading, and owned-job cancellation. Confirm printed orientation, scaling, n-up, copies, duplex, and banner behavior.
- Confirm automatic jump-key enrollment on a controlled account, including fingerprint-only `show` output, preservation of multiple existing keys, missing public-key handling, changed-list cancellation, and post-enrollment key-only access.
- Confirm a generated encrypted key unlocks after app restart from Keychain, per-account passphrases remain isolated when switching accounts, and the app aborts managed-key setup if Keychain persistence fails.
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

Still unverified: final full visual/VoiceOver/keyboard review of every tab in both appearances, minimum-size and Retina checks, macOS 13, physical Intel installation and upgrade, real Keychain authentication/clearing flows, a controlled direct/jump-host SoC print/queue/cancel smoke test, and Developer ID/notarization/Gatekeeper acceptance. The final green development app was launched, and its accessibility tree confirms the top banner is removed and the print controls and native menus are exposed. The flat Account screen was visually checked in light appearance, including larger section headings and the separator between account and key settings. Complete visual review is still pending; the UI automation tool had intermittent bundle selection and stale-element failures. These remain blockers for the fully validated signed production workflow; a successful cross-compile does not establish runtime compatibility on Intel.

Local PDF snapshots intentionally contain document contents in protected temporary files. Normal release/shutdown removes them; recovery retries abandoned session cleanup on the next launch. Abrupt termination can leave those private files until recovery. Remote cleanup failures are reported and require manual follow-up; no guarantee is made that an unreachable server has removed an upload.

## Follow-up regression fixes — 8 October 2026

The printer renderer's return/newline error is fixed and tested against the full embedded catalog, searching, and restricted-printer filtering. The directory initially includes restricted entries with access labels; they remain unavailable for student print submission. The public-key visibility control is removed.

Private-key parsing now uses the SSH signer returned by the parser directly, used for local public-key export and jump-host login. Fixtures reproduce the former unsupported-signer failure and verify encrypted/unencrypted Ed25519 public-key export, encrypted/unencrypted Ed25519/RSA/ECDSA signing, missing and incorrect passphrase rejection, leftover passphrases with unencrypted keys, malformed/oversized key rejection, and actual key-only authentication against a local SSH fixture. Local key unlock errors are distinguished from SoC password authentication. No real SoC password or enrollment request was sent during this verification. Enrollment is manual in v1.

## V1.0.0 developmental release

Automatic SSH-key registration has been removed from the frontend, native bridge, and transport implementation. Key creation and public-key copying remain local, and users register keys manually with SoC. Existing saved keys and settings remain readable. Regression checks cover local-only key creation/export, abandoned export after navigation, and rejection of the removed bridge action.

The GitHub Pages website has download, printing, and manual SSH-key guide pages. The v1 download links to the published universal DMG and checksum and discloses its unsigned/not-notarized status. Production packaging also rejects an incomplete or outdated manual validation record. The GitHub production workflow requires signing/notarization secrets and all recorded checks; it never falls back to a development artifact.

V1.0.0 was a developmental release. V1.0.1 was the first public launch and corrected the macOS deployment target. V1.1.0 adds automatic SSH jump-host key enrollment. The maintainer reports thorough testing and explicitly authorized unsigned v1.0.1 publication on 8 October 2026. The device-specific checks listed above have not been independently observed by the agent; the maintainer statement is recorded separately rather than inventing per-device evidence. V1.0.1 is ad-hoc signed, not Developer ID signed or Apple-notarized, and Gatekeeper acceptance is not asserted.

## V1.1.0 SSH jump setup

V1.1.0 adds automatic SSH jump-key setup as an explicit user action. The app checks a selected key with key-only authentication to the jump host and then checks password access to the Unix server. The setup banner appears only for an absent or rejected key; passphrase, network, host-verification, and account-authentication conditions have separate handling. Background checks are coalesced and do not block print or queue requests.

Enrollment reads the jump-key fingerprint section from the SoC service, discovers matching public-key lines locally, and asks for missing lines before submission. It rechecks the list before sending the complete merged key list and verifies both the service response and resulting fingerprints. Managed keys are encrypted Ed25519 keys with random 32-byte passphrases saved to Keychain before remote enrollment. Custom key selection and manual registration remain available. The implementation has automated coverage for parsing, key preservation, interrupted responses, UI status handling, duplicate suppression, and account-change invalidation.

The 1.1.0 validation record must continue to mark real-device, Keychain-after-restart, and controlled SoC enrollment checks incomplete until they are observed. The app-only universal DMG, test results, and source fingerprint are recorded separately from those checks. An ad-hoc build does not claim notarization, Gatekeeper acceptance, macOS 13 runtime, or physical Intel-device verification.

## V1.1.0 observed validation — 13 October 2026

The Go race suite, `go vet`, frontend and website tests, release-record fixtures, source scan, dependency integrity check, and native PDF/bridge tests pass. The dependency scan with Go 1.27.2 found no reachable vulnerabilities and one advisory in a required module that the application does not call. Go 1.27.2 is used because its security fixes remove the earlier standard-library findings.

The rebuilt universal DMG passes disk-image verification and strict ad-hoc signature checks. Its root contains only `SimplyPrint @ SoC.app`; both executable slices target macOS 13.0, and the app reports version 1.1.0/build 110. The exact SHA-256 and observed package details are in `release/validation.json`.

The validation record leaves hardware, accessibility, real printing, Keychain restart, and app-driven SoC enrollment checks incomplete. The supplied terminal transcript documents a successful manual `jump` registration and its ED25519 fingerprint, but does not verify that the new application preserved existing registrations or authenticated using the enrolled key. A prior isolated GUI launch attempt aborted inside the sandbox, so a successful 1.1.0 GUI launch is not claimed. The requested ad-hoc distribution authorization is recorded; the production gate continues to reject incomplete manual evidence.

## V1.0.1 packaging correction — 8 October 2026

The original v1.0.0 executable contained `LC_BUILD_VERSION minos 26.0` on both architectures even though Info.plist advertised macOS 13.0. Compiler flags alone did not constrain the final external link. V1.0.1 explicitly sets the deployment target for compilation and linking. Packaging now inspects both Mach-O slices and rejects any minimum OS version other than 13.0 before publication. This corrects an actual compatibility bug; it does not independently prove runtime behavior on an older Mac or Intel device.

The app bundle, native menus, embedded UI and DMG are renamed SimplyPrint @ SoC. Bundle ID, credential service names and settings/history paths remain compatible. The disk image root contains only the app bundle. Dependency notices stay inside the app bundle; opening guidance and signing information stay on the website and release notes. The release remains ad-hoc signed and not Apple-notarized.
