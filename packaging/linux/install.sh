#!/bin/sh
# Explicit first-install/removal transaction. Never run as part of a build.
set -eu
[ "$(id -u)" = 0 ] || { echo 'Run this reviewed installation as root.' >&2; exit 1; }
cd "$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
# Serialize installation/removal independently of a connection's state lock.
command -v flock >/dev/null
[ -d /run ] && [ ! -L /run ] && [ "$(stat -c %u /run)" = 0 ] && [ "$((0$(stat -c %a /run) & 022))" = 0 ] || exit 1
lock=/run/xdial-service.install.lock
if [ -e "$lock" ] || [ -L "$lock" ]; then
 [ -f "$lock" ] && [ ! -L "$lock" ] && [ "$(stat -c %u "$lock")" = 0 ] && [ "$(stat -c %h "$lock")" = 1 ] && [ "$((0$(stat -c %a "$lock") & 077))" = 0 ] || exit 1
fi
umask 077
exec 9>"$lock"
flock -n 9 || { echo 'Another installation transaction is active.' >&2; exit 1; }
case "${1:-}" in
install)
 [ "$#" = 7 ] || { echo 'install UID CAPTURE_BIT BYPASS_BIT TABLE PRIORITY IPV4_LOOPBACK:PORT' >&2; exit 1; }
 sha256sum -c SHA256SUMS
 # Validation is performed by the verified binary before any system installation.
 ./xdial-service validate-policy "$2" "$3" "$4" "$5" "$6" "$7" > /dev/null
 for path in /usr/local/libexec/xdial-service /etc/systemd/system/xdial-service.service /var/lib/xdial-service; do
  [ ! -e "$path" ] && [ ! -L "$path" ] || { echo "Existing installation requires an explicit upgrade: $path" >&2; exit 1; }
 done
 command -v systemctl >/dev/null
 [ -d /run/systemd/system ] || { echo 'This package requires a running systemd system.' >&2; exit 1; }
 # Do not install a root service below a user-writable or symlinked prefix.
 for path in /usr /usr/local /etc /etc/systemd /etc/systemd/system /var /var/lib; do
  [ -d "$path" ] && [ ! -L "$path" ] && [ "$(stat -c %u "$path")" = 0 ] && [ "$((0$(stat -c %a "$path") & 022))" = 0 ] || { echo "Unsafe installation parent: $path" >&2; exit 1; }
 done
 if [ -e /usr/local/libexec ]; then
  path=/usr/local/libexec
  [ -d "$path" ] && [ ! -L "$path" ] && [ "$(stat -c %u "$path")" = 0 ] && [ "$((0$(stat -c %a "$path") & 022))" = 0 ] || exit 1
 fi
 umask 077
 install -d -m 0755 /usr/local/libexec
 install -d -m 0700 /var/lib/xdial-service
 trap 'rm -f /usr/local/libexec/xdial-service /etc/systemd/system/xdial-service.service /var/lib/xdial-service/policy.json /var/lib/xdial-service/package.sha256; rmdir /var/lib/xdial-service/capture /var/lib/xdial-service/cache /var/lib/xdial-service 2>/dev/null || true' EXIT HUP INT TERM
 install -d -m 0700 /var/lib/xdial-service/capture /var/lib/xdial-service/cache
 ./xdial-service validate-policy "$2" "$3" "$4" "$5" "$6" "$7" > /var/lib/xdial-service/policy.json
 install -m 0755 xdial-service /usr/local/libexec/xdial-service
 install -m 0644 xdial-service.service /etc/systemd/system/xdial-service.service
 sha256sum /usr/local/libexec/xdial-service /etc/systemd/system/xdial-service.service > /var/lib/xdial-service/package.sha256
 systemctl daemon-reload
 trap - EXIT HUP INT TERM
 echo 'Installed, not started. Review policy.json, then systemctl enable --now xdial-service.'
 ;;
remove)
 [ "$#" = 1 ] || exit 1
 # Refuse a changed binary/unit; never remove an unrelated replacement.
 sha256sum -c /var/lib/xdial-service/package.sha256
 systemctl disable --now xdial-service
 /usr/local/libexec/xdial-service recover --state /var/lib/xdial-service
 rm /usr/local/libexec/xdial-service /etc/systemd/system/xdial-service.service
 systemctl daemon-reload
 echo 'Service removed. Root-owned policy/cache/journal directory retained for review.'
 ;;
*) echo 'Use install UID CAPTURE_BIT BYPASS_BIT TABLE PRIORITY LISTENER, or remove.' >&2; exit 1;;
esac
