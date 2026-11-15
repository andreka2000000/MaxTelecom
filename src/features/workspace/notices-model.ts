import type { Snapshot } from './types';
import { eventNames } from './model';
export type Notice = { id: string; at: string; title: string; detail: string };
export function shiftNotices(d: Snapshot): Notice[] {
  const out: Notice[] = [];
  for (const a of d.plan?.alerts || [])
    out.push({
      id: `alert:${a.alert_id}:${a.status}`,
      at: a.resolved_at || a.created_at,
      title: a.status === 'RESOLVED' ? 'Предупреждение закрыто' : 'Требует внимания',
      detail: [a.order_id, a.staff_id, a.message].filter(Boolean).join(' · '),
    });
  for (const e of d.events)
    if (e.type !== 'TIME_TICK')
      out.push({
        id: `event:${e.event_id}`,
        at: e.timestamp,
        title: eventNames[e.type] || e.type,
        detail: [
          'order_id' in e ? e.order_id : e.type === 'NEW_ORDER' ? e.order.order_id : '',
          'staff_id' in e ? e.staff_id : '',
          'reason' in e
            ? (
                {
                  VEHICLE_BREAKDOWN: 'Неисправность автомобиля',
                  ILLNESS: 'Болезнь',
                  OTHER: 'Другая причина',
                  CUSTOMER_REQUEST: 'По просьбе абонента',
                  NOT_CONFIRMED: 'Не подтверждена',
                } as Record<string, string>
              )[e.reason] || e.reason
            : '',
        ]
          .filter(Boolean)
          .join(' · '),
      });
  for (const a of d.assignments || [])
    out.push({
      id: `assignment:${a.request_id}`,
      at: a.as_of,
      title: 'Назначение исполнителя',
      detail: `${a.order_id} · ${a.from_staff || 'Без назначения'} → ${a.to_staff} · ${a.actor_name} · ${a.reason}`,
    });
  for (const s of d.staff_changes || [])
    out.push({
      id: `staff:${s.request_id}`,
      at: s.as_of,
      title: 'Изменена карточка сотрудника',
      detail: `${s.staff_id} · ${s.actor_name} · ${s.reason}`,
    });
  for (const f of d.facts || []) {
    const steps = [
      ['departed_at', 'Исполнитель выехал'],
      ['arrived_at', 'Исполнитель прибыл'],
      ['started_at', 'Работа начата'],
      ['completed_at', 'Работа завершена'],
    ] as const;
    for (const [field, title] of steps)
      if (f[field])
        out.push({
          id: `fact:${f.order_id}:${field}`,
          at: f[field]!,
          title,
          detail: `${f.order_id} · ${f.staff_id}${field === 'completed_at' && f.report ? ' · ' + f.report : ''}`,
        });
  }
  return out.sort((a, b) => Date.parse(b.at) - Date.parse(a.at) || a.id.localeCompare(b.id));
}
export function noticeScope(user: string, d: Snapshot) {
  return `maxtelecom-notices:${user}:${d.day.date}:${d.generation}`;
}
