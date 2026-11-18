#!/bin/sh
set -eu

mirror=${DEBIAN_MIRROR_BASE:-}
if [ -n "$mirror" ]; then
  mirror=${mirror%/}
  if ! printf '%s\n' "$mirror" | grep -Eq '^https?://[[:alnum:].:/_-]+$'; then
    printf 'DEBIAN_MIRROR_BASE must be an HTTP or HTTPS mirror base URL.\n' >&2
    exit 1
  fi
  for source in /etc/apt/sources.list /etc/apt/sources.list.d/*.list /etc/apt/sources.list.d/*.sources; do
    [ -f "$source" ] || continue
    sed -Ei "s#https?://(deb\.|security\.)debian\.org/#$mirror/#g" "$source"
  done
fi

apt-get update -o APT::Update::Error-Mode=any
apt-get install -y --no-install-recommends "$@"
rm -rf /var/lib/apt/lists/*
