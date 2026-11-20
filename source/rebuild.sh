#!/bin/sh
# Run in a fresh directory containing the source downloads listed below.
set -eu
kit=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
key=${1:?Pass the platform and build number shown on the source page}
version=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["builds"][sys.argv[2]]["version"])' "$kit/inputs.json" "$key")
configuration=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["builds"][sys.argv[2]].get("configuration","Release"))' "$kit/inputs.json" "$key")
number=${key#*-}
case "$key" in
  ios-*) scheme=XDialIOS; folder=ios; platform=iOS ;;
  tv-*) scheme=XDialTV; folder=appletv; platform=tvOS ;;
  mac-*) scheme=XDialStore; folder=macos; platform=macOS ;;
  *) echo "Unknown platform" >&2; exit 2 ;;
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
    if [ "$configuration" = AppStore ]; then
        cp "$kit/$key-notices.txt" ios/XDialIOS/ThirdPartyNotices.txt
        xcodegen generate --spec ios/project.yml
    else
        python3 "$kit/prepare-ios.py" "$PWD" "$kit/$key-notices.txt" "$PWD/build/store-plists"
    fi
elif [ "$platform" = tvOS ]; then
    make public-content-gate libbox-xcframework
    mkdir -p appletv/StoreResources
    cp "$kit/$key-notices.txt" appletv/StoreResources/ThirdPartyNotices.txt
    xcodegen generate --spec appletv/project.yml
else
    make public-content-gate libbox-macos-xcframework macos/AppIcon.icns
    cp "$kit/$1-notices.txt" macos/Store/Host/ThirdPartyNotices.txt
    python3 - "$configuration" <<'PY'
import sys
from pathlib import Path
spec = Path("macos/store.yml").read_text()
resource = "      - Store/Host/PrivacyInfo.xcprivacy"
flag = "        ITSAppUsesNonExemptEncryption: false\n"
assert spec.count(resource) == 1
if sys.argv[1] == "Release":
    assert spec.count(flag) == 1
    spec = spec.replace(flag, "")
Path("macos/store-source.yml").write_text(spec.replace(resource, resource+"\n      - Store/Host/ThirdPartyNotices.txt"))
PY
    xcodegen generate --spec macos/store-source.yml
fi
xcodebuild -project "$folder/$scheme.xcodeproj" -scheme "$scheme" -configuration "$configuration" \
  -destination "generic/platform=$platform" -archivePath "$workspace/XDial.xcarchive" \
  "MARKETING_VERSION=$version" "CURRENT_PROJECT_VERSION=$number" \
  "XDIAL_SOURCE_REVISION=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["builds"][sys.argv[2]]["commit"])' "$kit/inputs.json" "$1")" \
  "XDIAL_PRIVATE_CONFIG_DIR=${XDIAL_PRIVATE_CONFIG_DIR:-}" "XDIAL_IOS_PRIVATE_CONFIG_DIR=${XDIAL_PRIVATE_CONFIG_DIR:-}" \
  CODE_SIGNING_ALLOWED=NO archive
