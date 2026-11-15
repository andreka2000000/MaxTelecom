import { readFileSync } from 'node:fs';
import { renderToString } from 'react-dom/server';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { ExecutionPanel } from './execution';
import type { Snapshot, VisitFact } from './types';

const { workspace } = vi.hoisted(() => ({
  workspace: { data: null as Snapshot | null, busy: false, refresh: vi.fn() },
}));
vi.mock('./context', () => ({ useWorkspace: () => workspace }));
const fixture = (name: string) =>
  JSON.parse(readFileSync(new URL(`./__fixtures__/${name}.json`, import.meta.url), 'utf8'));

function snapshot(): Snapshot {
  const day = fixture('day');
  day.orders = [day.orders[0]];
  day.now = '2026-10-26T09:18:00+05:00';
  return {
    day,
    reference: fixture('reference'),
    generation: 1,
    event_count: 0,
    events: [],
    facts: [],
    contacts: [],
    plan: {
      date: day.date,
      as_of: day.now,
      plan_version: 1,
      routes: [
        {
          staff_id: 'ST-001',
          service_center_id: 'SC-01',
          status: 'ACTIVE',
          stops: [
            {
              seq: 1,
              type: 'VISIT',
              location_id: day.orders[0].building_id,
              order_id: day.orders[0].order_id,
              arrival: '2026-10-26T09:18:00+05:00',
              start: '2026-10-26T09:18:00+05:00',
              end: '2026-10-26T10:18:00+05:00',
              travel_sec: 1080,
            },
          ],
        },
      ],
      unassigned: [],
      pgp_groups: [],
      cancelled: [],
      alerts: [],
    },
  };
}
function render() {
  const client = new QueryClient();
  return renderToString(
    <QueryClientProvider client={client}>
      <ExecutionPanel onMap={() => {}} />
    </QueryClientProvider>,
  );
}
function fact(status: VisitFact['status']): VisitFact {
  return {
    order_id: workspace.data!.day.orders[0].order_id,
    staff_id: 'ST-001',
    status,
    departed_at: '2026-10-26T09:00:00+05:00',
    ...(status !== 'EN_ROUTE' ? { arrived_at: '2026-10-26T09:10:00+05:00' } : {}),
    ...(status === 'IN_PROGRESS' || status === 'COMPLETED'
      ? { started_at: '2026-10-26T09:18:00+05:00' }
      : {}),
    ...(status === 'COMPLETED' ? { completed_at: '2026-10-26T10:18:00+05:00' } : {}),
    report: status === 'COMPLETED' ? 'Связь восстановлена' : '',
  };
}

describe('technician execution interface', () => {
  beforeEach(() => {
    workspace.data = snapshot();
    workspace.busy = false;
  });
  it.each([
    [undefined, 'Выехал'],
    ['EN_ROUTE', 'Прибыл'],
    ['ARRIVED', 'Начал работу'],
    ['IN_PROGRESS', 'Закончил работу'],
  ] as const)('shows only the next action after %s', (status, label) => {
    workspace.data!.facts = status ? [fact(status)] : [];
    const html = render();
    expect(html).toContain(`${label}</button>`);
    expect(html).not.toContain('class="primary-button" disabled');
    expect(html.includes('name="report"')).toBe(status === 'IN_PROGRESS');
  });
  it('gates departure by planned travel start and work by planned visit start', () => {
    workspace.data!.day.now = '2026-10-26T08:59:00+05:00';
    expect(render()).toContain('class="primary-button" disabled');
    workspace.data!.day.now = '2026-10-26T09:00:00+05:00';
    expect(render()).not.toContain('class="primary-button" disabled');
    workspace.data!.facts = [fact('ARRIVED')];
    expect(render()).toContain('class="primary-button" disabled');
    workspace.data!.day.now = '2026-10-26T09:18:00+05:00';
    expect(render()).not.toContain('class="primary-button" disabled');
  });
  it('shows completion and handles legacy work facts without inventing journey times', () => {
    const completed = fact('COMPLETED');
    delete completed.departed_at;
    delete completed.arrived_at;
    workspace.data!.facts = [completed];
    const html = render();
    expect(html).toContain('Связь восстановлена');
    expect(html).not.toContain('class="primary-button"');
    expect(html).not.toContain('<dt>Выехал</dt>');
    expect(html).not.toContain('<dt>Прибыл</dt>');
  });
  it('blocks a later departure until the preceding visit is confirmed complete', () => {
    const d = workspace.data!;
    d.day.orders.push({ ...d.day.orders[0], order_id: 'ORD-next' });
    d.plan!.routes[0].stops.push({
      ...d.plan!.routes[0].stops[0],
      seq: 2,
      order_id: 'ORD-next',
      arrival: '2026-10-26T11:00:00+05:00',
      start: '2026-10-26T11:00:00+05:00',
      end: '2026-10-26T12:00:00+05:00',
    });
    d.day.now = '2026-10-26T11:00:00+05:00';
    const buttons = render().match(/<button class="primary-button"[^>]*>/g)!;
    expect(buttons).toHaveLength(2);
    expect(buttons[0]).not.toContain('disabled');
    expect(buttons[1]).toContain('disabled');
    d.facts = [fact('COMPLETED')];
    expect(render()).not.toContain('class="primary-button" disabled');
  });
  it('provides authorized customer contact and a coordinate-only navigator link', () => {
    workspace.data!.contacts = [
      {
        order_id: workspace.data!.day.orders[0].order_id,
        name: 'Анна',
        phone: '+7 (999) 123-45-67',
        description: 'Позвоните у подъезда',
      },
    ];
    const html = render();
    expect(html).toContain('tel:+79991234567');
    expect(html).toContain('Позвоните у подъезда');
    expect(html).toContain('destination=56.8712%2C60.6531');
    expect(html).toContain('rel="noopener noreferrer"');
    expect(html).not.toContain('Контакт абонента не указан');
  });
  it('does not fabricate contacts for an imported order', () => {
    const html = render();
    expect(html).toContain('Контакт абонента не указан');
    expect(html).not.toContain('tel:');
  });
});
