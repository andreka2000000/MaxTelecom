// Keeps the original magistral database untouched. Only this named training
// database may be replaced, and repeat loads require an explicit --replace.
import { spawn } from 'node:child_process';
import { readFile, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const metadata = JSON.parse(
  await readFile(path.join(root, 'data/synthetic/manifest.json'), 'utf8'),
);
if (metadata.synthetic !== true) throw new Error('Expected the generated synthetic dataset.');
const settings = {
  ...process.env,
  API_DATABASE: 'magistral_training',
  FRONTEND_PORT: process.env.FRONTEND_PORT || '3001',
};
function docker(args, capture = false) {
  return new Promise((resolve, reject) => {
    const child = spawn('docker', args, {
      cwd: root,
      env: settings,
      stdio: capture ? ['ignore', 'pipe', 'inherit'] : 'inherit',
      shell: false,
    });
    let out = '';
    if (capture)
      child.stdout.on('data', (chunk) => {
        out += chunk;
      });
    child.on('error', reject);
    child.on('exit', (code) =>
      code === 0 ? resolve(out.trim()) : reject(new Error(`docker exited ${code}`)),
    );
  });
}
await docker(['compose', 'build', 'api', 'optimizer']);
await docker([
  'compose',
  'run',
  '--rm',
  '--no-deps',
  '--entrypoint',
  '/app/importer',
  'api',
  '-date',
  metadata.date,
  '-now',
  metadata.now,
  '-validate-only',
]);
await docker(['compose', 'up', '-d', '--wait', 'postgres']);
const existing = await docker(
  [
    'compose',
    'exec',
    '-T',
    'postgres',
    'psql',
    '-U',
    'magistral',
    '-d',
    'postgres',
    '-tAc',
    "SELECT 1 FROM pg_database WHERE datname='magistral_training'",
  ],
  true,
);
if (existing === '1' && !process.argv.includes('--replace'))
  throw new Error(
    'Training database already exists. To replace only its contents, use npm run demo:load -- --replace. The original database is untouched.',
  );
if (existing !== '1')
  await docker([
    'compose',
    'exec',
    '-T',
    'postgres',
    'psql',
    '-U',
    'magistral',
    '-d',
    'postgres',
    '-c',
    'CREATE DATABASE magistral_training',
  ]);
const configFile = path.join(root, '.env');
let config = await readFile(configFile, 'utf8').catch((e) => {
  if (e.code !== 'ENOENT') throw e;
  return '';
});
for (const [key, value] of Object.entries({
  API_DATABASE: 'magistral_training',
  FRONTEND_PORT: settings.FRONTEND_PORT,
})) {
  const expression = new RegExp(`^${key}=.*$`, 'm');
  config = expression.test(config)
    ? config.replace(expression, `${key}=${value}`)
    : config.trimEnd() + `\n${key}=${value}\n`;
}
await writeFile(configFile, config.trimStart());
await docker(['compose', 'up', '-d', '--wait', 'optimizer', 'api', 'frontend']);
await docker([
  'compose',
  'exec',
  '-T',
  'api',
  '/app/importer',
  '-date',
  metadata.date,
  '-now',
  metadata.now,
  '-replace',
]);
console.log('Building the first full-size training plan…');
// The anonymous organizer protocol is reachable only from the Docker network.
const plan = JSON.parse(
  await docker(
    [
      'compose',
      'exec',
      '-T',
      'api',
      'wget',
      '-q',
      '-O',
      '-',
      '-T',
      '330',
      '--header',
      'Content-Type: application/json',
      '--post-data',
      JSON.stringify({ time_limit_sec: 30 }),
      `http://127.0.0.1:8081/api/v1/days/${metadata.date}/plan`,
    ],
    true,
  ),
);
console.log(
  JSON.stringify(
    {
      frontend: `http://127.0.0.1:${settings.FRONTEND_PORT}/app/dispatch`,
      database: 'magistral_training',
      original_database: 'magistral (preserved)',
      date: plan.date,
      version: plan.plan_version,
      summary: plan.summary,
      optimization: plan.optimization,
    },
    null,
    2,
  ),
);
