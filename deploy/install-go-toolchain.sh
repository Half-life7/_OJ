#!/bin/sh
set -eu

# Go 1.23.12 release checksums from https://go.dev/dl/.
version=1.23.12
arch=$(dpkg --print-architecture)
case "$arch" in
  amd64) checksum=d3847fef834e9db11bf64e3fb34db9c04db14e068eeb064f49af747010454f90 ;;
  arm64) checksum=52ce172f96e21da53b1ae9079808560d49b02ac86cecfa457217597f9bc28ab3 ;;
  armhf|armel)
    arch=armv6l
    checksum=9704eba01401a3793f54fac162164b9c5d8cc6f3cab5cee72684bb72294d9f41
    ;;
  i386)
    arch=386
    checksum=3b2fd446e26642555d1446a38ccbefb2a30bba3179d3ef132ed64d3c63b0c42a
    ;;
  ppc64el)
    arch=ppc64le
    checksum=1a7cc5f7baeaf39125dce5d660a39438e7f0e04d13d3498590d240aae976b565
    ;;
  s390x) checksum=2f43708aa0922d692da0a1fc775475c343907610bec77002de1bbe37601ea338 ;;
  riscv64) checksum=5798eda8c167dd620feb54e1bcca1b4cc014a529821d8c01f31d7e17a43cb8ed ;;
  *) printf 'Unsupported Go toolchain architecture: %s\n' "$arch" >&2; exit 1 ;;
esac

base_url=${GO_TOOLCHAIN_URL:-https://go.dev/dl}
url="${base_url%/}/go${version}.linux-${arch}.tar.gz"
archive=$(mktemp)
trap 'rm -f -- "$archive"' EXIT
printf 'Downloading Go toolchain from %s\n' "$url"
if ! curl --fail --show-error --location --retry 2 --retry-max-time 720 \
    --connect-timeout 10 --max-time 600 --speed-limit 16384 --speed-time 60 \
    --output "$archive" "$url"; then
  printf 'Go toolchain download failed. Check GO_TOOLCHAIN_URL or use an offline image archive.\n' >&2
  exit 1
fi
printf '%s  %s\n' "$checksum" "$archive" | sha256sum -c -
tar -xzf "$archive" -C /usr/local
