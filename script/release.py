"""Resolve build versions and publish completed archives from GitHub Actions."""

import json
import os
from pathlib import Path
import re
import subprocess
import sys
import zipfile


def gh_api(path, method='GET', data=None, missing_ok=False):
    command = ['gh', 'api', path, '--method', method]
    if data is not None:
        command += ['--input', '-']
    result = subprocess.run(command, input=None if data is None else json.dumps(data), text=True, capture_output=True)
    if result.returncode:
        if missing_ok and method == 'GET' and '(HTTP 404)' in result.stderr:
            return None
        raise RuntimeError(result.stderr.strip() or 'GitHub API request failed')
    return json.loads(result.stdout)


def normalize_version(version):
    version = version.strip()
    if not version.startswith('v'):
        version = 'v' + version
    if not re.fullmatch(r'v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?', version):
        raise ValueError('版本號格式應為 v1.2.3 或 v1.2.3-beta.1')
    return version


def existing_version(version):
    normalized = normalize_version(version)
    return normalized if version.startswith('v') else normalized[1:]


def prepare(event, requested, tag, sha, repo, api=gh_api):
    version, publish = sha, False
    if event == 'release':
        version, publish = existing_version(tag), True
    elif event == 'workflow_dispatch' and requested.strip():
        version, publish = normalize_version(requested), True
        for endpoint in [f'git/ref/tags/{version}', f'releases/tags/{version}']:
            if api(f'repos/{repo}/{endpoint}', missing_ok=True) is not None:
                raise ValueError('這個版本標籤或 Release 已存在，請使用新的版本號')
    return {'version': version, 'publish': 'true' if publish else 'false'}


def upload_assets(repo, version, assets, replace=False):
    command = ['gh', 'release', 'upload', version, '--repo', repo]
    if replace:
        command.append('--clobber')
    subprocess.run(command + [str(path) for path in assets], check=True)


def publish(event, version, sha, repo, directory, api=gh_api, upload=upload_assets):
    version = existing_version(version) if event == 'release' else normalize_version(version)
    if not re.fullmatch(r'[0-9a-f]{40}', sha):
        raise ValueError('Invalid source commit SHA')
    directory = Path(directory)
    archives = sorted(directory.glob('v2node-*.zip'))
    if not archives:
        raise ValueError('No completed release archives found')
    assets = []
    for archive in archives:
        checksum = archive.with_name(archive.name + '.dgst')
        if not checksum.is_file():
            raise ValueError('Missing archive checksum: ' + archive.name)
        with zipfile.ZipFile(archive) as content:
            if content.testzip() is not None:
                raise ValueError('Invalid ZIP: ' + archive.name)
        assets += [archive, checksum]
    base = f'repos/{repo}'
    release = api(f'{base}/releases/tags/{version}', missing_ok=True)
    if event == 'release':
        if release is None:
            raise ValueError('Published release no longer exists')
        upload(repo, version, assets, replace=False)
        return
    if event != 'workflow_dispatch':
        raise ValueError('This event cannot publish a release')
    if release is not None and not release['draft']:
        raise ValueError('Published releases are never overwritten')
    # Recheck after the build, and use an atomic tag creation to prevent a
    # concurrent run from silently publishing against a different commit.
    ref = api(f'{base}/git/ref/tags/{version}', missing_ok=True)
    if ref is None:
        api(f'{base}/git/refs', method='POST', data={'ref': f'refs/tags/{version}', 'sha': sha})
    elif ref['object']['type'] != 'commit' or ref['object']['sha'] != sha:
        raise ValueError('The version tag points to a different commit')
    prerelease = '-' in version
    if release is None:
        release = api(f'{base}/releases', method='POST', data={
            'tag_name': version, 'target_commitish': sha, 'name': 'v2node ' + version,
            'body': f'Built from commit {sha}.', 'draft': True, 'prerelease': prerelease,
            'make_latest': 'false',
        })
    # A failed upload leaves a draft. Re-running the failed publish job can
    # resume that draft without changing an already published release.
    upload(repo, version, assets, replace=True)
    api(f"{base}/releases/{release['id']}", method='PATCH', data={
        'draft': False, 'prerelease': prerelease,
        'make_latest': 'false' if prerelease else 'true',
    })


def main():
    event = os.environ['GITHUB_EVENT_NAME']
    repo, sha = os.environ['GITHUB_REPOSITORY'], os.environ['GITHUB_SHA']
    if sys.argv[1] == 'prepare':
        values = prepare(event, os.environ.get('REQUESTED_VERSION', ''), os.environ.get('RELEASE_TAG', ''), sha, repo)
        with open(os.environ['GITHUB_OUTPUT'], 'a', encoding='utf8') as output:
            for key, value in values.items():
                output.write(f'{key}={value}\n')
        print('Build version:', values['version'], 'Publish:', values['publish'])
    elif sys.argv[1] == 'publish':
        publish(event, os.environ['RELEASE_VERSION'], sha, repo, 'release')
    else:
        raise ValueError('Unknown release operation')


if __name__ == '__main__':
    try:
        main()
    except (ValueError, RuntimeError, subprocess.CalledProcessError) as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
