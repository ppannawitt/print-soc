# Security

Print @ SoC is an independent student project. Please report vulnerabilities privately through this repository’s GitHub Security Advisories if enabled, rather than posting credentials or sensitive documents in a public issue.

Do not include SoC passwords, private SSH keys, key passphrases, Keychain exports, personal PDFs, or unredacted diagnostics in a report. A minimal reproduction and affected version are usually sufficient.

Public Mac builds require Developer ID signing, notarization, stapling, Gatekeeper validation, and the manual release checks in `docs/RELEASE-READINESS.md`. Development builds are not notarized and are not public release artifacts. No guarantee of universal device safety is made.

V1 does not connect to the SSH Key Service or modify the list of registered keys. Public-key export is local; registration is performed manually by the user.
