#!/usr/bin/env bash
set -euo pipefail
project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
test_work="$(mktemp -d "${TMPDIR:-/tmp}/socprint-native-tests.XXXXXX")"
trap 'rm -rf "$test_work"' EXIT
cd "$project_root"
clang -fobjc-arc -fblocks -mmacosx-version-min=13.0 -framework Cocoa -framework WebKit -framework PDFKit \
  tests/pdf-native.m cmd/socprint-macos/pdfengine.m -o "$test_work/pdf-tests"
"$test_work/pdf-tests"
node --test tests/ui.test.mjs
