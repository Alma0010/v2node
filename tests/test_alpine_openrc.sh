#!/bin/bash
set -euo pipefail

if [[ "${1:-}" != --inside ]]; then
    fixture_dir=$(mktemp -d "${TMPDIR:-/tmp}/v2node-openrc.XXXXXX")
    trap 'rm -rf -- "$fixture_dir"' EXIT
    cat > "$fixture_dir/daemon.go" <<'GO'
package main

import (
    "fmt"
    "os"
    "time"
)

var version string

func main() {
    fmt.Println("fixture version " + version)
    fmt.Fprintln(os.Stderr, "fixture stderr " + version)
    for { time.Sleep(time.Hour) }
}
GO
    for version in old new; do
        CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o "$fixture_dir/$version" -ldflags "-X main.version=$version" "$fixture_dir/daemon.go"
    done
    curl -fL --retry 2 https://github.com/Alma0010/v2node/releases/download/v0.4.8/v2node-linux-64.zip -o "$fixture_dir/release.zip"
    echo "d297f0ece9e164a39a518d4fe23f4332c4374c0ea1d55388a827a6f23ce05a41  $fixture_dir/release.zip" | sha256sum -c -
    mkdir "$fixture_dir/release"
    unzip -q "$fixture_dir/release.zip" v2node -d "$fixture_dir/release"
    docker run --rm \
        --mount "type=bind,src=$PWD,dst=/work,readonly" \
        --mount "type=bind,src=$fixture_dir,dst=/fixtures,readonly" \
        alpine:3.21 sh -ec 'apk add --no-cache bash openrc curl unzip zip coreutils >/dev/null; exec bash /work/tests/test_alpine_openrc.sh --inside'
    exit
fi

# This runs only in the disposable Alpine container. It uses real OpenRC and
# real ELF processes, without contacting a panel or changing any host service.
mkdir -p /run/openrc /usr/local/v2node /etc/v2node /tmp/bundle
touch /run/openrc/softlevel
cat > /etc/init.d/net <<'RC'
#!/sbin/openrc-run
start() { return 0; }
stop() { return 0; }
RC
chmod +x /etc/init.d/net
rc-service --nodeps net start
cat > /etc/init.d/v2node <<'RC'
#!/sbin/openrc-run
command="/usr/local/v2node/v2node"
command_args="server"
command_background="yes"
pidfile="/run/v2node.pid"
depend() { need net; }
RC
chmod +x /etc/init.d/v2node
install -m 755 /fixtures/old /usr/local/v2node/v2node
printf '{"existing":"keep-this"}\n' > /etc/v2node/config.json
original_config=$(sha256sum /etc/v2node/config.json)
rc-service v2node start
old_pid=$(cat /run/v2node.pid)

# Reproduce the old installer's atomic replacement followed by start. The
# service remains marked started, but the old process still owns the old inode.
install -m 755 /fixtures/new /usr/local/v2node/v2node.new
mv /usr/local/v2node/v2node.new /usr/local/v2node/v2node
rc-service v2node start
if rc-service v2node status; then
    echo "Expected the old upgrade sequence to report crashed" >&2
    exit 1
fi
kill -0 "$old_pid"
[[ "$(readlink "/proc/$old_pid/exe")" == '/usr/local/v2node/v2node (deleted)' ]]
echo 'Reproduced: OpenRC says crashed while the old executable is still running'

source /work/script/install.sh
release=alpine
arch=64
cur_dir=/tmp
install -m 755 /fixtures/new /tmp/bundle/v2node
printf fixture > /tmp/bundle/geoip.dat
printf fixture > /tmp/bundle/geosite.dat
printf '{}' > /tmp/bundle/config.json
zip -jq /tmp/release.zip /tmp/bundle/*
download_file() {
    case "$1" in
        *.zip) cp /tmp/release.zip "$2" ;;
        */script/v2node.sh) cp /work/script/v2node.sh "$2" ;;
        *) return 91 ;;
    esac
}

(install_v2node v0.4.8)
rc-service v2node status
new_pid=$(cat /run/v2node.pid)
[[ "$new_pid" != "$old_pid" ]]
[[ "$(readlink "/proc/$new_pid/exe")" == /usr/local/v2node/v2node ]]
[[ "$original_config" == "$(sha256sum /etc/v2node/config.json)" ]]
grep -q 'fixture stderr new' /var/log/v2node.log
echo 'Recovered the deleted-inode process and retained the existing config'

(install_v2node v0.4.8)
rc-service v2node status
updated_pid=$(cat /run/v2node.pid)
[[ "$updated_pid" != "$new_pid" ]]
[[ "$original_config" == "$(sha256sum /etc/v2node/config.json)" ]]
echo 'A normal running-service upgrade also restarted correctly'

if (download_file() { return 22; }; install_v2node v0.4.8); then
    echo 'An unsuccessful download unexpectedly succeeded' >&2
    exit 1
fi
rc-service v2node status
[[ "$(cat /run/v2node.pid)" == "$updated_pid" ]]
echo 'Download failure left the running service untouched'

set +e
timeout 1 bash -c 'source /work/script/v2node.sh; release=alpine; show_log 0' > /tmp/read-log.txt
log_exit=$?
set -e
[[ "$log_exit" == 124 ]]
grep -q 'fixture stderr new' /tmp/read-log.txt
echo 'The Alpine management log command reads the captured daemon output'

# Execute the real published binary under musl as a separate compatibility
# check. No node configuration or credentials are supplied.
chmod +x /fixtures/release/v2node 2>/dev/null || true
/fixtures/release/v2node version
rc-service v2node stop
echo 'Alpine 3.21 OpenRC lifecycle regression passed'
