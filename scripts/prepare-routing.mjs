import { mkdir, readFile, writeFile, stat } from 'node:fs/promises';
import { spawn } from 'node:child_process';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const directory = path.join(root, 'work', 'routing');
await mkdir(directory, { recursive: true });
const filename = path.join(directory, 'ekaterinburg.osm');
const bbox = '56.65,60.35,57.05,60.95';
const endpoint = 'https://overpass-api.de/api/interpreter';
const query = `[out:xml][timeout:180][maxsize:268435456];(way["highway"](${bbox});relation["type"="restriction"](${bbox}););(._;>;);out body;`;
if (!(await stat(filename).catch(() => null))) {
  console.log('Downloading the Yekaterinburg road graph from OpenStreetMap / Overpass…');
  const response = await fetch(endpoint, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/x-www-form-urlencoded',
      'User-Agent': 'MaxTelecom-hackathon/0.6 (local OSM routing setup)',
    },
    body: new URLSearchParams({ data: query }),
    signal: AbortSignal.timeout(240_000),
  });
  if (!response.ok)
    throw new Error(`Overpass returned HTTP ${response.status}. Retry setup later.`);
  const xml = await response.text();
  if (
    !xml.includes('<osm') ||
    !xml.includes('<way') ||
    xml.includes('<remark>') ||
    !xml.trimEnd().endsWith('</osm>')
  )
    throw new Error('Incomplete road extract. No graph was saved; retry later.');
  await writeFile(filename, xml);
  await writeFile(
    path.join(directory, 'source.json'),
    JSON.stringify(
      {
        source: 'OpenStreetMap contributors',
        license: 'ODbL-1.0',
        endpoint,
        bbox,
        downloaded_at: new Date().toISOString(),
        bytes: Buffer.byteLength(xml),
      },
      null,
      2,
    ),
  );
  console.log(`Road extract saved: ${(Buffer.byteLength(xml) / 1024 / 1024).toFixed(1)} MiB`);
} else {
  console.log('Using the existing local OSM extract.');
}
function command(args) {
  return new Promise((resolve, reject) => {
    const child = spawn('docker', args, { cwd: root, stdio: 'inherit', shell: false });
    child.on('error', reject);
    child.on('exit', (code) =>
      code === 0 ? resolve() : reject(new Error(`docker exited ${code}`)),
    );
  });
}
await command(['compose', '--profile', 'routing', 'up', '-d', 'osrm']);
const target = process.env.OSRM_SETUP_URL || 'http://127.0.0.1:5000';
for (let attempt = 0; attempt < 30; attempt++) {
  try {
    const result = await fetch(`${target}/nearest/v1/driving/60.61,56.89`, {
      signal: AbortSignal.timeout(2000),
    });
    if (result.ok && (await result.json()).code === 'Ok') {
      const source = JSON.parse(await readFile(path.join(directory, 'source.json'), 'utf8'));
      console.log(JSON.stringify({ status: 'ready', osrm: target, source }, null, 2));
      process.exit(0);
    }
  } catch {
    /* The routing process may still be opening its graph. */
  }
  await new Promise((resolve) => setTimeout(resolve, 2000));
}
throw new Error('OSRM did not become ready. Inspect docker compose logs osrm.');
