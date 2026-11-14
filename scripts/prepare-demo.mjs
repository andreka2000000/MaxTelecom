// Prepare a new dated scenario; never replace a shift or reset account passwords.
import { randomBytes } from 'node:crypto';
import { readFile, writeFile, mkdir, rename } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { setTimeout as delay } from 'node:timers/promises';
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const args = process.argv.slice(2);
const option = (name) => (args.includes(name) ? args[args.indexOf(name) + 1] : undefined);
const date =
  option('--date') ||
  new Intl.DateTimeFormat('sv-SE', {
    timeZone: 'Asia/Yekaterinburg',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  }).format(new Date());
if (
  !/^\d{4}-\d{2}-\d{2}$/.test(date) ||
  new Date(date + 'T00:00:00Z').toISOString().slice(0, 10) !== date
)
  throw new Error('Use --date YYYY-MM-DD.');
const base = 'http://127.0.0.1:' + (option('--port') || '3001');
const accountFile = path.join(root, 'work/demo-accounts.json');
const accounts = JSON.parse(await readFile(accountFile, 'utf8'));
// Recover an already-created account if Windows briefly locked the final file
// during a previous checkpoint. Passwords are never regenerated for that user.
try {
  const pending = JSON.parse(await readFile(accountFile + '.tmp', 'utf8'));
  for (const account of pending)
    if (!accounts.some((a) => a.username === account.username)) accounts.push(account);
} catch (error) {
  if (error.code !== 'ENOENT') throw error;
}
async function saveAccounts() {
  const content = JSON.stringify(accounts, null, 2) + '\n';
  await writeFile(accountFile + '.tmp', content, { mode: 0o600 });
  for (let attempt = 0; attempt < 6; attempt++) {
    try {
      await rename(accountFile + '.tmp', accountFile);
      return;
    } catch (error) {
      if (!['EPERM', 'EACCES', 'EBUSY'].includes(error.code)) throw error;
      await delay(100 * (attempt + 1));
    }
  }
  await writeFile(accountFile, content, { mode: 0o600 });
}
const dispatcher = accounts.find((a) => a.role === 'DISPATCHER');
if (!dispatcher) throw new Error('Run npm run auth:bootstrap first.');
let cookie;
async function api(url, method = 'GET', body) {
  const response = await fetch(base + url, {
    method,
    headers: {
      Origin: base,
      ...(cookie ? { Cookie: cookie } : {}),
      ...(body ? { 'Content-Type': 'application/json' } : {}),
    },
    body: body ? JSON.stringify(body) : undefined,
    signal: AbortSignal.timeout(120000),
  });
  if (!response.ok) {
    const e = await response.json();
    throw new Error(`${method} ${url}: ${response.status} ${e.error?.message || ''}`);
  }
  return response;
}
const login = await api('/auth/v1/session', 'POST', {
  username: dispatcher.username,
  password: dispatcher.password,
});
cookie = login.headers.get('set-cookie')?.split(';')[0];
if (!cookie) throw new Error('Session was not created.');
try {
  const days = await (await api('/ui/v1/days')).json();
  if (!days.some((d) => d.date === date)) {
    const source = option('--source') || days.find((d) => d.orders_total === 400)?.date;
    if (!source) throw new Error('No 400-order source shift found. Specify --source YYYY-MM-DD.');
    await api(`/ui/v1/days/${source}/copy`, 'POST', { date });
  }
  let snapshot = await (await api(`/ui/v1/days/${date}/workspace`)).json();
  if (!snapshot.plan) await api(`/api/v1/days/${date}/plan`, 'POST', { time_limit_sec: 30 });
  let created = 0;
  if (args.includes('--accounts')) {
    await saveAccounts();
    const manifest = JSON.parse(
      await readFile(path.join(root, 'data/synthetic/manifest.json'), 'utf8'),
    );
    if (manifest.synthetic !== true || snapshot.reference.staff.length !== 40)
      throw new Error('Bulk demo accounts require the synthetic 40-staff dataset.');
    const users = await (await api('/ui/v1/users')).json();
    for (const staff of snapshot.reference.staff) {
      if (users.some((u) => u.staff_id === staff.staff_id && !u.disabled)) continue;
      const account = {
        username: 'demo.' + staff.staff_id.toLowerCase(),
        name: staff.display_name || staff.staff_id,
        role: 'TECHNICIAN',
        staff_id: staff.staff_id,
        password: 'Mt!' + randomBytes(18).toString('base64url'),
      };
      await api('/ui/v1/users', 'POST', account);
      accounts.push(account);
      created++;
      await saveAccounts();
    }
  }
  snapshot = await (await api(`/ui/v1/days/${date}/workspace`)).json();
  const users = await (await api('/ui/v1/users')).json();
  const report = {
    date,
    timezone: 'Asia/Yekaterinburg',
    model_now: snapshot.day.now,
    staff: snapshot.reference.staff.length,
    orders: snapshot.day.orders.length,
    summary: snapshot.plan?.summary,
    optimization: snapshot.plan?.optimization,
    accounts_created: created,
    active_technician_accounts: users.filter((u) => u.role === 'TECHNICIAN' && !u.disabled).length,
  };
  await mkdir(path.join(root, 'work'), { recursive: true });
  await writeFile(
    path.join(root, 'work/demo-preparation.json'),
    JSON.stringify(report, null, 2) + '\n',
  );
  console.log(JSON.stringify(report, null, 2));
  console.log(
    'Existing shifts and passwords are preserved. Local account details: work/demo-accounts.json',
  );
} finally {
  await api('/auth/v1/session', 'DELETE');
}
