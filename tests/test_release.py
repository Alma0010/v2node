import copy
import importlib.util
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
from types import SimpleNamespace
import zipfile

spec = importlib.util.spec_from_file_location('release_helper', Path(__file__).resolve().parents[1] / 'script/release.py')
release = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release)
SHA = 'a' * 40
REPO = 'Alma0010/v2node'


class FakeGitHub:
    def __init__(self):
        self.ref = None
        self.release = None
        self.writes = []
        self.uploads = []
        self.fail_upload = False

    def api(self, path, method='GET', data=None, missing_ok=False):
        if method == 'GET':
            return copy.deepcopy(self.ref if '/git/ref/' in path else self.release)
        self.writes.append((method, path, copy.deepcopy(data)))
        if method == 'POST' and path.endswith('/git/refs'):
            if self.ref is not None:
                raise RuntimeError('Tag exists')
            self.ref = {'object': {'type': 'commit', 'sha': data['sha']}}
            return copy.deepcopy(self.ref)
        if method == 'POST' and path.endswith('/releases'):
            self.release = dict(data, id=1)
            return copy.deepcopy(self.release)
        if method == 'PATCH' and path.endswith('/releases/1'):
            self.release.update(data)
            return copy.deepcopy(self.release)
        raise AssertionError('Unexpected API mutation')

    def upload(self, repo, version, assets, replace=False):
        if self.fail_upload:
            raise RuntimeError('Upload failed')
        if replace:
            assert self.release['draft']
        self.uploads.append((repo, version, list(assets), replace))


class ReleaseTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='v2node-release-')
        self.addCleanup(self.temp.cleanup)
        self.folder = Path(self.temp.name)
        archive = self.folder / 'v2node-linux-64.zip'
        with zipfile.ZipFile(archive, 'w') as content:
            content.writestr('v2node', b'test fixture')
        archive.with_name(archive.name + '.dgst').write_text('test checksum fixture')
        self.github = FakeGitHub()

    def publish(self, event='workflow_dispatch', version='v1.2.3'):
        release.publish(event, version, SHA, REPO, self.folder, self.github.api, self.github.upload)

    def test_push_and_empty_manual_input_only_build(self):
        for event in ['push', 'pull_request', 'workflow_dispatch']:
            self.assertEqual(release.prepare(event, '', '', SHA, REPO, self.github.api), {'version': SHA, 'publish': 'false'})
        self.assertEqual(self.github.writes, [])

    def test_manual_input_is_normalized_and_validated(self):
        self.assertEqual(release.prepare('workflow_dispatch', '1.2.3', '', SHA, REPO, self.github.api), {'version': 'v1.2.3', 'publish': 'true'})
        for value in ['v1.2.3\npublish=true', 'v1.2.3;echo bad', 'v1.2.3$(id)', '../main', 'latest']:
            with self.subTest(value=value), self.assertRaises(ValueError):
                release.prepare('workflow_dispatch', value, '', SHA, REPO, self.github.api)

    def test_existing_version_is_rejected_before_build(self):
        self.github.ref = {'object': {'sha': SHA, 'type': 'commit'}}
        with self.assertRaises(ValueError):
            release.prepare('workflow_dispatch', 'v1.2.3', '', SHA, REPO, self.github.api)
        self.assertEqual(self.github.writes, [])

    def test_release_uses_exact_build_commit_and_uploads_before_publication(self):
        self.publish()
        self.assertEqual(self.github.ref['object']['sha'], SHA)
        self.assertEqual(self.github.release['target_commitish'], SHA)
        self.assertFalse(self.github.release['draft'])
        self.assertEqual(self.github.release['make_latest'], 'true')
        self.assertEqual(len(self.github.uploads[0][2]), 2)
        self.assertTrue(self.github.uploads[0][3])

    def test_failed_upload_stays_draft_and_can_resume(self):
        self.github.fail_upload = True
        with self.assertRaises(RuntimeError):
            self.publish()
        self.assertTrue(self.github.release['draft'])
        self.github.fail_upload = False
        self.publish()
        self.assertFalse(self.github.release['draft'])

    def test_published_release_is_never_overwritten_by_manual_run(self):
        self.github.release = {'id': 1, 'draft': False}
        with self.assertRaises(ValueError):
            self.publish()
        self.assertEqual(self.github.writes, [])
        self.assertEqual(self.github.uploads, [])

    def test_moved_tag_is_rejected(self):
        self.github.ref = {'object': {'type': 'commit', 'sha': 'b' * 40}}
        with self.assertRaises(ValueError):
            self.publish()
        self.assertEqual(self.github.writes, [])

    def test_prerelease_is_not_latest(self):
        self.publish(version='v1.2.3-beta.1')
        self.assertTrue(self.github.release['prerelease'])
        self.assertEqual(self.github.release['make_latest'], 'false')

    def test_existing_release_event_keeps_tag_and_does_not_overwrite(self):
        info = release.prepare('release', '', '1.2.3', SHA, REPO, self.github.api)
        self.assertEqual(info, {'version': '1.2.3', 'publish': 'true'})
        self.github.release = {'id': 1, 'draft': False}
        self.publish(event='release', version='1.2.3')
        self.assertEqual(self.github.writes, [])
        self.assertFalse(self.github.uploads[0][3])

    def test_missing_checksum_never_creates_tag_or_release(self):
        next(self.folder.glob('*.dgst')).unlink()
        with self.assertRaises(ValueError):
            self.publish()
        self.assertEqual(self.github.writes, [])

    def test_api_errors_are_not_mistaken_for_missing_tags(self):
        for status in [403, 500]:
            with patch.object(release.subprocess, 'run', return_value=SimpleNamespace(returncode=1, stdout='', stderr=f'gh: Failed (HTTP {status})')):
                with self.assertRaises(RuntimeError):
                    release.gh_api('repos/test/git/ref/tags/v1.2.3', missing_ok=True)
        with patch.object(release.subprocess, 'run', return_value=SimpleNamespace(returncode=1, stdout='', stderr='gh: Not Found (HTTP 404)')):
            self.assertIsNone(release.gh_api('repos/test/git/ref/tags/v1.2.3', missing_ok=True))


if __name__ == '__main__':
    unittest.main()
