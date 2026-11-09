import os
from pathlib import Path
import shlex
import shutil
import subprocess
import tempfile
import unittest
import zipfile

ROOT = Path(__file__).resolve().parents[1]
BASH = shutil.which('bash') or r'C:\Program Files\Git\bin\bash.exe'
PROXY = 'https://v4.gh-proxy.org/'


class DownloadScriptsTest(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='v2node-script-')
        self.addCleanup(self.temporary.cleanup)
        self.base = Path(self.temporary.name)
        self.sandbox = self.base / 'root'
        self.fixtures = self.base / 'fixtures'
        self.fixtures.mkdir()
        for name in ['usr/local/v2node', 'usr/bin', 'etc/v2node', 'etc/systemd/system', 'etc/init.d']:
            (self.sandbox / name).mkdir(parents=True, exist_ok=True)
        self.program = self.sandbox / 'usr/local/v2node/v2node'
        self.config = self.sandbox / 'etc/v2node/config.json'
        self.program.write_text('old-program')
        self.config.write_text('{"existing":"keep-this"}')
        with zipfile.ZipFile(self.fixtures / 'release.zip', 'w') as archive:
            for name, value in {'v2node':'new-program', 'geoip.dat':'new-geoip', 'geosite.dat':'new-geosite', 'config.json':'{}'}.items():
                archive.writestr(name, value)
        (self.fixtures / 'v2node.sh').write_text('#!/bin/bash\nexit 0\n')
        (self.fixtures / 'install.sh').write_text('#!/bin/bash\nprintf "%s\\n" "$V2NODE_GITHUB_PROXY" "$@" > "$RECEIVED"\n')
        self.log = self.base / 'urls.txt'
        self.received = self.base / 'received.txt'

    def run_script(self, filename, commands, failure=''):
        source = (ROOT / 'script' / filename).read_text(encoding='utf8')
        # All absolute installation destinations are redirected into this test's
        # disposable filesystem. Network and service calls are mocked below.
        for prefix in ['/usr/local', '/usr/bin', '/etc/v2node', '/etc/systemd', '/etc/init.d', '/run/v2node.pid', '/var/log/v2node.log']:
            source = source.replace(prefix, self.sandbox.as_posix() + prefix)
        copy = self.base / filename
        copy.write_text(source, encoding='utf8', newline='\n')
        stub = r'''
systemctl() { return 0; }
service() { return 0; }
sleep() { return 0; }
check_status() { return 0; }
curl() {
    local output="" previous="" argument url=""
    for argument in "$@"; do
        if [[ "$previous" == --output ]]; then output="$argument"; fi
        previous="$argument"
        url="$argument"
    done
    printf '%s\n' "$url" >> "$URL_LOG"
    if [[ "$FAILURE" == download ]]; then return 22; fi
    case "$url" in
        https://api.github.com/repos/Alma0010/v2node/releases/latest)
            printf '{"tag_name":"v1.2.3"}\n' ;;
        */releases/download/v*/v2node-linux-64.zip)
            if [[ "$FAILURE" == archive ]]; then printf invalid > "$output"; else command cp "$FIXTURES/release.zip" "$output"; fi ;;
        */v*/script/v2node.sh|*/main/script/v2node.sh)
            if [[ "$FAILURE" == script ]]; then printf '#!/bin/bash\nif then\n' > "$output"; elif [[ "$FAILURE" == plain-script ]]; then printf 'Access denied\n' > "$output"; else command cp "$FIXTURES/v2node.sh" "$output"; fi ;;
        */main/script/install.sh) command cp "$FIXTURES/install.sh" "$output" ;;
        *) echo "Unexpected URL: $url" >&2; return 91 ;;
    esac
}
'''
        code = 'source ' + shlex.quote(copy.as_posix()) + '\n' + stub + '\n' + commands
        env = dict(os.environ, FIXTURES=self.fixtures.as_posix(), URL_LOG=self.log.as_posix(), RECEIVED=self.received.as_posix(), FAILURE=failure, TMPDIR=self.base.as_posix(), LC_ALL='C.UTF-8')
        env.pop('V2NODE_GITHUB_PROXY', None)
        return subprocess.run([BASH, '-c', code], capture_output=True, text=True, encoding='utf8', errors='replace', env=env, timeout=20)

    def test_proxy_covers_release_and_management_script(self):
        result = self.run_script('install.sh', 'parse_args 1.2.3 --proxy; load_download_proxy; release=debian; arch=64; install_v2node "$VERSION_ARG"')
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        urls = self.log.read_text().splitlines()
        self.assertEqual(urls, [PROXY+'https://github.com/Alma0010/v2node/releases/download/v1.2.3/v2node-linux-64.zip', PROXY+'https://raw.githubusercontent.com/Alma0010/v2node/v1.2.3/script/v2node.sh'])
        self.assertEqual(self.program.read_text(), 'new-program')
        self.assertEqual(self.config.read_text(), '{"existing":"keep-this"}')
        for name, value in [('geoip.dat', 'new-geoip'), ('geosite.dat', 'new-geosite')]:
            self.assertEqual((self.sandbox/'usr/local/v2node'/name).read_text(), value)
            self.assertEqual((self.sandbox/'etc/v2node'/name).read_text(), value)
        self.assertEqual((self.sandbox/'etc/v2node/download-proxy').read_text(), PROXY+'\n')
        self.assertFalse(list((self.sandbox/'usr/local').glob('.v2node-download.*')))

    def test_failed_download_keeps_existing_installation(self):
        for failure in ['download', 'archive', 'script', 'plain-script']:
            with self.subTest(failure=failure):
                result = self.run_script('install.sh', 'parse_args v1.2.3 --proxy; load_download_proxy; release=debian; arch=64; install_v2node "$VERSION_ARG"', failure)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(self.program.read_text(), 'old-program')
                self.assertEqual(self.config.read_text(), '{"existing":"keep-this"}')
                self.assertFalse(list((self.sandbox/'usr/local').glob('.v2node-download.*')))

    def test_latest_resolves_to_versioned_proxy_asset(self):
        result = self.run_script('install.sh', 'parse_args --proxy; load_download_proxy; release=debian; arch=64; install_v2node "$VERSION_ARG"')
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(self.log.read_text().splitlines(), [
            'https://api.github.com/repos/Alma0010/v2node/releases/latest',
            PROXY+'https://github.com/Alma0010/v2node/releases/download/v1.2.3/v2node-linux-64.zip',
            PROXY+'https://raw.githubusercontent.com/Alma0010/v2node/v1.2.3/script/v2node.sh'])

    def test_prerelease_uses_same_tag_for_archive_and_manager(self):
        result = self.run_script('install.sh', 'parse_args v0.4.10-rc.1 --proxy; load_download_proxy; release=debian; arch=64; install_v2node "$VERSION_ARG"')
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(self.log.read_text().splitlines(), [
            PROXY+'https://github.com/Alma0010/v2node/releases/download/v0.4.10-rc.1/v2node-linux-64.zip',
            PROXY+'https://raw.githubusercontent.com/Alma0010/v2node/v0.4.10-rc.1/script/v2node.sh'])

    def test_saved_route_reaches_update_installer(self):
        (self.sandbox/'etc/v2node/download-proxy').write_text(PROXY+'\n')
        result = self.run_script('v2node.sh', 'run_installer v1.2.3')
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(self.log.read_text().strip(), PROXY+'https://raw.githubusercontent.com/Alma0010/v2node/main/script/install.sh')
        self.assertEqual(self.received.read_text().splitlines(), [PROXY, 'v1.2.3'])

    def test_direct_override(self):
        (self.sandbox/'etc/v2node/download-proxy').write_text(PROXY+'\n')
        result = self.run_script('v2node.sh', 'run_installer v1.2.3 --direct')
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(self.log.read_text().strip(), 'https://raw.githubusercontent.com/Alma0010/v2node/main/script/install.sh')
        self.assertEqual(self.received.read_text().splitlines(), ['', 'v1.2.3', '--direct'])

    def test_update_shell_replaces_manager_without_calling_install_function(self):
        manager = self.sandbox / 'usr/bin/v2node'
        manager.write_text('old-manager')
        result = self.run_script('v2node.sh', 'DOWNLOAD_PROXY="$DEFAULT_GITHUB_PROXY"; PROXY_ARG_SET=1; update_shell')
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(manager.read_text(), (self.fixtures / 'v2node.sh').read_text())
        self.assertEqual(self.log.read_text().strip(), PROXY+'https://raw.githubusercontent.com/Alma0010/v2node/main/script/v2node.sh')
        self.assertFalse(list(self.base.glob('v2node-shell.*')))

    def test_update_shell_invalid_download_preserves_manager(self):
        manager = self.sandbox / 'usr/bin/v2node'
        manager.write_text('old-manager')
        result = self.run_script('v2node.sh', 'PROXY_ARG_SET=1; update_shell', 'script')
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(manager.read_text(), 'old-manager')
        self.assertFalse(list(self.base.glob('v2node-shell.*')))

    def test_install_repairs_legacy_tagged_updater(self):
        source = (ROOT / 'script/v2node.sh').read_text(encoding='utf8')
        fixed = 'command install -m 755 "$temporary" /usr/bin/v2node.new'
        legacy = 'install -m 755 "$temporary" /usr/bin/v2node.new'
        self.assertEqual(source.count(fixed), 1)
        (self.fixtures / 'v2node.sh').write_text(source.replace(fixed, legacy), encoding='utf8', newline='\n')
        result = self.run_script('install.sh', 'parse_args v1.2.3 --proxy; load_download_proxy; release=debian; arch=64; install_v2node "$VERSION_ARG"')
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        manager = (self.sandbox / 'usr/bin/v2node').read_text(encoding='utf8')
        self.assertEqual(manager.count(fixed), 1)
        self.assertEqual(self.log.read_text().splitlines()[-1], PROXY+'https://raw.githubusercontent.com/Alma0010/v2node/v1.2.3/script/v2node.sh')

    def test_install_preserves_already_fixed_tagged_updater(self):
        source = (ROOT / 'script/v2node.sh').read_text(encoding='utf8')
        (self.fixtures / 'v2node.sh').write_text(source, encoding='utf8', newline='\n')
        result = self.run_script('install.sh', 'parse_args v1.2.3 --proxy; load_download_proxy; release=debian; arch=64; install_v2node "$VERSION_ARG"')
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual((self.sandbox / 'usr/bin/v2node').read_text(encoding='utf8'), source)

    def test_missing_option_value_fails(self):
        result = self.run_script('install.sh', 'parse_args --api-key')
        self.assertNotEqual(result.returncode, 0)


if __name__ == '__main__':
    unittest.main()
