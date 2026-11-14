// Deterministic training data, not the organizers' hidden evaluation dataset.
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const output = path.join(root, 'data', 'synthetic');
const date = '2026-10-28';
const now = `${date}T08:30:00+05:00`;
const osrm = process.env.OSRM_SETUP_URL || 'http://127.0.0.1:5000';
let seed = 20261028;
const random = () => {
  seed = (Math.imul(seed, 1664525) + 1013904223) >>> 0;
  return seed / 2 ** 32;
};
const id = (n) => String(n).padStart(3, '0');
const iso = (value) =>
  new Date(Date.parse(value) + 5 * 3600_000).toISOString().slice(0, 19) + '+05:00';
const at = (hour, minute = 0) =>
  `${date}T${String(hour).padStart(2, '0')}:${String(minute).padStart(2, '0')}:00+05:00`;
const technologies = ['PON', 'FTTB', 'XDSL', 'DOCSIS'];
const centers = [
  ['Север', 60.6105, 56.8941],
  ['Запад', 60.565, 56.844],
  ['Восток', 60.67, 56.846],
  ['Юг', 60.605, 56.79],
  ['Центр', 60.611, 56.839],
].map(([name, lon, lat], i) => ({
  service_center_id: `SC-0${i + 1}`,
  name: `Учебный СЦ «${name}»`,
  address: `Екатеринбург, учебная точка СЦ №${i + 1} (синтетическая)`,
  lat,
  lon,
  opening_hours: { open: '08:00', close: '21:00' },
  restock_duration_min: 15,
}));
const staff = Array.from({ length: 40 }, (_, i) => {
  const late = i % 3 === 0;
  return {
    staff_id: `ST-${id(i + 1)}`,
    display_name: `${i < 28 ? 'Специалист ЛТП' : 'Инсталлятор'} ${id(i + 1)}`,
    service_center_id: centers[i % 5].service_center_id,
    profiles: [i < 28 ? 'LTP3' : 'INSTALL'],
    technologies:
      i % 4 === 0 ? [...technologies] : [technologies[i % 4], technologies[(i + 1) % 4]],
    height_permit: i % 3 !== 0,
    shift: {
      start: late ? '12:00' : '09:00',
      end: late ? '21:00' : '18:00',
      break_duration_min: 60,
      break_window: { from: late ? '15:00' : '12:00', to: late ? '18:00' : '15:00' },
    },
    equipment_start: { ONT: 1 + (i % 3), ROUTER: 1 + (i % 4), STB: i % 3 },
    equipment_capacity: { ONT: 6, ROUTER: 6, STB: 4 },
    experience_years: 1 + (i % 12),
  };
});
const nodes = Array.from({ length: 50 }, (_, i) => {
  const c = centers[i % 5],
    technology = technologies[i % 4];
  const lon = +(c.lon + (random() - 0.5) * 0.045).toFixed(7),
    lat = +(c.lat + (random() - 0.5) * 0.025).toFixed(7);
  return {
    access_node_id: `AN-${String(i + 1).padStart(4, '0')}`,
    node_type: ['OLT', 'SWITCH', 'DSLAM', 'CMTS'][i % 4],
    technology,
    name: `Учебный узел ${id(i + 1)} · ${technology}`,
    lat,
    lon,
    buildings: Array.from({ length: 2 }, (_, j) => ({
      building_id: `B-${String(i * 2 + j + 1).padStart(5, '0')}`,
      address: `Екатеринбург, учебный адрес №${id(i * 2 + j + 1)} (синтетический)`,
      lat: +(lat + (random() - 0.5) * 0.006).toFixed(7),
      lon: +(lon + (random() - 0.5) * 0.009).toFixed(7),
      building_type: i % 5 === 0 && j === 1 ? 'PRIVATE' : 'MKD',
      overhead_line: i % 5 === 0 && j === 1,
      zone_service_center_id: c.service_center_id,
    })),
  };
});
const points = [...centers, ...nodes.flatMap((n) => n.buildings)];
const coordinates = points.map((p) => `${p.lon},${p.lat}`).join(';');
console.log(
  `Calculating a directed ${points.length} × ${points.length} road matrix in local OSRM…`,
);
const response = await fetch(
  `${osrm}/table/v1/driving/${coordinates}?annotations=duration,distance&generate_hints=false`,
  { signal: AbortSignal.timeout(60_000) },
);
const table = await response.json();
if (!response.ok || table.code !== 'Ok' || table.sources?.length !== points.length)
  throw new Error('OSRM matrix unavailable. Run npm run routing:prepare first.');
for (const rows of [table.durations, table.distances]) {
  if (
    rows?.length !== points.length ||
    rows.some(
      (r) => r.length !== points.length || r.some((v) => v == null || !Number.isFinite(v) || v < 0),
    )
  )
    throw new Error('The road graph has disconnected or invalid training points.');
}
// Training locations are deliberately on routable streets, not claimed to be
// surveyed subscriber buildings. This keeps the synthetic matrix reproducible.
points.forEach((p, i) => {
  p.lon = table.sources[i].location[0];
  p.lat = table.sources[i].location[1];
});
nodes.forEach((n) => {
  n.lat = n.buildings[0].lat;
  n.lon = n.buildings[0].lon;
});
const work = (code, name, profiles, duration, delay, kind, value, repeat, pgp = false) => ({
  code,
  name,
  profiles,
  duration_min: duration,
  max_start_delay_min: delay,
  deadline_kind: kind,
  deadline_value: value,
  repeat_deadline_value: repeat,
  pgp_relevant: pgp,
});
const works = [
  work('REPAIR', 'Устранение неисправности', ['LTP3'], 60, null, 'HOURS', 24, 12, true),
  work('DEGRADATION', 'Устранение деградации связи', ['LTP3'], 45, null, 'HOURS', 24, null),
  work(
    'INST_PON_MKD',
    'Инсталляция ШПД/IPTV, МКД, PON',
    ['INSTALL'],
    90,
    10,
    'WORKING_DAYS',
    3,
    null,
  ),
  work(
    'INST_FTTB_MKD',
    'Инсталляция ШПД/IPTV, МКД, FTTB',
    ['INSTALL'],
    80,
    10,
    'WORKING_DAYS',
    3,
    null,
  ),
  work(
    'INST_PRIVATE_VLS',
    'Инсталляция в частном секторе с ВЛС',
    ['INSTALL'],
    120,
    10,
    'WORKING_DAYS',
    3,
    null,
  ),
  work('ADD_SERVICE', 'Доподключение услуги', ['INSTALL'], 45, null, 'HOURS', 48, null),
  work('SURVEY_PRIVATE', 'Обследование частного сектора', ['INSTALL'], 45, null, 'HOURS', 48, null),
  work('DISMANTLE', 'Демонтаж оборудования', ['LTP3'], 30, null, 'HOURS', 48, null),
];
const byWork = Object.fromEntries(works.map((w) => [w.code, w]));
const buildings = nodes.flatMap((n) => n.buildings.map((b) => ({ node: n, building: b })));
const counters = new Map();
const orders = Array.from({ length: 400 }, (_, i) => {
  const { node: n, building: b } = buildings[(i * 37) % 100];
  let code =
    i % 10 < 6
      ? 'REPAIR'
      : i % 10 === 6
        ? 'DEGRADATION'
        : i % 10 === 7
          ? 'DISMANTLE'
          : b.building_type === 'PRIVATE'
            ? 'INST_PRIVATE_VLS'
            : n.technology === 'PON'
              ? 'INST_PON_MKD'
              : n.technology === 'FTTB'
                ? 'INST_FTTB_MKD'
                : 'ADD_SERVICE';
  if (b.building_type === 'PRIVATE' && i % 13 === 0) code = 'SURVEY_PRIVATE';
  const type = byWork[code],
    repeat = code === 'REPAIR' && i % 17 === 0;
  const counter = counters.get(n.access_node_id) || 0;
  counters.set(n.access_node_id, counter + 1);
  const registered = repeat
    ? at(8)
    : iso(new Date(Date.parse('2026-10-27T22:00:00+05:00') + counter * 65 * 60_000).toISOString());
  const start = at(9 + 2 * (i % 5)),
    end = iso(new Date(Date.parse(start) + 2 * 3600_000).toISOString());
  const latest =
    type.max_start_delay_min == null
      ? end
      : iso(new Date(Date.parse(start) + type.max_start_delay_min * 60_000).toISOString());
  const deadline =
    type.deadline_kind === 'WORKING_DAYS'
      ? '2026-10-30T21:00:00+05:00'
      : iso(
          new Date(
            Date.parse(registered) +
              (repeat ? type.repeat_deadline_value : type.deadline_value) * 3600_000,
          ).toISOString(),
        );
  return {
    order_id: `ORD-20261028-${String(i + 1).padStart(4, '0')}`,
    work_type_code: code,
    technology: n.technology,
    building_id: b.building_id,
    address: b.address,
    lat: b.lat,
    lon: b.lon,
    building_type: b.building_type,
    requires_height: b.overhead_line,
    access_node_id: n.access_node_id,
    registered_at: registered,
    slot: { start, end },
    latest_start: latest,
    deadline,
    required_equipment: {
      ONT: n.technology === 'PON' && i % 3 === 0 ? 1 : 0,
      ROUTER: i % 4 === 0 ? 1 : 0,
      STB: code.startsWith('INST') && i % 2 === 0 ? 1 : 0,
    },
    is_repeat: repeat,
    previous_order_id: repeat ? `ORD-20261020-${String(i + 1).padStart(4, '0')}` : null,
  };
});
// Three explicit, documented repair reports form a controlled PGP incident.
for (let i = 0; i < 3; i++) {
  const n = nodes[0],
    b = n.buildings[i % 2];
  Object.assign(orders[i], {
    work_type_code: 'REPAIR',
    technology: n.technology,
    building_id: b.building_id,
    address: b.address,
    lat: b.lat,
    lon: b.lon,
    building_type: b.building_type,
    requires_height: b.overhead_line,
    access_node_id: n.access_node_id,
    registered_at: at(7, 30 + i * 10),
    slot: { start: at(9), end: at(11) },
    latest_start: at(11),
    deadline: iso(new Date(Date.parse(at(7, 30 + i * 10)) + 24 * 3600_000).toISOString()),
    is_repeat: false,
    previous_order_id: null,
  });
}
for (const o of orders) {
  if (
    Date.parse(o.registered_at) > Date.parse(now) ||
    Date.parse(o.latest_start) + byWork[o.work_type_code].duration_min * 60_000 >
      Date.parse(o.deadline)
  )
    throw new Error(`Invalid generated time window: ${o.order_id}`);
}
const matrix = {
  matrix_id: 'synthetic-ekb-osrm-20261028-v1',
  points: points.map((p) => p.service_center_id || p.building_id),
  durations_sec: table.durations.map((r, i) => r.map((v, j) => (i === j ? 0 : Math.ceil(v)))),
  distances_m: table.distances.map((r, i) => r.map((v, j) => (i === j ? 0 : Math.ceil(v)))),
  hourly_factors: [],
};
const newOrder = {
  ...orders[20],
  order_id: 'ORD-20261028-0401',
  registered_at: at(9, 45),
  slot: { start: at(14), end: at(16) },
  latest_start: at(16),
  work_type_code: 'REPAIR',
  deadline: '2026-10-29T09:45:00+05:00',
  is_repeat: false,
  previous_order_id: null,
};
const cancellation = orders.find(
  (o, i) => i > 300 && Date.parse(o.slot.start) >= Date.parse(at(13)),
);
const triggerOrders = orders.filter((o, i) => i > 2 && o.work_type_code === 'REPAIR').slice(0, 2);
const scenario = {
  date,
  now,
  events: [
    { event_id: 'synthetic-new-001', type: 'NEW_ORDER', timestamp: at(9, 45), order: newOrder },
    {
      event_id: 'synthetic-cancel-001',
      type: 'ORDER_CANCELLED',
      timestamp: at(10, 15),
      order_id: cancellation.order_id,
      reason: 'CUSTOMER_REQUEST',
    },
    { event_id: 'synthetic-tick-001', type: 'TIME_TICK', timestamp: at(11) },
    {
      event_id: 'synthetic-unavailable-001',
      type: 'STAFF_UNAVAILABLE',
      timestamp: at(16),
      staff_id: 'ST-040',
      reason: 'OTHER',
      until: at(17),
    },
  ],
  triggers: [
    {
      trigger_id: 'synthetic-delay-001',
      type: 'VISIT_DELAYED',
      order_id: triggerOrders[0].order_id,
      fire_after_start_min: 5,
      extra_duration_min: 20,
    },
    {
      trigger_id: 'synthetic-transfer-001',
      type: 'ORDER_TRANSFERRED',
      order_id: triggerOrders[1].order_id,
      fire_after_start_min: 10,
      transferred_to: 'STATION_EQUIPMENT',
    },
  ],
};
const csvColumns = [
  'code',
  'name',
  'profiles',
  'duration_min',
  'max_start_delay_min',
  'deadline_kind',
  'deadline_value',
  'repeat_deadline_value',
  'pgp_relevant',
];
const csvCell = (v) => {
  const s = v == null ? '' : Array.isArray(v) ? v.join('|') : String(v);
  return /[",\r\n]/.test(s) ? '"' + s.replaceAll('"', '""') + '"' : s;
};
const csv =
  [csvColumns.join(','), ...works.map((w) => csvColumns.map((k) => csvCell(w[k])).join(','))].join(
    '\r\n',
  ) + '\r\n';
await mkdir(output, { recursive: true });
const files = {
  'service_centers.json': centers,
  'staff.json': staff,
  'access_nodes.json': nodes,
  'travel_matrix.json': matrix,
  'orders_train.json': orders,
  'events_example.json': scenario,
};
for (const [file, data] of Object.entries(files))
  await writeFile(path.join(output, file), JSON.stringify(data, null, 2) + '\n');
await writeFile(path.join(output, 'work_types.csv'), csv);
const graph = JSON.parse(await readFile(path.join(root, 'work', 'routing', 'source.json'), 'utf8'));
await writeFile(
  path.join(output, 'manifest.json'),
  JSON.stringify(
    {
      synthetic: true,
      seed: 20261028,
      date,
      now,
      counts: {
        staff: 40,
        ltp3: 28,
        install: 12,
        service_centers: 5,
        access_nodes: 50,
        buildings: 100,
        orders: 400,
        work_types: works.length,
      },
      routing: { provider: 'OSRM', traffic: 'unavailable', graph },
      notes: [
        'Учебные адреса и сотрудники синтетические, координаты привязаны к дорожной сети.',
        'Нормативы демонстрационные; при получении официального CSV заменить его.',
        '3 первых ремонта образуют контрольный ПГП.',
        'Триггеры событий разворачиваются по фактически построенному плану.',
        'Набор не гарантирует назначение всех 400 нарядов: ограничения обязательны.',
      ],
    },
    null,
    2,
  ) + '\n',
);
console.log(
  `Generated ${output}: 40 staff, 5 centers, 400 orders, directed road matrix. No database changes.`,
);
