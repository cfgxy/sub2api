import importlib.util
import json
from pathlib import Path
import subprocess
import tempfile
import unittest

SCRIPT = Path(__file__).with_name('package-version.py')


class PackageVersionTests(unittest.TestCase):
    def test_two_editions_and_shared_identity(self):
        for edition in ('ent', 'ob'):
            with self.subTest(edition=edition), tempfile.TemporaryDirectory() as directory:
                manifest = Path(directory) / 'manifest.json'
                result = subprocess.run([
                    'python3', str(SCRIPT), '--edition', edition,
                    '--sequence', '7', '--output', str(manifest)
                ], text=True, capture_output=True, check=True)
                data = json.loads(manifest.read_text())
                self.assertEqual(result.stdout.strip(), data['version'])
                self.assertRegex(data['version'], rf'^\d+\.\d+\.\d+-{edition}\.\d{{8}}-7$')
                self.assertEqual(data['build_date'], data['built_at'][:10].replace('-', ''))
                self.assertEqual(data['timezone'], 'UTC')
                self.assertRegex(data['source_sha'], r'^[a-f0-9]{40}$')

    def test_repeated_local_packages_get_distinct_sequences(self):
        with tempfile.TemporaryDirectory() as directory:
            versions = [subprocess.check_output([
                'python3', str(SCRIPT), '--state-dir', directory
            ], text=True).strip() for _ in range(2)]
            first, second = [int(version.rsplit('-', 1)[1]) for version in versions]
            self.assertEqual(second, first + 1)

    def test_concurrent_packages_use_one_sequence_registry(self):
        with tempfile.TemporaryDirectory() as directory:
            processes = [subprocess.Popen([
                'python3', str(SCRIPT), '--state-dir', directory
            ], stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True) for _ in range(8)]
            sequences = []
            for process in processes:
                stdout, stderr = process.communicate(timeout=10)
                self.assertEqual(process.returncode, 0, stderr)
                sequences.append(int(stdout.strip().rsplit('-', 1)[1]))
            self.assertEqual(sorted(sequences), list(range(1, 9)))

    def test_multi_arch_reuses_manifest_identity(self):
        with tempfile.TemporaryDirectory() as directory:
            manifest = Path(directory) / 'manifest.json'
            subprocess.run(['python3', str(SCRIPT), '--sequence', '3',
                            '--output', str(manifest)], capture_output=True, check=True)
            data = json.loads(manifest.read_text())
            for _ in range(2):
                result = subprocess.check_output([
                    'python3', str(SCRIPT), '--require-sequence', '--version', data['version'],
                    '--source-sha', data['source_sha'], '--built-at', data['built_at'],
                    '--upstream', data['upstream_version']
                ], text=True).strip()
                self.assertEqual(result, data['version'])

    def test_reuse_rejects_inconsistent_packaging_facts(self):
        with tempfile.TemporaryDirectory() as directory:
            manifest = Path(directory) / 'manifest.json'
            subprocess.run(['python3', str(SCRIPT), '--sequence', '3',
                            '--output', str(manifest)], capture_output=True, check=True)
            data = json.loads(manifest.read_text())
            other_edition = 'ob' if data['edition'] == 'ent' else 'ent'
            for extra in (['--edition', other_edition], ['--sequence', '4'],
                          ['--built-at', '2000-01-01T00:00:00Z']):
                result = subprocess.run([
                    'python3', str(SCRIPT), '--version', data['version'],
                    '--built-at', data['built_at'], *extra
                ], capture_output=True)
                self.assertNotEqual(result.returncode, 0)

    def test_docker_requires_outer_sequence_assignment(self):
        result = subprocess.run(['python3', str(SCRIPT), '--require-sequence'],
                                capture_output=True)
        self.assertNotEqual(result.returncode, 0)

    def test_invalid_sequence_is_rejected(self):
        for sequence in ('0', '-1', '1.2', 'abc'):
            result = subprocess.run(['python3', str(SCRIPT), '--sequence', sequence],
                                    capture_output=True)
            self.assertNotEqual(result.returncode, 0)

    def test_upstream_tag_resolution_is_not_packaged_twice(self):
        spec = importlib.util.spec_from_file_location('package_version', SCRIPT)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        self.assertEqual(module.format_version('v0.2.17', 'ent', '20261018', 2),
                         '0.2.17-ent.20261018-2')
        with self.assertRaises(ValueError):
            module.format_version('0.2.17-ent.20261018-1', 'ent', '20261018', 2)


if __name__ == '__main__':
    unittest.main()
