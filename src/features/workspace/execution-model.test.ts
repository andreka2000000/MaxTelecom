import { describe, expect, it } from 'vitest';
import { nextVisit, visitQueue } from './execution-model';
import type { ViewOrder } from './model';

function order(id: string, start: string, status = 'Назначена'): ViewOrder {
  return {
    source: { order_id: id } as ViewOrder['source'],
    stop: { start } as ViewOrder['stop'],
    status,
    tone: 'blue',
    title: id,
  };
}
describe('technician work queue', () => {
  it('keeps an overdue unfinished visit visible and orders visits by schedule', () => {
    const later = order('later', '2026-10-28T14:00:00+05:00');
    const overdue = order('overdue', '2026-10-28T09:00:00+05:00', 'Ожидает подтверждения');
    expect(visitQueue([later, overdue]).map((o) => o.source.order_id)).toEqual([
      'overdue',
      'later',
    ]);
    expect(nextVisit([later, overdue])?.source.order_id).toBe('overdue');
  });
  it('prioritizes actual work and advances after confirmed completion', () => {
    const earlier = order('earlier', '2026-10-28T09:00:00+05:00');
    const active = order('active', '2026-10-28T14:00:00+05:00', 'На объекте');
    expect(nextVisit([earlier, active])?.source.order_id).toBe('active');
    active.status = 'Выполнена';
    expect(nextVisit([active, earlier])?.source.order_id).toBe('earlier');
    earlier.status = 'Выполнена';
    expect(nextVisit([active, earlier])).toBeUndefined();
  });
});
