#!/usr/bin/env bash
set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
output_dir="${OUTPUT_DIR:-${project_root}/dist}"
version="${VERSION:-dev}"
go_command="${GO:-$(command -v go || true)}"
if [[ -z "${go_command}" && -x /usr/local/go/bin/go ]]; then
	go_command="/usr/local/go/bin/go"
fi
if [[ -z "${go_command}" ]]; then
	echo "Go was not found. Install Go or set GO to the go executable path." >&2
	exit 1
fi
mkdir -p "${output_dir}"
rm -f "${output_dir}"/print-at-soc-*.tar.gz "${output_dir}"/print-at-soc-*.zip "${output_dir}"/socprint-*.tar.gz "${output_dir}"/socprint-*.zip "${output_dir}/SHA256SUMS"

build_one() {
	local goos="$1" goarch="$2"
	local executable="socprint"
	local archive
	local stage="${output_dir}/stage-${goos}-${goarch}"
	if [[ "${goos}" == "windows" ]]; then executable="socprint.exe"; fi
	archive="${output_dir}/print-at-soc-${version}-${goos}-${goarch}"
	rm -rf "${stage}"
	mkdir -p "${stage}"
	(
		cd "${project_root}"
		CGO_ENABLED=0 GOOS="${goos}" GOARCH="${goarch}" "${go_command}" build -trimpath -ldflags "-s -w -X main.version=${version}" -o "${stage}/${executable}" ./cmd/socprint
	)
	if [[ "${goos}" == "windows" ]]; then
		(cd "${stage}" && zip -q -j "${archive}.zip" "${executable}")
	else
		(cd "${stage}" && tar -czf "${archive}.tar.gz" "${executable}")
	fi
	rm -rf "${stage}"
}

build_one darwin amd64
build_one darwin arm64
build_one linux amd64
build_one linux arm64
build_one windows amd64

(
	cd "${output_dir}"
	if command -v shasum >/dev/null; then
		shasum -a 256 print-at-soc-*.tar.gz print-at-soc-*.zip > SHA256SUMS
	else
		sha256sum print-at-soc-*.tar.gz print-at-soc-*.zip > SHA256SUMS
	fi
)
