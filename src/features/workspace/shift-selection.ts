import type { DaySummary } from './types';

export function ekaterinburgDate(value = new Date()) {
  return new Intl.DateTimeFormat('sv-SE', {
    timeZone: 'Asia/Yekaterinburg',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  }).format(value);
}
export function chooseShift(days: DaySummary[], saved: string, today: string) {
  if (days.some((d) => d.date === saved)) return saved;
  if (days.some((d) => d.date === today)) return today;
  return (
    [...days].sort(
      (a, b) =>
        Math.abs(Date.parse(a.date) - Date.parse(today)) -
          Math.abs(Date.parse(b.date) - Date.parse(today)) || b.date.localeCompare(a.date),
    )[0]?.date || ''
  );
}
export function nextShiftDate(selected: string, today = ekaterinburgDate()) {
  const next = selected
    ? new Date(Date.parse(selected + 'T00:00:00Z') + 86400000).toISOString().slice(0, 10)
    : today;
  return next < today ? today : next;
}
