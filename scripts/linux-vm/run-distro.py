#!/usr/bin/env python3
"""Boot a verified full Debian disk in disposable, unprivileged QEMU TCG.

No NIC, host mount, SSH, KVM or host configuration changes. Test inputs are
read-only ISO images. The writable overlay is deleted after QEMU exits.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess


def run(a):
    if os.uname().sysname != 'Linux' or os.geteuid() == 0:
        raise SystemExit('unprivileged Linux host required')
    tools = a.tools.resolve()
    root = tools / 'root'
    image = a.image.resolve()
    receipt = json.loads((image / 'image.json').read_text())
    if receipt['file'] != 'debian-13-generic-amd64.qcow2':
        raise SystemExit('unexpected image filename')
    base = image / receipt['file']
    h = hashlib.sha512()
    with base.open('rb') as f:
        for block in iter(lambda: f.read(1024*1024), b''):
            h.update(block)
    if h.hexdigest() != receipt['sha512']:
        raise SystemExit('base image no longer matches verified receipt')
    if not (tools / 'packages.json').is_file():
        raise SystemExit('missing tool verification receipt')
    work = a.output.resolve()
    work.mkdir(exist_ok=False)
    env = dict(os.environ, LD_LIBRARY_PATH=f'{root}/usr/lib/x86_64-linux-gnu:{root}/lib/x86_64-linux-gnu',
               QEMU_MODULE_DIR=str(root / 'usr/lib/x86_64-linux-gnu/qemu'))
    payload = work / 'payload'
    seed = work / 'seed'
    payload.mkdir(); seed.mkdir()
    shutil.copy2(a.bundle, payload / 'service.tar.gz')
    shutil.copy2(a.tests, payload / 'service-tests')
    (payload / 'SHA256SUMS').write_text(''.join(hashlib.sha256(p.read_bytes()).hexdigest()+'  '+p.name+'\n' for p in sorted(payload.iterdir())))
    (payload / 'run.sh').write_text('''#!/bin/sh
set -eu
[ "$(cat /sys/class/dmi/id/product_name)" = XDial-isolated-distro ]
[ "$(cat /proc/1/comm)" = systemd ]
[ "$(id -u)" = 0 ]
cd /opt/xdial-test
sha256sum -c SHA256SUMS
tar -xzf service.tar.gz
install -m 0755 service-tests /service-tests
if getent passwd 1001 || getent group 1001; then echo 'test UID already occupied'; exit 1; fi
groupadd -g 1001 xdial-test
useradd -u 1001 -g 1001 -M -s /usr/sbin/nologin xdial-test
export XDIAL_ISOLATED_VM=1 XDIAL_DISTRO_VM=1
/service-tests -test.v -test.run='^TestInstalledSystemdLifecycle$' -test.timeout=90s
''')
    (seed / 'meta-data').write_text('instance-id: xdial-disposable-distro\nlocal-hostname: xdial-disposable-distro\n')
    # All commands and root privileges are inside the disposable guest. Never
    # enable a password or SSH key, and never attach a network device.
    (seed / 'user-data').write_text('''#cloud-config
ssh_pwauth: false
disable_root: true
runcmd:
  - |
    systemctl stop serial-getty@hvc0.service
    exec >/dev/hvc0 2>&1
    echo XDIAL_DISTRO_BEGIN
    mkdir -p /mnt/xdial-payload /opt/xdial-test
    if mount -o ro /dev/disk/by-label/XDIALTEST /mnt/xdial-payload && cp /mnt/xdial-payload/* /opt/xdial-test/ && sh /opt/xdial-test/run.sh; then
      echo XDIAL_DISTRO_EXIT=0
    else
      echo XDIAL_DISTRO_EXIT=1
      journalctl -u xdial-service --no-pager -n 80
    fi
    systemctl poweroff
''')
    iso = root / 'usr/bin/genisoimage'
    for directory,label in ((seed,'cidata'),(payload,'XDIALTEST')):
        subprocess.run([str(iso),'-quiet','-output',str(work/(directory.name+'.iso')),'-volid',label,'-joliet','-rock',str(directory)],env=env,check=True)
    overlay = work / 'guest.qcow2'
    subprocess.run([str(root/'usr/bin/qemu-img'),'create','-f','qcow2','-F','qcow2','-b',str(base),str(overlay),'8G'],env=env,check=True)
    command = [str(root/'usr/bin/qemu-system-x86_64'),'-machine','accel=tcg','-cpu','max',
               '-smp','2','-m','2048','-nodefaults','-no-user-config','-display','none',
               '-serial','stdio','-device','virtio-serial-pci',
               '-chardev',f'file,id=testlog,path={work}/test.log',
               '-device','virtconsole,chardev=testlog','-monitor','none','-nic','none','-no-reboot','-device',f'VGA,romfile={root}/usr/share/seabios/vgabios-stdvga.bin',
               '-L',str(root/'usr/share/qemu'),'-bios',str(root/'usr/share/seabios/bios-256k.bin'),
               '-smbios','type=1,manufacturer=XDial,product=XDial-isolated-distro',
               '-drive',f'file={overlay},if=virtio,format=qcow2',
               '-drive',f'file={work / "seed.iso"},media=cdrom,readonly=on,format=raw',
               '-drive',f'file={work / "payload.iso"},media=cdrom,readonly=on,format=raw']
    try:
        with (work/'guest.log').open('wb') as log:
            result = subprocess.run(command,env=env,stdout=log,stderr=subprocess.STDOUT,timeout=480)
    except subprocess.TimeoutExpired:
        raise SystemExit('full guest timed out; QEMU killed and waited; see guest.log')
    finally:
        # No process retains the disk now. Keep logs and image identity, discard
        # guest-generated keys/state and all per-run writable disks.
        overlay.unlink(missing_ok=True)
        for name in ('seed.iso','payload.iso'):
            (work/name).unlink(missing_ok=True)
        shutil.rmtree(payload); shutil.rmtree(seed)
    output=(work/'guest.log').read_bytes()
    (work/'image.json').write_text(json.dumps(receipt,indent=2)+'\n')
    test_output=(work/'test.log').read_bytes()
    print(test_output.decode(errors='replace'))
    if result.returncode or b'XDIAL_DISTRO_EXIT=0' not in test_output or b'XDIAL_DISTRO_SYSTEMD_OK' not in test_output:
        raise SystemExit('full distribution systemd test failed')

if __name__ == '__main__':
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--tools',required=True,type=Path)
    p.add_argument('--image',required=True,type=Path)
    p.add_argument('--bundle',required=True,type=Path)
    p.add_argument('--tests',required=True,type=Path)
    p.add_argument('--output',required=True,type=Path)
    run(p.parse_args())
