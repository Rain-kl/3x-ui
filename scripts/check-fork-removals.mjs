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
