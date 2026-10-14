import assert from 'node:assert/strict';
import { existsSync, readFileSync } from 'node:fs';
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
