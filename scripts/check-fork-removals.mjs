import assert from 'node:assert/strict';
import { existsSync, readFileSync, readdirSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const root = fileURLToPath(new URL('../', import.meta.url));
const read = (name) => readFileSync(path.join(root, name), 'utf8');

assert.equal(existsSync(path.join(root, 'internal/trafficshaper')), false,
  'native-rate-limits: the removed TC shaper must stay absent');
for (const name of ['main.go', 'internal/web/job/check_client_ip_job.go',
  'internal/web/service/inbound.go', 'internal/web/service/client_crud.go']) {
  assert.doesNotMatch(read(name), /trafficshaper|syncTrafficShaper|syncClientTrafficShaper/,
    `native-rate-limits: ${name} must not restore TC hooks`);
}
assert.match(read('internal/database/model/model.go'), /NativeRateLimitSettings/);
assert.match(read('internal/web/runtime/local.go'), /SetUserRateLimits/);
assert.match(read('internal/xray/api.go'), /DownlinkGroupSpeedLimit:\s+nativeRateLimitUint/);
assert.match(read('internal/web/service/xray.go'), /NativeRateLimitsChanged/);
console.log('native-rate-limits: TC removal and core integration guards passed');

for (const name of ['internal/web/service/server.go', 'internal/web/service/setting.go',
  'internal/web/controller/server.go', 'internal/web/service/panel/panel.go']) {
  assert.doesNotMatch(read(name), /GetXrayVersionLock|SetXrayVersionLock|XUI_LOCK_XRAY/);
}
assert.doesNotMatch(read('frontend/src/pages/index/VersionModal.tsx'), /isLocked|selectedVersion|<Select|<Switch/);
assert.doesNotMatch(read('frontend/src/layouts/AppSidebar.tsx'), /SponsorSlot|key: '\/sponsors'/);
for (const file of [
  'frontend/src/routes.tsx',
  'frontend/src/pages/index/IndexPage.tsx',
  'frontend/src/pages/login/LoginPage.tsx',
  'frontend/src/pages/api-docs/endpoints.ts',
  'internal/web/controller/index.go',
]) {
  assert.doesNotMatch(read(file), /SponsorSlot|sponsors|SponsorList|GetSponsor/);
}
assert.match(read('internal/audit/spool.go'), /e.Inbound ===? "api"/);
console.log('custom-core-only and audit-workbench removals passed');

const removedSponsorPaths = [
  'frontend/src/components/sponsor',
  'frontend/src/pages/sponsors',
  'frontend/src/lib/sponsors.ts',
  'internal/web/service/panel/sponsor.go',
];
for (const name of removedSponsorPaths) {
  assert.equal(existsSync(path.join(root, name)), false, `audit-workbench: removed advertising implementation returned: ${name}`);
}
function checkSponsorWiring(directory) {
  for (const entry of readdirSync(path.join(root, directory), { withFileTypes: true })) {
    const name = `${directory}/${entry.name}`;
    if (entry.isDirectory()) {
      if (entry.name !== 'dist' && entry.name !== 'translation') checkSponsorWiring(name);
      continue;
    }
    if (!/\.(?:go|tsx?|mjs)$/.test(name) || /(?:_test\.go|\.test\.|\.stories\.)/.test(name)) continue;
    const source = read(name).split('\n').filter((line) => !line.trimStart().startsWith('//')).join('\n');
    assert.doesNotMatch(source, /\b(?:SponsorSlot|SponsorCard|SponsorList|SponsorsPage|GetSponsor\w*|FetchSponsor\w*)\b|['"`]\/(?:sponsors|sponsor-logo)(?:[\/'"`?]|$)/,
      `audit-workbench: advertising wiring returned in ${name}`);
  }
}
checkSponsorWiring('frontend/src');
checkSponsorWiring('internal/web');
for (const entry of readdirSync(path.join(root, 'internal/web/translation'))) {
  if (!entry.endsWith('.json')) continue;
  const locale = JSON.parse(read(`internal/web/translation/${entry}`));
  assert.equal(locale.menu?.sponsors, undefined, `audit-workbench: sponsor menu translation returned in ${entry}`);
  assert.equal(locale.pages?.sponsors, undefined, `audit-workbench: sponsor page translation returned in ${entry}`);
}
console.log('audit-workbench: sponsor implementations, new entry points and locale guards passed');
