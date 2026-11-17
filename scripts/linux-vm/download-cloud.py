#!/usr/bin/env python3
"""Fetch pinned official Fedora/Arch cloud images without installing anything.

HTTPS checksum manifests are retained alongside the actual byte identity. This
does not claim to verify the manifests' OpenPGP signatures.
"""
import argparse
import hashlib
import json
from pathlib import Path
import re
import time
import urllib.parse
import urllib.request

SOURCES = {
    'fedora': ('https://dl.fedoraproject.org/pub/fedora/linux/releases/45/Cloud/x86_64/images/',
               'Fedora-Cloud-Base-Generic-45-1.4.x86_64.qcow2', 'Fedora-Cloud-45-1.4-x86_64-CHECKSUM'),
    'arch': ('https://geo.mirror.pkgbuild.com/images/v20261115.625457/',
             'Arch-Linux-x86_64-cloudimg-20261115.625457.qcow2',
             'Arch-Linux-x86_64-cloudimg-20261115.625457.qcow2.SHA256'),
}
# Listed by Fedora MirrorManager. Only image bytes use the regional mirror;
# checksums continue to come from dl.fedoraproject.org.
FEDORA_IMAGE_BASE = 'https://mirror.aarnet.edu.au/pub/fedora/linux/releases/45/Cloud/x86_64/images/'

class OfficialRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        target = urllib.parse.urlsplit(newurl)
        if target.scheme != 'https' or target.hostname not in {'dl.fedoraproject.org', 'geo.mirror.pkgbuild.com', 'mirror.aarnet.edu.au'}:
            raise RuntimeError('unexpected image redirect')
        return super().redirect_request(req, fp, code, msg, headers, newurl)

def download(distro, output):
    base, name, manifest = SOURCES[distro]
    output.mkdir(exist_ok=False)
    opener = urllib.request.build_opener(OfficialRedirect())
    with opener.open(base + manifest, timeout=30) as r:
        data = r.read(1024 * 1024 + 1)
    if len(data) > 1024 * 1024:
        raise RuntimeError('checksum manifest exceeds bound')
    (output / manifest).write_bytes(data)
    if distro == 'fedora':
        matches = re.findall(r'^SHA256 \(' + re.escape(name) + r'\) = ([0-9a-f]{64})$', data.decode(), re.M)
    else:
        matches = re.findall(r'^([0-9a-f]{64})  ' + re.escape(name) + r'$', data.decode(), re.M)
    if len(matches) != 1:
        raise RuntimeError('exact image absent or ambiguous in official manifest')
    expected = matches[0]
    h = hashlib.sha256()
    size = 0
    deadline = time.monotonic() + 450
    temp = output / 'image.partial'
    image_url = (FEDORA_IMAGE_BASE if distro == 'fedora' else base) + name
    with opener.open(image_url, timeout=30) as r, temp.open('xb') as f:
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
        raise RuntimeError('image checksum mismatch; keep partial for diagnosis')
    temp.rename(output / name)
    receipt = {'distro': distro, 'url': image_url, 'manifestURL': base + manifest, 'resolvedURL': actual_url,
               'sha256': expected, 'bytes': size, 'file': name,
               'verification': 'official HTTPS manifest SHA256 (not OpenPGP)'}
    (output / 'image.json').write_text(json.dumps(receipt, indent=2) + '\n')
    print(json.dumps(receipt))

if __name__ == '__main__':
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('distro', choices=SOURCES)
    p.add_argument('--output', required=True, type=Path)
    a = p.parse_args()
    download(a.distro, a.output)
