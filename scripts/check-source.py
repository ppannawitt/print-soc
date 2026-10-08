#!/usr/bin/env python3
"""Scan publishable source files without printing matched secrets."""
from pathlib import Path
import re
import subprocess

root = Path(__file__).resolve().parent.parent
paths = subprocess.check_output(['git', 'ls-files', '-c', '-o', '--exclude-standard', '-z'], cwd=root).decode().split('\0')
allowed_roots = {'.github', 'bin', 'cmd', 'data', 'docs', 'internal', 'macos', 'release', 'scripts', 'site', 'src', 'tests'}
allowed_files = {'.gitignore', 'LICENSE', 'README.md', 'SECURITY.md', 'go.mod', 'go.sum', 'package.json'}
allowed_suffixes = {'.go', '.m', '.h', '.js', '.mjs', '.json', '.md', '.svg', '.css', '.html', '.plist', '.sh', '.py', '.yml', '.yaml', '.txt'}
patterns = {
    'private key material': rb'-----BEGIN (?:RSA |OPENSSH |EC |ENCRYPTED )?PRIVATE KEY-----\s+[A-Za-z0-9+/=\r\n]{100,}',
    'GitHub token': rb'\b(?:gh[pousr]_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{50,})\b',
    'AWS access key': rb'\b(?:AKIA|ASIA)[A-Z0-9]{16}\b',
    'OpenAI API key': rb'\bsk-(?:proj-|svcacct-)?[A-Za-z0-9_-]{40,}\b',
    'personal absolute path': rb'/' + rb'Users/[^/\s]+/',
}
issues = []
for raw in sorted(set(paths)):
    if not raw:
        continue
    relative = Path(raw)
    path = root / relative
    if raw not in allowed_files and (relative.parts[0] not in allowed_roots or
       (relative.suffix not in allowed_suffixes and raw != 'site/.nojekyll')):
        issues.append(raw + ': unexpected publishable file')
        continue
    if path.is_symlink() or not path.is_file():
        issues.append(raw + ': symlinks and non-files are not publishable')
        continue
    data = path.read_bytes()
    for name, pattern in patterns.items():
        if re.search(pattern, data):
            issues.append(raw + ': ' + name)
if issues:
    raise SystemExit('Source publication blocked:\n' + '\n'.join(issues))
print('Source allowlist and credential/user-data pattern scan passed.')
