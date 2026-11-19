"""Prepare release resources from a clean XDial c1a3c700 source. Use a private-free checkout."""
from pathlib import Path
import argparse,plistlib,subprocess
ap=argparse.ArgumentParser()
ap.add_argument('source',type=Path);ap.add_argument('notices',type=Path);ap.add_argument('generated_plists',type=Path)
args=ap.parse_args()
src=args.source.resolve()
if (src/'ios/PrivateConfig').exists(): raise SystemExit('Use an empty public checkout without enterprise resources.')
(src/'ios/XDialIOS/ThirdPartyNotices.txt').write_bytes(args.notices.read_bytes())
subprocess.run(['xcodegen','generate','--spec','ios/project.yml'],cwd=src,check=True)
project=src/'ios/XDialIOS.xcodeproj/project.pbxproj';text=project.read_text()
assert 'ThirdPartyNotices.txt in Resources' in text
key='INFOPLIST_KEY_ITSAppUsesNonExemptEncryption = NO;'
setting='INFOPLIST_FILE = XDialIOSTunnel/Info.plist;'
assert text.count(key)==2 and text.count(setting)==2
document=plistlib.loads((src/'ios/XDialIOSTunnel/Info.plist').read_bytes())
assert document.pop('ITSAppUsesNonExemptEncryption') is False
args.generated_plists.mkdir(exist_ok=True,parents=True)
target=args.generated_plists.resolve()/'Tunnel-Store-Info.plist';target.write_bytes(plistlib.dumps(document))
project.write_text(text.replace(key,'').replace(setting,'INFOPLIST_FILE = "'+str(target)+'";'))
