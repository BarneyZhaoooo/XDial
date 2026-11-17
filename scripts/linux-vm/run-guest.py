#!/usr/bin/env python3
"""Run the Linux kernel integration tests without changing the host network.

Requires an already verified/extracted tools directory from prepare-tools.sh.
QEMU uses TCG, no NIC, no disk, no host mount, and a bounded foreground lifetime.
All administrative commands in the tests run inside the disposable initramfs.
"""
import argparse
import os
from pathlib import Path
import re
import shutil
import subprocess


def run(args):
    if os.uname().sysname != "Linux" or os.geteuid() == 0:
        raise SystemExit("run QEMU as an unprivileged Linux host user")
    tools = args.tools.resolve()
    root = tools / "root"
    if not (tools / "packages.json").is_file():
        raise SystemExit("missing verified package manifest")
    work = args.output.resolve()
    work.mkdir(exist_ok=False)
    guest = work / "guest"
    for d in ("bin", "sbin", "proc", "sys", "dev", "tmp", "run", "etc", "lib/modules"):
        (guest / d).mkdir(parents=True, exist_ok=True)
        (guest / d).chmod(0o755)
    guest.chmod(0o755)
    env = dict(os.environ, LD_LIBRARY_PATH=f"{root}/usr/lib/x86_64-linux-gnu:{root}/lib/x86_64-linux-gnu",
               QEMU_MODULE_DIR=str(root / "usr/lib/x86_64-linux-gnu/qemu"))
    for name in ("busybox", "ip", "nft", "kmod"):
        source = next(p for p in (root / d / name for d in ("usr/bin", "usr/sbin", "bin", "sbin")) if p.is_file())
        shutil.copy2(source, guest / "bin" / name)
        result = subprocess.run(["ldd", str(source)], capture_output=True, text=True, env=env)
        if result.returncode and "not a dynamic executable" not in result.stdout + result.stderr:
            raise SystemExit(result.stderr)
        for library in re.findall(r"(/[\w./+:-]+)", result.stdout):
            path = Path(library)
            if not path.is_file():
                continue
            relative = path.relative_to(root) if path.is_relative_to(root) else path.relative_to("/")
            target = guest / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(path, target)
    for name in ("sh", "mount", "poweroff", "grep", "uname", "sleep"):
        (guest / "bin" / name).symlink_to("busybox")
    (guest / "sbin/modprobe").symlink_to("../bin/kmod")
    modules = next((root / "lib/modules").iterdir())
    shutil.copytree(modules, guest / "lib/modules" / modules.name)
    # Explicitly decompress within the guest tree. kmod's finit_module path and
    # kernel compression support differ between distros; don't pass zstd as ELF.
    for module in (guest / "lib/modules").rglob("*.ko.zst"):
        subprocess.run(["zstd", "-d", "--rm", "--quiet", str(module)], check=True)
    subprocess.run(["depmod", "-b", str(guest), modules.name], check=True)
    shutil.copy2(args.binary, guest / "app-core")
    (guest / "app-core").chmod(0o755)
    for app in ("app-block", "app-allow"):
        os.link(guest / "app-core", guest / app)
    if args.capture_binary:
        shutil.copy2(args.capture_binary, guest / "app-capture")
        (guest / "app-capture").chmod(0o755)
    if bool(args.service_binary) != bool(args.service_tests):
        raise SystemExit("service binary and tests must be supplied together")
    if args.service_binary:
        # This minimal initramfs has no native NSS/cache service. Declare its
        # wire-DNS fixture explicitly; do not relax the production admission.
        (guest / "etc/nsswitch.conf").write_text("hosts: files dns\n")
        shutil.copy2(args.service_binary, guest / "app-service")
        shutil.copy2(args.service_tests, guest / "service-tests")
        (guest / "app-service").chmod(0o755)
        (guest / "service-tests").chmod(0o755)
        for name in ("service-proxy", "service-allow"):
            os.link(guest / "service-tests", guest / name)
    init = '''#!/bin/sh
set -eu
export PATH=/bin:/sbin
[ "$$" = 1 ] || exit 90
mount -t proc proc /proc
mount -t sysfs sysfs /sys
mount -t devtmpfs devtmpfs /dev
/bin/busybox ln -s /proc/self/fd/0 /dev/stdin
grep -q 'xdial_isolated_vm=transparent' /proc/cmdline || exit 91
ip link set lo up
modprobe nf_tables
modprobe nft_tproxy
modprobe tcp_diag
modprobe udp_diag
export XDIAL_ISOLATED_VM=1
echo XDIAL_GUEST_BEGIN
uname -r
set +e
if [ -x /app-capture ]; then
  /app-capture -test.v -test.timeout=60s
  result=$?
  if [ "$result" != 0 ]; then echo XDIAL_CAPTURE_TEST_EXIT=$result; poweroff -f; exit "$result"; fi
fi
if [ -x /app-service ]; then
  /service-tests -test.v -test.timeout=60s
  result=$?
  if [ "$result" != 0 ]; then echo XDIAL_SERVICE_TEST_EXIT=$result; poweroff -f; exit "$result"; fi
fi
/app-core -test.v -test.run='TestLinux(TProxyProtectionContract|TransparentKernelTraffic)$' -test.timeout=90s
result=$?
echo XDIAL_GUEST_TEST_EXIT=$result
if [ "$result" = 0 ]; then echo XDIAL_TRANSPARENT_GUEST_OK; fi
poweroff -f
'''
    (guest / "init").write_text(init)
    (guest / "init").chmod(0o755)
    # No shell interpolation of paths. Both archivers are bounded foreground
    # children, and failure from either side is propagated.
    names = b"\0".join(str(p.relative_to(guest)).encode() for p in [guest] + list(guest.rglob("*"))) + b"\0"
    with (work / "cpio.log").open("wb") as error_log, (work / "guest.cpio.gz").open("wb") as archive:
        cpio = subprocess.Popen(["cpio", "--null", "-o", "--format=newc", "--owner=0:0"], cwd=guest,
                                stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=error_log)
        zipper = subprocess.Popen(["gzip", "-1"], stdin=cpio.stdout, stdout=archive)
        cpio.stdout.close()
        cpio.stdout = None
        cpio.communicate(names)
        if cpio.returncode or zipper.wait():
            raise SystemExit("initramfs archive failed")
    command = [str(root / "usr/bin/qemu-system-x86_64"), "-machine", "accel=tcg", "-cpu", "max",
               "-smp", "2", "-m", "1536", "-nodefaults", "-no-user-config", "-display", "none",
               "-serial", "stdio", "-monitor", "none", "-nic", "none", "-no-reboot",
               "-L", str(root / "usr/share/qemu"), "-bios", str(root / "usr/share/seabios/bios-256k.bin"),
               "-kernel", str(next((root / "boot").glob("vmlinuz-*"))),
               "-initrd", str(work / "guest.cpio.gz"),
               "-append", "console=ttyS0 quiet panic=-1 xdial_isolated_vm=transparent"]
    try:
        result = subprocess.run(command, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, env=env, timeout=150)
    except subprocess.TimeoutExpired as error:
        (work / "guest.log").write_bytes(error.stdout or b"")
        raise SystemExit("guest timed out; owned QEMU process was killed and waited")
    finally:
        # QEMU has exited (or subprocess.run killed and waited it). Remove only
        # our generated tree now; per-file SFTP cleanup of kernel modules is
        # needlessly slow. Keep logs, source receipts, and downloaded tool inputs.
        shutil.rmtree(guest)
        (work / "guest.cpio.gz").unlink()
    (work / "guest.log").write_bytes(result.stdout)
    print(result.stdout.decode(errors="replace"))
    if result.returncode or b"XDIAL_TRANSPARENT_GUEST_OK" not in result.stdout:
        raise SystemExit("guest integration test failed")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--tools", required=True, type=Path)
    parser.add_argument("--binary", required=True, type=Path)
    parser.add_argument("--capture-binary", type=Path)
    parser.add_argument("--service-binary", type=Path)
    parser.add_argument("--service-tests", type=Path)
    parser.add_argument("--output", required=True, type=Path)
    run(parser.parse_args())
