#!/bin/sh
# Embeds the company preset into a Mac app (Developer ID and App Store builds).
# Sources, first match wins:
#   XDIAL_SEED_PROFILE_FILE             an explicit .json file (name = Profile name)
#   XDIAL_PRIVATE_CONFIG_DIR/Seed.json  the shared preset all platforms build from
#   (XDIAL_IOS_PRIVATE_CONFIG_DIR is the older name of the same directory)
#   macos/PrivateConfig/SeedProfile/*.json   local, git-ignored
# A shared Seed.json is embedded as "<profile_name>.json": the file name is the
# Profile name, which must stay stable so later builds update the same Profile.
set -eu
resource_dir="$1"
srcroot="$2"
seed_dir="${resource_dir}/SeedProfile"
rm -f "${seed_dir}"/*.json 2>/dev/null || true
seed_source="${XDIAL_SEED_PROFILE_FILE:-}"
shared_dir="${XDIAL_PRIVATE_CONFIG_DIR:-${XDIAL_IOS_PRIVATE_CONFIG_DIR:-}}"
if [ -n "${shared_dir}" ] && [ ! -d "${shared_dir}" ]; then
  echo "error: shared private configuration directory not found: ${shared_dir}" >&2
  exit 1
fi
[ -n "${shared_dir}" ] || shared_dir="${srcroot}/../ios/PrivateConfig"
name=""
if [ -z "${seed_source}" ] && [ -f "${shared_dir}/Seed.json" ]; then
  seed_source="${shared_dir}/Seed.json"
  name="$(/usr/bin/plutil -extract profile_name raw -o - "${seed_source}" 2>/dev/null || true)"
  [ -n "${name}" ] || name="Seed"
  case "${name}" in
    */*|.*) echo "error: invalid profile_name in ${seed_source}" >&2; exit 1 ;;
  esac
fi
if [ -z "${seed_source}" ]; then
  for candidate in "${srcroot}"/PrivateConfig/SeedProfile/*.json; do
    if [ -f "${candidate}" ]; then seed_source="${candidate}"; break; fi
  done
fi
if [ -z "${seed_source}" ]; then
  # An earlier build may have embedded a preset; an empty SeedProfile/ reads as an invalid one.
  rmdir "${seed_dir}" 2>/dev/null || true
  exit 0
fi
case "${seed_source}" in
  *.json) ;;
  *) echo "error: seed Profile must be a .json file: ${seed_source}" >&2; exit 1 ;;
esac
if [ ! -f "${seed_source}" ]; then
  echo "error: private build configuration not found: ${seed_source}" >&2
  exit 1
fi
[ -n "${name}" ] || name="$(basename "${seed_source}" .json)"
mkdir -p "${seed_dir}"
cp "${seed_source}" "${seed_dir}/${name}.json"
