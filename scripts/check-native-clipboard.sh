#!/bin/sh
# Run while no other Pi Popchat instance is running. Temporarily exercises the
# system pasteboard, restoring its contents unless another app changes them.
set -eu
cd "$(dirname "$0")/.."
python3 - <<'PY'
import json, os, pathlib, subprocess, sys, tempfile
app=pathlib.Path('dist/Pi Popchat.app/Contents/MacOS/pi-popchat').resolve()
if not app.is_file(): sys.exit('请先执行 sh scripts/build-app.sh')
pathlib.Path('work').mkdir(exist_ok=True)
root=pathlib.Path(tempfile.mkdtemp(prefix='clipboard-acceptance-',dir='work'))
report=root.resolve()/'result.json'
env=dict(os.environ,PI_POPCHAT_CHECK_CLIPBOARD='1',PI_POPCHAT_CHECK_OUTPUT=str(report))
with (root/'process.log').open('w') as log:
    p=subprocess.run([str(app)],env=env,stdout=log,stderr=subprocess.STDOUT,timeout=90)
if not report.exists(): sys.exit('本次未产生验收结果，请检查是否已有实例：'+str(root/'process.log'))
r=json.loads(report.read_text())
print(json.dumps(r,ensure_ascii=False,indent=2))
if p.returncode or r.get('suite')!='native-clipboard' or len(r.get('results',[]))!=7 or r.get('failed')!=0 or any(x.get('pass') is not True for x in r['results']): sys.exit(1)
PY
