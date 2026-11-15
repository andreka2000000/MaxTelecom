import type { ViewOrder } from './model';
import type { Stop, VisitFact } from './types';

export const executionSteps = [
  { action: 'DEPART', status: 'EN_ROUTE', label: 'Выехал' },
  { action: 'ARRIVE', status: 'ARRIVED', label: 'Прибыл' },
  { action: 'START', status: 'IN_PROGRESS', label: 'Начал работу' },
  { action: 'COMPLETE', status: 'COMPLETED', label: 'Закончил работу' },
] as const;

export function nextExecutionStep(status?: VisitFact['status']) {
  const current = executionSteps.findIndex((step) => step.status === status);
  return executionSteps[current + 1];
}

export function isActiveVisit(status: VisitFact['status']) {
  return status !== 'COMPLETED';
}

export function plannedDeparture(stop: Stop) {
  return new Date(
    Date.parse(stop.arrival) - Math.max(0, stop.travel_sec || 0) * 1000,
  ).toISOString();
}

export function navigatorURL(point: [number, number] | undefined) {
  if (!point) return null;
  const [lon, lat] = point;
  if (!Number.isFinite(lon) || !Number.isFinite(lat) || Math.abs(lon) > 180 || Math.abs(lat) > 90)
    return null;
  const params = new URLSearchParams({
    api: '1',
    destination: `${lat},${lon}`,
    travelmode: 'driving',
    dir_action: 'navigate',
  });
  return `https://www.google.com/maps/dir/?${params}`;
}

export function phoneURL(phone: string) {
  const value = phone.trim();
  if (!/^\+?[\d\s()-]+$/.test(value)) return null;
  const number = value.replace(/[^\d+]/g, '');
  return number.replace(/\D/g, '').length >= 5 ? `tel:${number}` : null;
}

export function visitQueue(orders: ViewOrder[]) {
  return orders
    .filter((o) => o.stop && !o.groupId && o.status !== 'Отменена')
    .sort((a, b) => Date.parse(a.stop!.start) - Date.parse(b.stop!.start));
}

export function nextVisit(orders: ViewOrder[]) {
  const queue = visitQueue(orders);
  return (
    queue.find((o) => ['В пути', 'Прибыл на объект', 'Работа начата'].includes(o.status)) ||
    queue.find((o) => o.status !== 'Выполнена')
  );
}
