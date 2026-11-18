#!/bin/sh
set -eu

java=$(readlink -f /usr/bin/java)
java_home=$(dirname "$(dirname "$java")")
# go-judge mounts /usr read-only. Keep OpenJDK's packaged configuration in
# that tree so the nested sandbox can read it without mounting extra /etc paths.
find "$java_home" -type l -exec sh -ec '
  for link do
    target=$(readlink -f "$link") || continue
    case "$target" in
      /etc/java-*-openjdk|/etc/java-*-openjdk/*|/etc/ssl/certs/java/cacerts)
        cp -aL -- "$target" "$link.oj-copy"
        rm -- "$link"
        mv -T -- "$link.oj-copy" "$link"
        ;;
    esac
  done
' sh {} +
test -r "$java_home/conf/security/java.security"
test ! -L "$java_home/conf/security/java.security"
