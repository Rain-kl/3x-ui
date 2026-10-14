import { readFileSync, readdirSync } from 'node:fs';
import { resolve, relative } from 'node:path';
import { execFileSync } from 'node:child_process';

const root = process.cwd();
const read = (file) => readFileSync(resolve(root, file), 'utf8');
const fail = (message) => {
  throw new Error(message);
};
const unquote = (value) => value.trim().replace(/^(['"])(.*)\1$/, '$2');

try {
  const expectedNode = read('.nvmrc').trim().replace(/^v/, '').split('.')[0];
  if (process.versions.node.split('.')[0] !== expectedNode) {
    fail(`Node ${expectedNode} required; found ${process.versions.node}`);
  }
  const expectedGo = read('go.mod').match(/^go (\d+\.\d+(?:\.\d+)?)$/m)?.[1];
  const actualGo = execFileSync('go', ['env', 'GOVERSION'], { encoding: 'utf8' }).trim();
  const required = expectedGo?.split('.').map(Number);
  const actual = actualGo.match(/^go(\d+\.\d+(?:\.\d+)?)/)?.[1].split('.').map(Number);
  const difference = required && actual && [0, 1, 2].find((i) => (actual[i] ?? 0) !== (required[i] ?? 0));
  if (!required || !actual || (difference !== undefined && (actual[difference] ?? 0) < (required[difference] ?? 0))) {
    fail(`Go ${expectedGo} or newer required; found ${actualGo}`);
  }
  const frontend = JSON.parse(read('frontend/package.json'));
  const expectedPnpm = frontend.packageManager?.match(/^pnpm@(\d+\.\d+\.\d+)/)?.[1];
  const actualPnpm = execFileSync('pnpm', ['--version'], {
    cwd: resolve(root, 'frontend'), encoding: 'utf8',
  }).trim();
  if (!expectedPnpm || actualPnpm !== expectedPnpm) {
    fail(`frontend pins pnpm ${expectedPnpm}; found ${actualPnpm}`);
  }

  let steps = 0;
  for (const file of readdirSync(resolve(root, '.github/workflows')).filter((name) => /\.ya?ml$/.test(name))) {
    const lines = read(`.github/workflows/${file}`).split('\n');
    for (let i = 0; i < lines.length; i++) {
      const uses = lines[i].match(/^(\s*)(-\s*)?uses:\s*['"]?pnpm\/action-setup@/);
      if (!uses) continue;
      const indent = uses[1].length - (uses[2] ? 0 : 2);
      let end = i + 1;
      while (end < lines.length) {
        if (lines[end].trim() && !lines[end].trim().startsWith('#') && lines[end].search(/\S/) <= indent) break;
        end++;
      }
      const block = lines.slice(i, end).join('\n');
      const packageFile = unquote(block.match(/^\s*package_json_file:\s*([^\n#]+)/m)?.[1] ?? 'package.json');
      const path = resolve(root, packageFile);
      if (relative(root, path).startsWith('..')) fail(`${file}: package_json_file must stay inside the repository`);
      let metadata;
      try {
        metadata = JSON.parse(readFileSync(path, 'utf8'));
      } catch (error) {
        fail(`${file}:${i + 1}: pnpm/action-setup reads ${packageFile}: ${error.message}`);
      }
      const pinned = metadata.packageManager?.match(/^pnpm@(\d+\.\d+\.\d+)/)?.[1] ?? metadata.devEngines?.packageManager?.version;
      const explicit = block.match(/^\s*version:\s*([^\n#]+)/m)?.[1];
      if (!pinned) fail(`${file}:${i + 1}: ${packageFile} must declare a pinned pnpm version`);
      if (explicit && unquote(explicit) !== pinned) fail(`${file}:${i + 1}: action version conflicts with ${packageFile}`);
      steps++;
    }
  }
  if (!steps) fail('No pnpm/action-setup steps found; workflow layout changed');
  console.log(`CI config: Node ${expectedNode}, ${actualGo}, pnpm ${actualPnpm}; ${steps} setup steps checked`);
} catch (error) {
  console.error(`CI config guard: ${error.message}`);
  process.exitCode = 1;
}
