// Explicit local demo accounts; credentials never enter Git or command arguments.
import { randomBytes } from 'node:crypto';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { spawn } from 'node:child_process';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const file = path.join(root, 'work', 'demo-accounts.json');
await mkdir(path.dirname(file), { recursive: true });
let accounts;
try {
  accounts = JSON.parse(await readFile(file, 'utf8'));
} catch (error) {
  if (error.code !== 'ENOENT') throw error;
  accounts = [
    { username: 'dispatcher', name: 'Диспетчер МаксТелеком', role: 'DISPATCHER', staff_id: '' },
    {
      username: 'technician',
      name: 'Исполнитель МаксТелеком',
      role: 'TECHNICIAN',
      staff_id: 'ST-001',
    },
    { username: 'subscriber', name: 'Александр', role: 'SUBSCRIBER', staff_id: '' },
  ].map((account) => ({ ...account, password: 'Mt!' + randomBytes(18).toString('base64url') }));
  await writeFile(file, JSON.stringify(accounts, null, 2) + '\n', { mode: 0o600, flag: 'wx' });
}
await new Promise((resolve, reject) => {
  const child = spawn('docker', ['compose', 'exec', '-T', 'api', '/app/accounts'], {
    cwd: root,
    stdio: ['pipe', 'inherit', 'inherit'],
    shell: false,
  });
  child.on('error', reject);
  child.on('exit', (code) =>
    code === 0 ? resolve() : reject(new Error(`Account bootstrap exited ${code}`)),
  );
  child.stdin.end(JSON.stringify(accounts));
});
console.log(`Local account details: ${file}`);
console.log('Existing accounts and passwords are preserved. Login: /login');
