#!/usr/bin/env python3
"""Download the official Debian generic image; verify HTTPS-published SHA512.

No host installation or image mounting. The receipt pins the actual bytes even
when the upstream 'latest' alias moves. This is not a claim of PGP verification.
"""
import hashlib
import json
from pathlib import Path
import time
import urllib.request
import urllib.parse

BASE = 'https://cloud.debian.org/images/cloud/trixie/latest/'
NAME = 'debian-13-generic-amd64.qcow2'
ALLOWED = {'cloud.debian.org', 'cdimage.debian.org', 'gemmei.ftp.acc.umu.se'}

def fetch(url):
    r = urllib.request.urlopen(url, timeout=30)
    target = urllib.parse.urlsplit(r.url)
    if target.scheme != 'https' or target.hostname not in ALLOWED:
        r.close()
        raise RuntimeError('unexpected image redirect')
    return r

def main():
    output = Path('debian-image')
    output.mkdir(exist_ok=False)
    with fetch(BASE + 'SHA512SUMS') as r:
        checksums = r.read(1024 * 1024 + 1)
    if len(checksums) > 1024 * 1024:
        raise RuntimeError('checksum manifest exceeds bound')
    (output / 'SHA512SUMS').write_bytes(checksums)
    candidates = [line.split()[0] for line in checksums.decode().splitlines()
                  if len(line.split()) == 2 and line.split()[1].lstrip('*') == NAME]
    if len(candidates) != 1 or len(candidates[0]) != 128:
        raise RuntimeError('exact Debian image absent from checksum manifest')
    expected = candidates[0]
    h = hashlib.sha512()
    size = 0
    deadline = time.monotonic() + 450
    temporary = output / 'image.partial'
    with fetch(BASE + NAME) as r, temporary.open('xb') as f:
        actual_url = r.url
        while True:
            chunk = r.read(1024 * 1024)
            if not chunk:
                break
            size += len(chunk)
            if size > 2 * 1024 ** 3 or time.monotonic() > deadline:
                raise RuntimeError('image download exceeds bound')
            h.update(chunk)
            f.write(chunk)
    if h.hexdigest() != expected:
        raise RuntimeError('Debian image checksum mismatch; keep partial for diagnosis')
    temporary.rename(output / NAME)
    receipt = {'url': BASE + NAME, 'resolvedURL': actual_url, 'sha512': expected,
               'bytes': size, 'verification': 'official HTTPS manifest SHA512', 'file': NAME}
    (output / 'image.json').write_text(json.dumps(receipt, indent=2) + '\n')
    print(json.dumps(receipt))

if __name__ == '__main__':
    main()
