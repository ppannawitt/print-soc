#!/usr/bin/env python3
"""Fail production packaging until recorded manual checks match this app source."""
import argparse
import hashlib
import json
from pathlib import Path
import sys

ROOT = Path(__file__).resolve().parent.parent
CHECKS = ('apple_silicon_install_upgrade', 'intel_install_upgrade', 'macos_13',
          'visual_keyboard_voiceover', 'real_soc_print_queue_cancel', 'keychain_account_switching')


def source_fingerprint():
    files = [p for directory in ('cmd', 'internal', 'macos', 'scripts', 'tests', '.github/workflows')
             for p in (ROOT / directory).rglob('*') if p.is_file()]
    files.extend(ROOT / name for name in ('go.mod', 'go.sum'))
    digest = hashlib.sha256()
    for path in sorted(files):
        digest.update(path.relative_to(ROOT).as_posix().encode() + b'\0')
        digest.update(path.read_bytes() + b'\0')
    return digest.hexdigest()


def validate(record, version):
    issues = []
    if record.get('version') != version:
        issues.append('Validation version does not match the release.')
    if record.get('source_sha256') != source_fingerprint():
        issues.append('Validation does not match the current app source fingerprint.')
    checks = record.get('checks', {})
    for name in CHECKS:
        check = checks.get(name, {})
        if check.get('passed') is not True or not isinstance(check.get('evidence'), str) or not check['evidence'].strip():
            issues.append('Manual check is incomplete: ' + name)
    return issues


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--fingerprint', action='store_true')
    parser.add_argument('--version', default='1.0.0')
    args = parser.parse_args()
    if args.fingerprint:
        print(source_fingerprint())
        sys.exit(0)
    try:
        record = json.loads((ROOT / 'release/validation.json').read_text())
        if not isinstance(record, dict):
            raise ValueError('Expected an object')
        issues = validate(record, args.version)
    except (ValueError, OSError, TypeError, AttributeError) as error:
        issues = ['Cannot read the release validation record: ' + str(error)]
    if issues:
        print('Public release blocked:\n- ' + '\n- '.join(issues), file=sys.stderr)
        sys.exit(1)
    print('Recorded manual release checks match this source and version.')
