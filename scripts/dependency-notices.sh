#!/usr/bin/env bash
set -euo pipefail
output="${1:?Pass the output notices path}"
python3 - "$output" <<'PY'
import json,subprocess,sys,pathlib
raw=subprocess.check_output(['go','list','-deps','-json','./cmd/socprint-macos'],text=True)
decoder=json.JSONDecoder();modules=[]
while raw.strip():
    package,end=decoder.raw_decode(raw.lstrip());
    module=package.get('Module');
    if module and module not in modules:modules.append(module)
    raw=raw.lstrip()[end:]
parts=['SimplyPrint @ SoC — License and third-party software notices\nVersions correspond to the modules used by the Mac executable.\n', pathlib.Path('LICENSE').read_text(), pathlib.Path(subprocess.check_output(['go','env','GOROOT'],text=True).strip(),'LICENSE').read_text()]
for module in modules:
    if module.get('Main'):continue
    path=pathlib.Path(module.get('Dir',''))
    licenses=sorted({*path.glob('LICENSE*'),*path.glob('COPYING*'),*path.glob('NOTICE*')}) if module.get('Dir') else []
    parts.append('\n'+module['Path']+' '+module.get('Version','')+'\n')
    if not licenses:raise SystemExit('Missing dependency license: '+module['Path'])
    for file in licenses:
        if file.is_file():parts.append(file.name+'\n'+file.read_text(errors='replace')+'\n')
pathlib.Path(sys.argv[1]).write_text('\n'.join(parts))
PY
