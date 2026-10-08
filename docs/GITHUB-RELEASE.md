# GitHub and website release

Repository: https://github.com/ppannawitt/print-soc
Website: https://ppannawitt.github.io/print-soc/

The website is a static GitHub Pages site in `site/`. It has Download, Printing, and SSH keys pages. It uses no analytics, cookies, credentials, or printing backend. GitHub may retain ordinary hosting/request logs under its own privacy terms.

## Website deployment

In repository Settings → Pages, select **GitHub Actions** as the build source. The Website workflow deploys `site/` when it changes on `main`, or when dispatched manually. No custom domain is required. Repository settings may require owner access.

The home page queries GitHub’s public latest-release API. It enables the download only for a non-draft, non-prerelease version with the universal DMG, checksum, and `release-validation.json`. Development assets and release candidates never activate the main download. V1 is an explicitly authorized ad-hoc signed release; the website and release notes disclose that it is not Apple-notarized. A missing release or failed API request leaves the download unavailable and preserves the guides and Releases link. The app’s official download filename stays `SimplyPrint-at-SoC-Universal.dmg` across stable releases.

## Prepare a public app release

1. Complete every real-device and SoC check in `docs/RELEASE-READINESS.md` against the intended source. Run `python3 scripts/check-release-gates.py --fingerprint`, copy the result into `release/validation.json`, and record actual evidence for every check. Leave incomplete checks false. The fingerprint excludes the validation record itself so the record can be committed without invalidating it; code and test changes invalidate it.
2. Configure a protected GitHub environment named `production`. Restrict deployments to `main` and add a required maintainer reviewer if available for the repository.
3. Add environment secrets: `DEVELOPER_ID` (the full Developer ID Application identity), `DEVELOPER_ID_CERTIFICATE_BASE64` (P12 certificate export), `DEVELOPER_ID_CERTIFICATE_PASSWORD`, `NOTARY_APPLE_ID`, `NOTARY_TEAM_ID`, and `NOTARY_PASSWORD` (app-specific Apple password). Keep these in GitHub Secrets, never in source or chat.
4. Commit the completed validation record, review release notes, and dispatch **Production Mac release** from `main` with version `1.0.1`. The workflow tests and scans the source, imports the certificate into an ephemeral runner Keychain, builds the universal app, signs with hardened runtime, notarizes/staples the app and DMG, validates Gatekeeper, and uploads a draft release. It publishes only after all preceding steps pass. Signing material is removed even after failure.
5. Verify the public release assets, checksum, download button, fresh download, and guide links. If release upload fails, any incomplete release stays a draft. Resolve it explicitly before retrying; the workflow does not overwrite an existing tag or release.

Local production packaging requires the same manual-validation gate plus a Developer ID identity and notarytool Keychain profile. `MODE=development VERSION=1.0.1 ./scripts/build-macos-app.sh` is for maintainer review only.

## Unsigned v1 release — 8 October 2026

The maintainer reports thorough testing and explicitly authorized publication without Apple signing. Build this release using `MODE=unsigned VERSION=1.0.1 ./scripts/build-macos-app.sh`. This mode packages the normal app name and bundle identifier, uses ad-hoc signing, scans the bundle, and writes the DMG checksum. The DMG contains only the app bundle. Signing status and opening instructions are in the release notes and printing guide; this mode does not claim Developer ID, notarization, or Gatekeeper acceptance.

Create a draft `v1.0.1` release at the exact source commit, upload `SimplyPrint-at-SoC-Universal.dmg`, its `.sha256` file, and `release-validation.json`, verify uploaded digests, then publish it as latest. Keep the maintainer attestation distinct from independently observed device-specific checks. The signed production workflow above remains available and requires all its original signing and validation gates.
