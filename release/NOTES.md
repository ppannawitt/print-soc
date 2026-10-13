SimplyPrint @ SoC v1.1.0 for macOS 13 or later, on Apple Silicon and Intel.

## What’s new

This release adds guided SSH-key enrollment for the SoC jump host. After connecting, reloading a queue, or printing through direct SoC access, the app checks the selected key and sends a macOS notification if setup is needed. Jump verification requires the key to be accepted; if the server then requests the SoC password, the saved password completes that additional step. Password-only access never counts as a working key. Users can enroll their selected key, choose another key, or create an encrypted managed Ed25519 key. Managed-key passphrases are generated randomly and stored in macOS Keychain by account and key fingerprint.

Before enrollment, the app reads the current jump-key fingerprints and recovers matching public-key lines from local public-key files. It asks for any missing lines and stops if every registered key cannot be included or the configuration changes. It submits the complete merged list, verifies the service response and resulting fingerprints, then tests jump access. Interrupted results are checked before another enrollment can be attempted. Existing private-key files and saved credentials are preserved. Custom key selection, passphrase entry, public-key copying, and manual registration remain available.

The network-dependent false key-rejection bug is fixed: a jump server's accepted public key followed by a required password is now handled correctly. A confirmed registration remains successful if the later access check cannot complete. Enrolling an already registered key checks access without resubmitting the key list.

## Printing and network access

Print PDFs with the SimplyPrint @ SoC app for macOS or through SSH from Terminal or Command Prompt. On SoC campus, NUS Wi-Fi such as `nus_stu` provides direct access from most locations. COM2 and other campus locations without direct access require the SoC SSH jump host. Off-campus printing may require NUS VPN.

## Release history

- **1.0.0 — Developmental:** early development release.
- **1.0.1 — First launch:** first public launch of SimplyPrint @ SoC.
- **1.1.0 — SSH jump-host setup:** guided automatic enrollment for the SoC SSH jump host.

The app and package version are 1.1.0; the bundle build is 110. Windows is unchanged.

Download `SimplyPrint-at-SoC-Universal.dmg`, open it, and drag the app into Applications in Finder.

**Signing status:** ad-hoc signed, not Developer ID signed and not notarized by Apple. If macOS says the developer cannot be verified, first try opening the app, then go to System Settings → Privacy & Security → Open Anyway, review the app name, and choose Open. Follow [Apple’s opening guide](https://support.apple.com/en-gb/102445). Do not override a malware or damaged-app warning.

[Printing guide](https://ppannawitt.github.io/print-soc/printing.html) · [SSH-key setup](https://ppannawitt.github.io/print-soc/ssh-keys.html)

Automated checks and unresolved device-specific or live SoC checks are recorded in `release-validation.json`. This is an independent student project, not an official NUS application.
