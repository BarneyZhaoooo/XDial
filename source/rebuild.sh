#!/bin/sh
# Run in a fresh directory containing the source downloads listed below.
set -eu
kit=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
case "${1:-}" in
  ios-62|ios-66|ios-71) version=1.0; number=${1#ios-}; scheme=XDialIOS; folder=ios; platform=iOS ;;
  mac-72) version=0.1.0; number=72; scheme=XDialStore; folder=macos; platform=macOS ;;
  *) echo "Usage: sh rebuild.sh ios-62 | ios-66 | ios-71 | mac-72" >&2; exit 2 ;;
esac
workspace=$(mktemp -d "${TMPDIR:-/tmp}/xdial-build.XXXXXXXX")
workspace=$(CDPATH='' cd -P -- "$workspace" && pwd -P)
echo "Build workspace: $workspace"
cd "$workspace"
srcfile=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["builds"][sys.argv[2]]["source"])' "$kit/inputs.json" "$1")
mkdir XDial
tar xzf "$kit/$srcfile" --strip-components 1 -C XDial
for archive in "$kit"/go-inputs-*.zip; do unzip -qn "$archive"; done
export GOPROXY="file://$workspace/go-proxy,https://proxy.golang.org"
cd XDial
git init -q
git add -f -- .
if [ "$platform" = iOS ]; then
    make public-content-gate libbox-ios-xcframework
    python3 "$kit/prepare-ios.py" "$PWD" "$kit/$1-notices.txt" "$PWD/build/store-plists"
else
    make public-content-gate libbox-macos-xcframework macos/AppIcon.icns
    cp "$kit/$1-notices.txt" macos/Store/Host/ThirdPartyNotices.txt
    python3 - <<'PY'
from pathlib import Path
spec = Path("macos/store.yml").read_text()
resource = "      - Store/Host/PrivacyInfo.xcprivacy"
flag = "        ITSAppUsesNonExemptEncryption: false\n"
assert spec.count(resource) == 1 and spec.count(flag) == 1
Path("macos/store-source.yml").write_text(spec.replace(resource, resource+"\n      - Store/Host/ThirdPartyNotices.txt").replace(flag,""))
PY
    xcodegen generate --spec macos/store-source.yml
fi
xcodebuild -project "$folder/$scheme.xcodeproj" -scheme "$scheme" -configuration Release \
  -destination "generic/platform=$platform" -archivePath "$workspace/XDial.xcarchive" \
  "MARKETING_VERSION=$version" "CURRENT_PROJECT_VERSION=$number" \
  "XDIAL_SOURCE_REVISION=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["builds"][sys.argv[2]]["commit"])' "$kit/inputs.json" "$1")" \
  "XDIAL_PRIVATE_CONFIG_DIR=${XDIAL_PRIVATE_CONFIG_DIR:-}" "XDIAL_IOS_PRIVATE_CONFIG_DIR=${XDIAL_PRIVATE_CONFIG_DIR:-}" \
  CODE_SIGNING_ALLOWED=NO archive
