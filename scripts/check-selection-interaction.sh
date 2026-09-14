#!/bin/sh
# Serial native input tests: dedicated synthetic windows only.
set -eu
cd "$(dirname "$0")/.."
mkdir -p work outputs
probe_dir=$(mktemp -d "$PWD/work/selection-interaction-XXXXXX")
clang -fobjc-arc -fblocks -mmacosx-version-min=15.0 -framework AppKit -framework ApplicationServices -framework WebKit \
  scripts/selection-acceptance.m -o "$probe_dir/selection-acceptance"
python3 - "$probe_dir" <<'PY'
import json,os,pathlib,signal,subprocess,sys,time
root=pathlib.Path(sys.argv[1]); results=[]
cases=[('native',0,0),('web',0,0),('web-readonly',0,0),('secure',0,0),('native',1,0),('native',2,0),('native',0,1)]
if os.environ.get('SELECTION_CASES'):
    cases=[(m,int(s),int(f)) for m,s,f in (x.split(':') for x in os.environ['SELECTION_CASES'].split(','))]
for mode,screen,full in cases:
    name=f'{mode}-{screen}-{full}'
    try:
        p=subprocess.Popen([str(root/'selection-acceptance'),'run',mode,str(screen),str(full),str(root/(name+'.ready.json'))],stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,start_new_session=True)
        try:
            stdout,stderr=p.communicate(timeout=45)
        finally:
            try: os.killpg(p.pid,signal.SIGTERM)
            except ProcessLookupError: pass
        (root/(name+'.stderr')).write_text(stderr)
        (root/(name+'.json')).write_text(stdout)
        r=json.loads(stdout)
    except (subprocess.TimeoutExpired,json.JSONDecodeError) as e:
        if isinstance(e,subprocess.TimeoutExpired):
            stdout,stderr=p.communicate()
            (root/(name+'.stderr')).write_text(stderr)
            (root/(name+'.stdout')).write_text(stdout)
        r={'mode':mode,'screenIndex':screen,'fullscreen':full,'failed':1,'error':type(e).__name__}
    results.append(r)
    pathlib.Path('outputs/selection-interaction-results.json').write_text(json.dumps(results,ensure_ascii=False,indent=2))
    print(json.dumps({'case':name,'failed':r['failed'],'checks':len(r.get('results',[]))}),flush=True)
    print('evidence: '+str(root/(name+'.json')),flush=True)
    time.sleep(1)
pathlib.Path('outputs/selection-interaction-results.json').write_text(json.dumps(results,ensure_ascii=False,indent=2))
sys.exit(1 if any(r['failed'] for r in results) else 0)
PY
