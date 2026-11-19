#!/usr/bin/env python3
"""Check the signed public XD tvOS app from an xcarchive or an exported IPA."""
import argparse
import plistlib
import subprocess
from pathlib import Path

BUNDLE = "com.xd.xdial.ios"
TEAM = "SX9RG894L5"
GROUP = "group." + BUNDLE
KEYCHAIN = TEAM + ".com.xd.xdial.shared"


def require(ok, message):
    if not ok:
        raise SystemExit(message)


def info(path):
    return plistlib.loads((path / "Info.plist").read_bytes())


def verify_target(path, bundle_id, markers):
    metadata = info(path)
    require(metadata["CFBundleIdentifier"] == bundle_id, f"{path.name}: unexpected bundle ID")
    require(metadata["CFBundleSupportedPlatforms"] == ["AppleTVOS"], f"{path.name}: device build required")
    require(metadata.get("XDialTunnelBundleIdentifier") == BUNDLE + ".tunnel" and
            metadata.get("XDialAppGroupIdentifier") == GROUP and
            metadata.get("XDialSharedKeychainAccessGroup") == KEYCHAIN,
            f"{path.name}: incorrect runtime identity")
    require((path / "PrivacyInfo.xcprivacy").is_file(), f"{path.name}: privacy manifest missing")
    subprocess.run(["codesign", "--verify", "--strict", str(path)], check=True)
    signed = plistlib.loads(subprocess.check_output(["codesign", "-d", "--entitlements", "-", "--xml", str(path)]))
    require(signed.get("application-identifier") == TEAM + "." + bundle_id and
            signed.get("com.apple.developer.team-identifier") == TEAM and not signed.get("get-task-allow"),
            f"{path.name}: incorrect signing identity or development signing")
    for key, value in [("com.apple.security.application-groups", GROUP),
                       ("keychain-access-groups", KEYCHAIN),
                       ("com.apple.developer.networking.networkextension", "packet-tunnel-provider")]:
        require(value in signed.get(key, []), f"{path.name}: missing signed entitlement {key}")
    binary = (path / metadata["CFBundleExecutable"]).read_bytes()
    for marker in markers:
        require(marker.encode() in binary, f"{path.name}: missing production engine symbol {marker}")
    for marker in ("NoopTunnelEngine", "FakeTunnelEngine", "LANShareSettings", "AddLANShareInbound", "core/macgateway."):
        require(marker.encode() not in binary, f"{path.name}: unexpected engine/experiment symbol {marker}")
    return metadata


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("app", type=Path)
    app = parser.parse_args().app
    host = verify_target(app, BUNDLE, ["GoEngine", "TunnelManager", "NETunnelProviderManager"])
    extensions = list((app / "PlugIns").glob("*.appex"))
    require(len(extensions) == 1, "Expected one embedded packet tunnel")
    provider = verify_target(extensions[0], BUNDLE + ".tunnel",
                             ["PacketTunnelProvider", "NEPacketTunnelProvider", "LibboxNew",
                              "libbox.(*Libbox).Start"])
    require(provider.get("NSExtension", {}).get("NSExtensionPointIdentifier") ==
            "com.apple.networkextension.packet-tunnel", "Incorrect extension point")
    for key in ("CFBundleVersion", "CFBundleShortVersionString"):
        require(host[key] == provider[key], "Host and provider version mismatch")
    for path in app.rglob("*"):
        require(path.name.lower() not in {"privateconfig", "seed.json", "rulesets"} and
                path.suffix.lower() not in {".srs", ".p8", ".p12", ".pem", ".key"},
                f"Private release input found: {path.relative_to(app)}")
    require((app / "Assets.car").is_file(), "Store artwork missing")
    require((app / "ThirdPartyNotices.txt").is_file(), "Third-party notices missing")
    print(f"Verified tvOS {host['CFBundleShortVersionString']} ({host['CFBundleVersion']}), "
          f"{host['CFBundleIdentifier']} and embedded packet tunnel")


if __name__ == "__main__":
    main()
