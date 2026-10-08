#!/usr/bin/env python3
"""Regression checks for production release refusal on missing/stale evidence."""
import copy
import importlib.util
from pathlib import Path
import sys
import unittest

sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location('gates', Path(__file__).resolve().parents[1] / 'scripts/check-release-gates.py')
gates = importlib.util.module_from_spec(spec)
spec.loader.exec_module(gates)


class ReleaseGates(unittest.TestCase):
    def setUp(self):
        self.record = {'version': '1.0.0', 'source_sha256': gates.source_fingerprint(),
                       'checks': {name: {'passed': True, 'evidence': 'Synthetic fixture evidence'} for name in gates.CHECKS}}

    def test_complete_fixture_matches_source_and_version(self):
        self.assertEqual(gates.validate(self.record, '1.0.0'), [])

    def test_missing_or_unverified_device_checks_block_publication(self):
        for name in gates.CHECKS:
            for value in ({}, {'passed': False, 'evidence': 'Pending'}, {'passed': True, 'evidence': ''}):
                with self.subTest(check=name, value=value):
                    record = copy.deepcopy(self.record)
                    record['checks'][name] = value
                    self.assertTrue(gates.validate(record, '1.0.0'))

    def test_stale_source_or_different_version_blocks_publication(self):
        record = copy.deepcopy(self.record)
        record['source_sha256'] = 'outdated'
        self.assertTrue(gates.validate(record, '1.0.0'))
        self.assertTrue(gates.validate(self.record, '1.0.1'))


if __name__ == '__main__':
    unittest.main()
