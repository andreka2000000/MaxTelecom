'use client';
import { useState } from 'react';
import { Bell, CalendarDays, RefreshCw } from 'lucide-react';
import { useOptionalWorkspace } from '@/features/workspace/context';
import { countText } from '@/features/workspace/model';
import { ClockControl } from '@/features/workspace/clock';
import { NotificationCenter } from '@/features/workspace/notification-center';
export function PageHeader({
  title,
  subtitle,
  children,
}: {
  title: string;
  subtitle: string;
  children?: React.ReactNode;
}) {
  const [notifications, setNotifications] = useState(false);
  const w = useOptionalWorkspace();
  const active = w?.data?.plan?.alerts.filter((a) => a.status === 'ACTIVE') || [];
  return (
    <header className="page-header">
      <div className="heading-block">
        <h1>{title}</h1>
        <p>{subtitle}</p>
      </div>
      <div className="header-controls">
        {children}
        {w && (
          <>
            <label className="day-picker">
              <CalendarDays size={15} />
              <span className="sr-only">День смены</span>
              <select
                aria-label="День смены"
                value={w.date}
                disabled={w.busy || !w.days.data?.length}
                onChange={(e) => w.setDate(e.target.value)}
              >
                {!w.days.data?.length && <option value="">Нет смен</option>}
                {w.days.data?.map((d) => (
                  <option key={d.date} value={d.date}>
                    {new Intl.DateTimeFormat('ru-RU', {
                      day: 'numeric',
                      month: 'long',
                      year: 'numeric',
                      timeZone: 'UTC',
                    }).format(new Date(d.date + 'T00:00:00Z'))}
                    {d.orders_total !== undefined
                      ? ` · ${countText(d.orders_total, 'заявка', 'заявки', 'заявок')}`
                      : ''}
                    {d.clock_mode ? ` · ${d.clock_mode === 'LIVE' ? 'рабочий' : 'демо'}` : ''}
                  </option>
                ))}
              </select>
            </label>
            <button
              className="icon-button"
              aria-label="Обновить смену"
              disabled={w.busy}
              onClick={() => void w.refresh()}
            >
              <RefreshCw size={17} />
            </button>
          </>
        )}
        {w && (
          <button
            className="icon-button notification-button"
            aria-label="Уведомления"
            aria-expanded={notifications}
            onClick={() => setNotifications(!notifications)}
          >
            <Bell size={19} />
            {active.length > 0 && <i />}
          </button>
        )}
        {w?.data && notifications && (
          <NotificationCenter
            key={`${w.date}:${w.data.generation}`}
            onClose={() => setNotifications(false)}
          />
        )}
      </div>
      {w?.data && (
        <p className="selected-shift-note">
          Смена {w.data.day.date} ·{' '}
          {countText(w.data.day.orders.length, 'заявка', 'заявки', 'заявок')} ·{' '}
          {countText(w.data.reference.staff.length, 'сотрудник', 'сотрудника', 'сотрудников')} ·{' '}
          {new Intl.DateTimeFormat('ru-RU', {
            timeZone: 'Asia/Yekaterinburg',
            hour: '2-digit',
            minute: '2-digit',
            hourCycle: 'h23',
          }).format(new Date(w.data.day.now))}{' '}
          · Екатеринбург (UTC+5)
        </p>
      )}
      {w?.data && <ClockControl key={w.date} />}
    </header>
  );
}
