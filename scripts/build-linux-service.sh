#!/bin/sh
# Build an inspectable Linux bundle; never install or change host networking.
set -eu
cd "$(dirname "$0")/.."
arch=${1:-amd64}
case "$arch" in amd64|arm64) ;; *) echo 'expected amd64 or arm64' >&2; exit 1;; esac
expected=$(awk '/^go / { print $2 }' go.mod)
[ "$(go env GOVERSION)" = "go$expected" ] || { echo "requires Go $expected" >&2; exit 1; }
[ -f build/patched-go/xdial.work ] || bash scripts/prepare-patched-go-mod.sh
out="build/linux-service/$arch"
mkdir -p "$out"
GOWORK="$PWD/build/patched-go/xdial.work" GOOS=linux GOARCH="$arch" CGO_ENABLED=0 go build -trimpath -tags with_gvisor,with_utls -ldflags '-s -w' -o "$out/xdial-service" ./cmd/xdial-service
cp packaging/linux/xdial-service.service packaging/linux/install.sh "$out/"
chmod 0755 "$out/install.sh"
(cd "$out"; shasum -a 256 xdial-service xdial-service.service install.sh > SHA256SUMS)
python3 - "$out" <<'PY'
import hashlib,json,pathlib,subprocess,sys,tarfile,gzip,io
p=pathlib.Path(sys.argv[1]); files={v.name:hashlib.sha256(v.read_bytes()).hexdigest() for v in p.iterdir() if v.name in ('xdial-service','install.sh','xdial-service.service','SHA256SUMS')}
m={'head':subprocess.check_output(['git','rev-parse','HEAD'],text=True).strip(),'dirty':bool(subprocess.check_output(['git','status','--porcelain'])),'go':subprocess.check_output(['go','version'],text=True).strip(),'files':files}
(p/'manifest.json').write_text(json.dumps(m,indent=2)+'\n')
epoch=int(subprocess.check_output(['git','log','-1','--format=%ct']))
archive=p.parent/('xdial-service-linux-'+p.name+'.tar.gz')
with archive.open('wb') as raw, gzip.GzipFile(filename='',fileobj=raw,mode='wb',mtime=0) as compressed, tarfile.open(fileobj=compressed,mode='w|') as tar:
 for name in sorted([*files,'manifest.json']):
  data=(p/name).read_bytes();info=tarfile.TarInfo('xdial-service/'+name);info.size=len(data);info.mtime=epoch;info.mode=0o755 if name in ('xdial-service','install.sh') else 0o644;tar.addfile(info,io.BytesIO(data))
(p.parent/(archive.name+'.sha256')).write_text(hashlib.sha256(archive.read_bytes()).hexdigest()+'  '+archive.name+'\n')
PY
printf '%s\n' "$out"
