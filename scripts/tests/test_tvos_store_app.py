"""Preset acceptance uses synthetic inputs only, never the private packaging repo."""
import importlib.util
import json
import os
from pathlib import Path
import shutil
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('tvos_verify', ROOT / 'scripts/verify-tvos-store-app.py')
verify = importlib.util.module_from_spec(spec)
spec.loader.exec_module(verify)


class TVPresetGateTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.app = Path(self.tmp.name) / 'XDialTV.app'
        self.authority = Path(self.tmp.name) / 'authority'
        self.app.mkdir()
        self.authority.mkdir()
        shutil.copyfile(ROOT / 'ios/Examples/Seed.example.json', self.authority / 'Seed.json')

    def embed(self):
        shutil.copytree(self.authority, self.app / 'PrivateConfig')

    def test_blank_is_independent_and_rejects_even_empty_private_directory(self):
        verify.verify_seed(self.app)
        (self.app / 'PrivateConfig').mkdir()
        with self.assertRaises(SystemExit):
            verify.verify_seed(self.app)

    def test_company_requires_source_and_packaged_seed(self):
        with self.assertRaises(SystemExit):
            verify.verify_seed(self.app, self.authority)
        self.embed()
        with self.assertRaises(SystemExit):
            verify.verify_seed(self.app)
        verify.verify_seed(self.app, self.authority)

    def test_revision_and_history_cannot_drift(self):
        self.embed()
        packaged = self.app / 'PrivateConfig/Seed.json'
        seed = json.loads(packaged.read_bytes())
        seed['revision'] += 1
        packaged.write_text(json.dumps(seed))
        with self.assertRaises(SystemExit):
            verify.verify_seed(self.app, self.authority)

    def test_identical_but_invalid_seed_is_not_accepted(self):
        (self.authority / 'Seed.json').write_text('{"format":"xdial-mobile-seed","revision":3,"profile":{}}')
        self.embed()
        with self.assertRaises(SystemExit):
            verify.verify_seed(self.app, self.authority)

    def test_offline_rule_drift_is_rejected(self):
        rules = self.authority / 'RuleSets'
        rules.mkdir()
        (rules / 'manifest.json').write_text(json.dumps({'https://rules.example.com/region-ips.srs': 'region.srs'}))
        (rules / 'region.srs').write_bytes(b'fixture-rule-bytes')
        self.embed()
        verify.verify_seed(self.app, self.authority)
        (self.app / 'PrivateConfig/RuleSets/region.srs').write_bytes(b'drift')
        with self.assertRaises(SystemExit):
            verify.verify_seed(self.app, self.authority)

    def test_unexpected_private_file_and_signing_keys_are_rejected(self):
        self.embed()
        for relative in ('PrivateConfig/Auth.p8', 'PrivateConfig/extra.json', 'PrivateConfig/RuleSets/extra.json', 'PlugIns/Seed.json'):
            path = self.app / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text('fixture')
            with self.assertRaises(SystemExit):
                verify.verify_seed(self.app, self.authority)
            path.unlink()

    @unittest.skipUnless(os.environ.get('XDIAL_TVOS_TEST_APP'), 'actual built app not supplied')
    def test_actual_built_preset_matches_example_authority(self):
        verify.verify_seed(Path(os.environ['XDIAL_TVOS_TEST_APP']), self.authority)


if __name__ == '__main__':
    unittest.main()
