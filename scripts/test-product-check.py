#!/usr/bin/env python3
"""Negative tests for the product audit; only temporary copies are modified."""
import importlib.util
import json
from pathlib import Path
import shutil
import tempfile
import unittest

ROOT=Path(__file__).resolve().parents[1]
spec=importlib.util.spec_from_file_location('product_check',ROOT/'scripts/check-product.py')
checker=importlib.util.module_from_spec(spec);spec.loader.exec_module(checker)

class ProductAuditTests(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory(prefix='svolo-audit-contract-')
        self.root=Path(self.temp.name)/'svolo'
        shutil.copytree(ROOT,self.root,ignore=shutil.ignore_patterns('node_modules','__pycache__','.git','out','dist'))
    def tearDown(self): self.temp.cleanup()
    def rejected(self,fragment):
        result=checker.run(self.root,[])
        self.assertEqual(result['status'],'fail')
        self.assertTrue(any(fragment in e for e in result['errors']),result['errors'])
    def test_current_tree_passes(self):
        result=checker.run(self.root,[])
        self.assertEqual(result['status'],'pass',result['errors'])
    def test_version_drift_is_rejected(self):
        path=self.root/'desktop/package.json';data=json.loads(path.read_text());data['version']='9.9.9';path.write_text(json.dumps(data))
        self.rejected('Desktop version differs')
    def test_bad_asset_is_rejected(self):
        with (self.root/'brand/icons/app-dark-16.png').open('ab') as f:f.write(b'changed')
        self.rejected('Changed asset without manifest update')
    def test_missing_document_is_rejected(self):
        with (self.root/'README.md').open('a') as f:f.write('\n[Broken](docs/does-not-exist.md)\n')
        self.rejected('Broken local link')
    def test_absent_source_import_is_rejected(self):
        (self.root/'desktop/src/fixture.ts').write_text('import "./missing-contract";\n')
        self.rejected('Unresolved local import')
    def test_explicit_text_policy_is_applied(self):
        (self.root/'policy-fixture.txt').write_text('Retired-Identity-Fixture')
        result=checker.run(self.root,['retired-identity-fixture'])
        self.assertEqual(result['status'],'fail')
        self.assertTrue(any('Forbidden text' in e for e in result['errors']))

if __name__=='__main__': unittest.main(verbosity=2)
