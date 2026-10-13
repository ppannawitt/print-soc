# Security

Print @ SoC is an independent student project. Please report vulnerabilities privately through this repository’s GitHub Security Advisories if enabled, rather than posting credentials or sensitive documents in a public issue.

Do not include SoC passwords, private SSH keys, key passphrases, Keychain exports, personal PDFs, or unredacted diagnostics in a report. A minimal reproduction and affected version are usually sufficient.

Signed production Mac builds require Developer ID signing, notarization, stapling, Gatekeeper validation, and the manual release checks in `docs/RELEASE-READINESS.md`. The separately authorized ad-hoc distribution method is not Developer ID signed or notarized. No guarantee of universal device safety is made.

The app can connect to SoC's SSH Key Service after the user chooses automatic jump setup. It uses the SoC password and verifies the SSH host fingerprint through the existing confirmation flow. It submits only complete public-key lines after confirming that every currently registered jump-key fingerprint is represented. Private keys and generated passphrases stay on the Mac; passphrases are kept in Keychain by account and key fingerprint. If existing public keys cannot be recovered or the configuration changes during setup, enrollment stops without submitting a partial list. An uncertain service response triggers a read-back before another attempt.
