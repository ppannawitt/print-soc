#!/usr/bin/env python3
"""Inspect the packaged bundle, fail if user data or unexpected resources are included."""
import pathlib,sys,re
root=pathlib.Path(sys.argv[1])
allowed={'Contents/Info.plist','Contents/PkgInfo','Contents/MacOS/socprint','Contents/Resources/socprint.icns','Contents/Resources/Third-Party-Notices.txt'}
for file in root.rglob('*'):
    if file.is_symlink():raise SystemExit('Unexpected symlink in bundle: '+str(file))
    if file.is_file():
        relative=str(file.relative_to(root))
        if relative not in allowed and not relative.startswith('Contents/_CodeSignature/'):raise SystemExit('Unexpected bundled file: '+relative)
        content=file.read_bytes()
        if re.search(rb'-----BEGIN (?:RSA |OPENSSH |EC |ENCRYPTED )?PRIVATE KEY-----[\r\n]+[A-Za-z0-9+/=\r\n]{100,}-----END',content):raise SystemExit('Private key material found in '+relative)
for relative in allowed:
    if not (root/relative).is_file():raise SystemExit('Missing bundle resource: '+relative)
print('Bundle contains only approved app resources; no user files or private-key material detected.')
