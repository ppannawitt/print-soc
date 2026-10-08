#!/usr/bin/env python3
"""Inspect the packaged bundle, fail if user data or unexpected resources are included."""
import pathlib,sys,re,plistlib,subprocess
root=pathlib.Path(sys.argv[1])
allowed={'Contents/Info.plist','Contents/PkgInfo','Contents/MacOS/SimplyPrint','Contents/Resources/SimplyPrint.icns','Contents/Resources/Third-Party-Notices.txt'}
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

# The bundle claim and both Mach-O slices must agree on the supported macOS version.
info=plistlib.loads((root/'Contents/Info.plist').read_bytes())
if info.get('LSMinimumSystemVersion')!='13.0':raise SystemExit('Bundle must target macOS 13.0.')
if not info.get('CFBundleName','').startswith('SimplyPrint @ SoC'):raise SystemExit('App branding is incorrect.')
executable=root/'Contents/MacOS/SimplyPrint'
architectures=subprocess.check_output(['lipo','-archs',str(executable)],text=True).split()
if set(architectures)!={'arm64','x86_64'}:raise SystemExit('Universal app must contain arm64 and x86_64.')
for architecture in architectures:
    build=subprocess.check_output(['xcrun','vtool','-arch',architecture,'-show-build',str(executable)],text=True)
    minimum=re.search(r'\bminos\s+(\d+(?:\.\d+){1,2})',build)
    if not minimum or minimum.group(1) not in {'13.0','13.0.0'}:raise SystemExit('Incorrect minimum macOS version for '+architecture)
print('Both universal executable slices target macOS 13.0; bundle branding matches.')
