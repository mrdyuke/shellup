#!/usr/bin/env bash
# build.sh builds release binaries for Linux amd64 and arm64 into dist/.
set -euo pipefail

rm -rf dist
mkdir -p dist

build() {
    goos=$1
    goarch=$2
    out="dist/shellup-linux-${goarch}"
    echo "Building for ${goos}/${goarch}..."
    CGO_ENABLED=0 GOOS="${goos}" GOARCH="${goarch}" go build -o "${out}" ./cmd/shellup
    echo "Success: ${out}"
}

build linux amd64
build linux arm64

echo "All builds finished. Artifacts:"
ls -lh dist
