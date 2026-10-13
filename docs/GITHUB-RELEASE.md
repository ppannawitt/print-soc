# GitHub and website release

Repository: https://github.com/ppannawitt/print-soc

Website: https://ppannawitt.github.io/print-soc/

The website is a static GitHub Pages site in `site/`. It has Download, Printing, and SSH keys pages. It uses no analytics, cookies, credentials, or printing backend. GitHub may retain ordinary hosting/request logs under its own privacy terms.

## Website deployment

In repository Settings → Pages, select **GitHub Actions** as the build source. The Website workflow deploys `site/` when it changes on `main`, or when dispatched manually. No custom domain is required. Repository settings may require owner access.

The home page queries GitHub’s public latest-release API. It enables a download only for a non-draft, non-prerelease version with the universal DMG, checksum, and `release-validation.json`. Development assets and release candidates never activate the main download. The static link remains on the last published release until the new release assets are available. The app’s official download filename stays `SimplyPrint-at-SoC-Universal.dmg` across stable releases.

## Signed production release

The production workflow requires every manual release check, a protected GitHub `production` environment, a Developer ID certificate, and Apple notarization credentials. It tests and scans the source, builds the universal app, signs with hardened runtime, notarizes and staples the app and DMG, validates Gatekeeper, and uploads a draft release. It publishes only after all preceding steps pass. Signing material is removed even after failure.

Before dispatching it, run `python3 scripts/check-release-gates.py --fingerprint`, record actual evidence for every release check in `release/validation.json`, and commit the completed record with the intended source. Then dispatch **Production Mac release** from `main` with the desired version. The gate fails when source or validation evidence changes.

Local production packaging requires the same validation gate plus a Developer ID identity and a `notarytool` Keychain profile. Use `MODE=production VERSION=1.1.0 DEVELOPER_ID='Developer ID Application: …' NOTARY_PROFILE='…' ./scripts/build-macos-app.sh` only when those credentials are configured.

## Authorized ad-hoc distribution for v1.1.0

The project maintainer explicitly authorized v1.1.0 distribution through the existing ad-hoc signed method. Build the universal app-only DMG with `MODE=unsigned VERSION=1.1.0 OUTPUT_DIR=/private/tmp/socprint-release ./scripts/build-macos-app.sh`. This creates the app bundle, DMG, and SHA-256 checksum without claiming Developer ID signing, notarization, or Gatekeeper acceptance. Include the current `release/validation.json` as a separate release asset and keep unverified device and SoC checks marked incomplete.

Create a draft `v1.1.0` release at the exact source commit. Upload `SimplyPrint-at-SoC-Universal.dmg`, its `.sha256` file, and `release-validation.json`; verify the uploaded checksum, then publish it as latest. Confirm the website download and checksum resolve to the public assets, and test a fresh download. If release upload fails, leave any incomplete release as a draft and resolve it before retrying. Do not overwrite an existing tag or release.

The website’s static download fallback should be updated to v1.1.0 only after the public assets exist. Deploy the website, then verify the latest-release API lookup, fresh DMG download, checksum, and guide links.
