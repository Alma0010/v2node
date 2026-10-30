"""Fetch the checksum-pinned mihomo client used by the loopback regression test."""

import argparse
import gzip
import hashlib
import io
import json
import os
from pathlib import Path
import platform
import tempfile
import time
import urllib.error
import urllib.request
import zipfile

VERSION = "v1.19.31"
ASSETS = {
    "Windows": ("mihomo-windows-amd64-compatible-v1.19.31.zip", "93d14e9a13b49b2f2d256202d02cc8d14a7c4695edf084cae0f941986bc9c218"),
    "Linux": ("mihomo-linux-amd64-compatible-v1.19.31.gz", "04cf9f09671704f839ddbee2e93069dc831a4123a75281e725d1d96ab9ac1afc"),
}


def fetch(output):
    system = platform.system()
    if system not in ASSETS or platform.machine().lower() not in {"amd64", "x86_64"}:
        raise RuntimeError("The pinned test download supports Linux/Windows amd64")
    name, expected = ASSETS[system]
    url = f"https://github.com/MetaCubeX/mihomo/releases/download/{VERSION}/{name}"
    for attempt in range(3):
        try:
            with urllib.request.urlopen(url, timeout=60) as response:
                archive = response.read()
            break
        except (urllib.error.URLError, TimeoutError):
            if attempt == 2:
                raise
            time.sleep(1)
    if hashlib.sha256(archive).hexdigest() != expected:
        raise RuntimeError("mihomo release checksum mismatch; executable was not written")
    if system == "Windows":
        with zipfile.ZipFile(io.BytesIO(archive)) as content:
            programs = [p for p in content.namelist() if p.lower().endswith(".exe")]
            if len(programs) != 1 or content.testzip() is not None:
                raise RuntimeError("Unexpected mihomo release archive")
            program = content.read(programs[0])
    else:
        program = gzip.decompress(archive)
    output = Path(output).resolve()
    output.parent.mkdir(parents=True, exist_ok=True)
    temporary = None
    try:
        with tempfile.NamedTemporaryFile(dir=output.parent, delete=False) as target:
            temporary = Path(target.name)
            target.write(program)
        if system != "Windows":
            temporary.chmod(0o755)
        os.replace(temporary, output)
    finally:
        if temporary is not None:
            temporary.unlink(missing_ok=True)
    return {"version": VERSION, "asset": name, "archive_sha256": expected, "path": str(output)}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", required=True)
    print(json.dumps(fetch(parser.parse_args().output)))
