#!/usr/bin/env bash
set -euo pipefail
project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
mode="${MODE:-development}"
version="${VERSION:-1.0.0}"
output_dir="${OUTPUT_DIR:-$project_root/dist}"
go_command="${GO:-$(command -v go || true)}"
if [[ -z "$go_command" && -x /usr/local/go/bin/go ]]; then go_command=/usr/local/go/bin/go; fi
if [[ -z "$go_command" || "$(uname -s)" != Darwin ]]; then
  echo 'Build requires macOS, Go, and Apple command-line tools.' >&2; exit 1
fi
if [[ "$mode" != development && "$mode" != unsigned && "$mode" != production ]]; then echo 'MODE must be development, unsigned, or production.' >&2; exit 1; fi
if [[ "$mode" == production ]]; then
  : "${DEVELOPER_ID:?Production requires a Developer ID Application signing identity}"
  : "${NOTARY_PROFILE:?Production requires a notarytool Keychain profile}"
  if [[ "$DEVELOPER_ID" != 'Developer ID Application:'* ]]; then echo 'Production requires Developer ID Application signing.' >&2; exit 1; fi
  if ! security find-identity -v -p codesigning | rg -F -- "$DEVELOPER_ID" >/dev/null; then echo 'Developer ID identity is unavailable in Keychain.' >&2; exit 1; fi
  python3 "$project_root/scripts/check-release-gates.py" --version "$version"
fi
build_work="$(mktemp -d "${TMPDIR:-/tmp}/socprint-build.XXXXXX")"
trap 'rm -rf "$build_work"' EXIT
mkdir -p "$output_dir" "$build_work/stage"
app_name='Print @ SoC'
asset_name='Print-at-SoC-Universal'
if [[ "$mode" == development ]]; then app_name='Print @ SoC (Development)'; asset_name='Print-at-SoC-Universal-Development'; fi
app="$build_work/$app_name.app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
cp "$project_root/macos/Info.plist" "$app/Contents/Info.plist"
/usr/libexec/PlistBuddy -c "Set :CFBundleShortVersionString $version" "$app/Contents/Info.plist"
/usr/libexec/PlistBuddy -c 'Set :CFBundleVersion 100' "$app/Contents/Info.plist"
if [[ "$mode" == development ]]; then
  /usr/libexec/PlistBuddy -c 'Set :CFBundleName Print @ SoC (Development)' "$app/Contents/Info.plist"
  /usr/libexec/PlistBuddy -c 'Set :CFBundleDisplayName Print @ SoC (Development)' "$app/Contents/Info.plist"
  /usr/libexec/PlistBuddy -c 'Set :CFBundleIdentifier edu.nus.soc.socprint.development' "$app/Contents/Info.plist"
fi
printf 'APPL????' > "$app/Contents/PkgInfo"
cd "$project_root"
for arch in arm64 amd64; do
  clang_arch=arm64; if [[ "$arch" == amd64 ]]; then clang_arch=x86_64; fi
  GOCACHE="${GOCACHE:-$build_work/go-cache}" CGO_ENABLED=1 GOOS=darwin GOARCH="$arch" CC="clang -arch $clang_arch" \
    "$go_command" build -trimpath -ldflags='-s -w' -o "$build_work/socprint-$arch" ./cmd/socprint-macos
done
lipo -create "$build_work/socprint-arm64" "$build_work/socprint-amd64" -output "$app/Contents/MacOS/socprint"
clang -fobjc-arc -framework Cocoa macos/icon.m -o "$build_work/make-icon"
"$build_work/make-icon" "$build_work/socprint.iconset" "$build_work/icon-preview.png" "$app/Contents/Resources/socprint.icns"
./scripts/dependency-notices.sh "$app/Contents/Resources/Third-Party-Notices.txt"
if [[ "$mode" == production ]]; then
  codesign --force --sign "$DEVELOPER_ID" --options runtime --timestamp "$app"
  codesign --verify --strict --verbose=2 "$app"
  ditto -c -k --keepParent "$app" "$build_work/notarize.zip"
  xcrun notarytool submit "$build_work/notarize.zip" --keychain-profile "$NOTARY_PROFILE" --wait
  xcrun stapler staple "$app"
  xcrun stapler validate "$app"
  spctl --assess --type execute --verbose=2 "$app"
else
  codesign --force --sign - --timestamp=none "$app"
  if [[ "$mode" == unsigned ]]; then
    printf 'Print @ SoC v%s\nAd-hoc signed. Not Developer ID signed or Apple-notarized. macOS may require approval to open.\n' "$version" > "$build_work/stage/Signing-Status.txt"
  else
    printf 'DEVELOPMENT BUILD\nAd-hoc signed. Not notarized.\n' > "$build_work/stage/Development-Build.txt"
  fi
fi
codesign --verify --strict "$app"
python3 "$project_root/scripts/check-macos-artifact.py" "$app"
ditto "$app" "$build_work/stage/$app_name.app"
ln -s /Applications "$build_work/stage/Applications"
cp "$project_root/docs/RELEASE-READINESS.md" "$build_work/stage/Release-Readiness.txt"
dmg="$build_work/$asset_name.dmg"
hdiutil create -volname "$app_name" -srcfolder "$build_work/stage" -ov -format UDZO "$dmg"
if [[ "$mode" == production ]]; then
  codesign --sign "$DEVELOPER_ID" --timestamp "$dmg"
  xcrun notarytool submit "$dmg" --keychain-profile "$NOTARY_PROFILE" --wait
  xcrun stapler staple "$dmg"
  xcrun stapler validate "$dmg"
  spctl --assess --type open --context context:primary-signature --verbose=2 "$dmg"
fi
# Copy only complete, verified artifacts into dist. Existing builds survive failed packaging.
if [[ -d "$output_dir/$app_name.app" ]]; then rm -rf "$output_dir/$app_name.app"; fi
ditto "$app" "$output_dir/$app_name.app"
cp "$dmg" "$output_dir/$asset_name.dmg"
cp "$build_work/icon-preview.png" "$output_dir/icon-preview.png"
(cd "$output_dir" && shasum -a 256 "$asset_name.dmg" > "$asset_name.dmg.sha256")
printf 'Built %s: %s\n' "$mode" "$output_dir/$asset_name.dmg"
