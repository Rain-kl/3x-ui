#!/usr/bin/env python3
"""Build the pinned official OpenCode UI with the panel's path/auth adapter."""
import hashlib
import io
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import urllib.request
import zipfile

VERSION = '2.0.32'
SOURCE_SHA256 = 'd9e080eae0965104dfe3857aabbc8ce976e9aeaa62110ce101bc1a107d243d97'
ROOT = Path(__file__).resolve().parents[1]
WORK = Path(os.environ.get('OPENCODE_BUILD_WORK', ROOT / 'work/opencode-web'))
SOURCE = WORK / 'source'


def replace(path, old, new):
    file = SOURCE / path
    text = file.read_text()
    if text.count(old) != 1:
        raise RuntimeError(f'Official source changed: {path}: expected one adapter insertion')
    file.write_text(text.replace(old, new))


def main():
    WORK.mkdir(parents=True, exist_ok=True)
    data = urllib.request.urlopen(f'https://codeload.github.com/anomalyco/opencode/tar.gz/refs/tags/v{VERSION}').read()
    if hashlib.sha256(data).hexdigest() != SOURCE_SHA256:
        raise RuntimeError('Official OpenCode source checksum mismatch')
    SOURCE.mkdir(exist_ok=True)
    with tarfile.open(fileobj=io.BytesIO(data), mode='r:gz') as archive:
        for member in archive.getmembers():
            member.name = '/'.join(member.name.split('/')[1:])
            if member.name:
                archive.extract(member, SOURCE, filter='data')
    adapt(SOURCE)
    bun = os.environ.get('BUN_BIN', 'bun')
    subprocess.run([bun, 'install', '--frozen-lockfile', '--ignore-scripts', '--filter', '@opencode/app'], cwd=SOURCE, check=True)
    subprocess.run([bun, 'run', 'build'], cwd=SOURCE / 'packages/app', check=True)
    output = ROOT / 'internal/panelai/officialweb'
    output.mkdir(exist_ok=True)
    shutil.copyfile(SOURCE / 'LICENSE', output / 'LICENSE')
    with zipfile.ZipFile(output / 'app.zip', 'w', compression=zipfile.ZIP_DEFLATED, compresslevel=9) as archive:
        for file in sorted((SOURCE / 'packages/app/dist').rglob('*')):
            if file.is_file() and file.suffix != '.map' and file.name not in ('sw.js', 'registerSW.js'):
                archive.write(file, arcname=file.relative_to(SOURCE / 'packages/app/dist'))


def adapt(source):
    global SOURCE
    SOURCE = source
    replace('packages/app/vite.config.ts', 'export default defineConfig({', 'export default defineConfig({\n  base: "./",')
    # Upstream parses useLocation().pathname as root-relative; Router base would keep the panel prefix in it.
    replace('packages/app/src/app.tsx', 'import { type BaseRouterProps, Router } from "@solidjs/router"', 'import { type BaseRouterProps, HashRouter } from "@solidjs/router"')
    replace('packages/app/src/app.tsx', '<Dynamic component={props.router ?? Router} root={Root}>', '<Dynamic component={props.router ?? HashRouter} root={Root}>')
    replace('packages/app/src/runtime/platform/web.ts', 'return location.origin', 'return location.origin + document.querySelector<HTMLMetaElement>(\'meta[name="panel-opencode-base"]\')!.content')
    replace('packages/app/src/entry.tsx', 'if (import.meta.env.PROD && "serviceWorker" in navigator)', 'if (false && "serviceWorker" in navigator)')
    replace('packages/app/src/composer/attachments/destination.ts', 'new URL("/api/experimental/fs/write", server.conn.http.url)', 'new URL("api/experimental/fs/write", server.conn.http.url.replace(/\/$/, "") + "/")')
    replace('packages/app/src/entry.tsx', 'const web = createWebPlatform(pkg.version)', '''const panelBase = document.querySelector<HTMLMetaElement>('meta[name="panel-opencode-base"]')!.content
const nativeFetch = globalThis.fetch.bind(globalThis)
globalThis.fetch = async (input, init) => {
  const request = new Request(input, init)
  const url = new URL(request.url)
  if (url.origin === location.origin && url.pathname.startsWith(panelBase + "/")) {
    if (!["GET", "HEAD", "OPTIONS"].includes(request.method)) {
      const response = await nativeFetch(panelBase + "/csrf", { cache: "no-store" })
      if (!response.ok) throw new Error("Panel administrator session expired")
      const token = await response.text()
      request.headers.set("X-CSRF-Token", token)
    }
    return nativeFetch(request)
  }
  return nativeFetch(request)
}
const web = createWebPlatform(pkg.version)''')


if __name__ == '__main__':
    main()
