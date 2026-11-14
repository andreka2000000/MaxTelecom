import type { ViewOrder } from './model';

export function visitQueue(orders: ViewOrder[]) {
  return orders
    .filter((o) => o.stop && !o.groupId && o.status !== 'Отменена')
    .sort((a, b) => Date.parse(a.stop!.start) - Date.parse(b.stop!.start));
}

export function nextVisit(orders: ViewOrder[]) {
  const queue = visitQueue(orders);
  return (
    queue.find((o) => o.status === 'На объекте') || queue.find((o) => o.status !== 'Выполнена')
  );
}
