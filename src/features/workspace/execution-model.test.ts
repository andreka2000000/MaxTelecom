import { describe, expect, it } from 'vitest';
import {
  nextVisit,
  visitQueue,
  nextExecutionStep,
  plannedDeparture,
  navigatorURL,
  phoneURL,
} from './execution-model';
import type { ViewOrder } from './model';
import type { Stop } from './types';

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
    const active = order('active', '2026-10-28T14:00:00+05:00', 'Работа начата');
    expect(nextVisit([earlier, active])?.source.order_id).toBe('active');
    active.status = 'Выполнена';
    expect(nextVisit([active, earlier])?.source.order_id).toBe('earlier');
    earlier.status = 'Выполнена';
    expect(nextVisit([active, earlier])).toBeUndefined();
  });
  it.each(['В пути', 'Прибыл на объект'])(
    'keeps an active journey %s as the current destination',
    (status) => {
      const earlier = order('earlier', '2026-10-28T09:00:00+05:00');
      const active = order('active', '2026-10-28T14:00:00+05:00', status);
      expect(nextVisit([earlier, active])?.source.order_id).toBe('active');
    },
  );
  it('advances the journey without reopening a confirmed completion', () => {
    expect(nextExecutionStep()?.action).toBe('DEPART');
    expect(nextExecutionStep('EN_ROUTE')?.action).toBe('ARRIVE');
    expect(nextExecutionStep('ARRIVED')?.action).toBe('START');
    expect(nextExecutionStep('IN_PROGRESS')?.action).toBe('COMPLETE');
    expect(nextExecutionStep('COMPLETED')).toBeUndefined();
  });
  it('uses planned arrival minus the incoming travel time for departure', () => {
    const stop = { arrival: '2026-10-28T09:18:00+05:00', travel_sec: 1080 } as Stop;
    expect(new Date(plannedDeparture(stop)).toISOString()).toBe('2026-10-28T04:00:00.000Z');
  });
  it('sends a valid latitude-longitude destination to the external navigator', () => {
    const url = new URL(navigatorURL([60.6531, 56.8712])!);
    expect(url.origin + url.pathname).toBe('https://www.google.com/maps/dir/');
    expect(Object.fromEntries(url.searchParams)).toEqual({
      api: '1',
      destination: '56.8712,60.6531',
      travelmode: 'driving',
      dir_action: 'navigate',
    });
    expect(navigatorURL(undefined)).toBeNull();
    expect(navigatorURL([NaN, 56.8])).toBeNull();
    expect(navigatorURL([60, 100])).toBeNull();
  });
  it('offers calling only for a phone number', () => {
    expect(phoneURL('+7 (999) 123-45-67')).toBe('tel:+79991234567');
    expect(phoneURL('javascript:alert(1)')).toBeNull();
    expect(phoneURL('')).toBeNull();
  });
});
