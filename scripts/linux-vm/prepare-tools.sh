#!/bin/sh
# Download and extract trusted Ubuntu packages inside a fresh task directory.
# No installation, postinst, module load, elevation, or host networking changes.
set -eu
[ "$(uname -s)" = Linux ]
[ "$(id -u)" != 0 ]
[ ! -e downloads ] && [ ! -e root ]
mkdir downloads root
python3 - <<'PY'
import subprocess,re,pathlib,json,os,hashlib
root=pathlib.Path.cwd()
env=dict(os.environ,LC_ALL='C')
plan=subprocess.check_output(['apt-get','-s','-o','Debug::NoLocking=true','--no-install-recommends','install','qemu-system-x86'],text=True,env=env)
(root/'apt-simulation.txt').write_text(plan)
packages={m.group(1):m.group(2) for m in re.finditer(r'^Inst (\S+)(?: \[[^\]]+\])? \((\S+)',plan,re.M)}
for name in ['qemu-system-x86','qemu-system-common','qemu-system-data','seabios','busybox-static','nftables','iproute2','kmod','linux-image-6.8.0-149-generic','linux-modules-6.8.0-149-generic']:
 policy=subprocess.check_output(['apt-cache','policy',name],text=True,env=env)
 m=re.search(r'Candidate: (\S+)',policy)
 if not m or m.group(1)=='(none)':raise SystemExit('missing candidate '+name)
 packages[name]=m.group(1)
manifest=[]
for name,version in packages.items():
 spec=name+'='+version
 metadata=subprocess.check_output(['apt-cache','show',spec],text=True,env=env)
 allowed=set(re.findall(r'^SHA256: ([a-f0-9]{64})$',metadata,re.M))
 if not allowed:raise SystemExit('unsigned package metadata missing hash '+name)
 subprocess.run(['apt-get','-o','APT::Get::AllowUnauthenticated=false','download',spec],cwd=root/'downloads',env=env,check=True)
 matches=[]
 for file in (root/'downloads').glob('*.deb'):
  identity=subprocess.check_output(['dpkg-deb','-f',str(file),'Package','Version'],text=True)
  if ('Package: '+name+'\n') in identity and ('Version: '+version+'\n') in identity:matches.append(file)
 if len(matches)!=1:raise SystemExit('ambiguous downloaded package '+name)
 file=matches[0];digest=hashlib.sha256(file.read_bytes()).hexdigest()
 if digest not in allowed:raise SystemExit('package checksum mismatch '+name)
 subprocess.run(['dpkg-deb','--extract',str(file),str(root/'root')],check=True)
 manifest.append({'package':name,'version':version,'sha256':digest,'file':file.name})
(root/'packages.json').write_text(json.dumps(manifest,indent=2))
print('verified_packages',len(manifest))
PY
export LD_LIBRARY_PATH="$PWD/root/usr/lib/x86_64-linux-gnu:$PWD/root/lib/x86_64-linux-gnu"
./root/usr/bin/qemu-system-x86_64 --version
find root -name '*tproxy*' -o -name '*TPROXY*' -o -name 'vmlinuz*'
