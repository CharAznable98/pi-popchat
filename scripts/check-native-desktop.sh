#!/bin/sh
# Run serially with other desktop automation; this probe has its own instance ID.
set -eu
cd "$(dirname "$0")/.."
python3 - <<'PY'
import json, os, pathlib, subprocess, sys, tempfile
app = (pathlib.Path(os.environ.get('PI_POPCHAT_BUILD_DIR', 'dist')) / 'Pi Popchat.app/Contents/MacOS/pi-popchat').resolve()
if not app.is_file():
    sys.exit('请先执行 sh scripts/build-app.sh')
pathlib.Path('work').mkdir(exist_ok=True)
root = pathlib.Path(tempfile.mkdtemp(prefix='native-acceptance-', dir='work'))
report = root.resolve() / 'result.json'
env = dict(os.environ, PI_POPCHAT_CHECK_DESKTOP='1', PI_POPCHAT_CHECK_OUTPUT=str(report))
try:
    with (root/'process.log').open('w') as log:
        p = subprocess.run([str(app)], env=env, stdout=log, stderr=subprocess.STDOUT, timeout=90)
except subprocess.TimeoutExpired:
    sys.exit('原生验收超时，日志：'+str(root/'process.log'))
if not report.exists():
    sys.exit('原生验收没有产生本次JSON（可能已有实例），日志：'+str(root/'process.log'))
r = json.loads(report.read_text())
results = r.get('results', [])
failures = [x for x in results if x.get('pass') is not True]
print(json.dumps(r, ensure_ascii=False, indent=2))
print('验收报告：'+str(report))
if p.returncode != 0 or r.get('suite') != 'owned-native-desktop' or not results or r.get('failed') != 0 or failures:
    sys.exit(1)
PY
