#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
python3 - <<'PY'
import json,os,pathlib,subprocess,sys,tempfile
pathlib.Path('work').mkdir(exist_ok=True)
root=pathlib.Path(tempfile.mkdtemp(prefix='notification-acceptance-',dir='work')).resolve()
report=root/'result.json'
env=dict(os.environ,PI_POPCHAT_CHECK_NOTIFICATIONS='1',PI_POPCHAT_CHECK_OUTPUT=str(report))
with (root/'process.log').open('w') as log:
    p=subprocess.run([str(pathlib.Path('dist/Pi Popchat.app/Contents/MacOS/pi-popchat').resolve())],env=env,stdout=log,stderr=subprocess.STDOUT,timeout=45)
if not report.exists():sys.exit('通知验收没有生成结果，请先退出已有应用。日志：'+str(root/'process.log'))
r=json.loads(report.read_text());print(json.dumps(r,ensure_ascii=False,indent=2));print('报告：'+str(report))
if p.returncode or not all(r.get(k) is True for k in ['systemDelivered','noFocusSteal','nativeCallbackTargetsSession']):sys.exit(1)
PY
