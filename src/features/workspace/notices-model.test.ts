import { readFileSync } from 'node:fs';
import { describe, it, expect } from 'vitest';
import { shiftNotices, noticeScope } from './notices-model';
import type { Snapshot } from './types';
const fixture = (name: string) =>
  JSON.parse(readFileSync(new URL(`./__fixtures__/${name}.json`, import.meta.url), 'utf8'));
const data = (): Snapshot => ({
  day: fixture('day'),
  reference: fixture('reference'),
  plan: null,
  generation: 1,
  event_count: 0,
  events: [],
});
describe('shift notifications', () => {
  it('records each actual execution milestone, preserves report, and omits unrecorded legacy travel', () => {
    const d = data();
    d.facts = [
      {
        order_id: 'one',
        staff_id: 'staff',
        status: 'COMPLETED',
        started_at: '2026-10-26T11:00:00+05:00',
        completed_at: '2026-10-26T12:00:00+05:00',
        report: 'Связь восстановлена',
      },
    ];
    const n = shiftNotices(d);
    expect(n).toHaveLength(2);
    expect(n[0].title).toBe('Работа завершена');
    expect(n[0].detail).toContain('Связь восстановлена');
    expect(n[1].title).toBe('Работа начата');
  });
  it('omits automatic ticks but keeps meaningful events and closed alerts', () => {
    const d = data();
    d.events = [
      { event_id: 'clock', type: 'TIME_TICK', timestamp: '2026-10-26T11:00:00+05:00' },
      {
        event_id: 'outage',
        type: 'STAFF_UNAVAILABLE',
        timestamp: '2026-10-26T12:00:00+05:00',
        staff_id: 's',
        reason: 'ILLNESS',
        until: null,
      },
    ];
    expect(shiftNotices(d).map((n) => n.id)).toEqual(['event:outage']);
  });
  it('isolates read state by account, shift and generation', () => {
    const d = data();
    expect(noticeScope('a', d)).not.toBe(noticeScope('b', d));
    expect(noticeScope('a', d)).not.toBe(noticeScope('a', { ...d, generation: 2 }));
    expect(noticeScope('a', d)).not.toBe(
      noticeScope('a', { ...d, day: { ...d.day, date: '2026-10-27' } }),
    );
  });
});
