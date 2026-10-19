# Official OpenCode Web UI

Source: https://github.com/anomalyco/opencode/tree/v2.0.32/packages/app

Version: 2.0.32. License: MIT (see LICENSE).

`app.zip` contains the official production Web UI, embedded into the panel.
The panel adapter changes only the deployment base path, routing (the official
hash router, so the app sees root-relative paths), API and attachment URL
prefix, browser CSRF headers, and service-worker registration.
No chat components or agent loop are reimplemented. Source maps are omitted.
The gateway removes the inline theme preload script to retain a restrictive CSP.

Rebuild with Python 3.12+, Node 26 and Bun 1.4.2:

```sh
BUN_BIN=/absolute/path/to/bun python3 scripts/build-opencode-web.py
```

The script downloads the pinned official source tarball, verifies its SHA-256,
installs its locked dependencies, builds the official UI, and packages the assets.
`OPENCODE_BUILD_WORK` can select a temporary build directory.
Build tools are needed only to rebuild this artifact, never on the deployed panel.
