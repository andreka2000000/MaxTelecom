'use client';
import { useState } from 'react';
import { useRouter } from 'next/navigation';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { CalendarPlus } from 'lucide-react';
import { request } from '@/shared/api/client';
import { Modal } from '@/components/ui/modal';
import { useWorkspace } from './context';
import { nextShiftDate, ekaterinburgDate } from './shift-selection';
export function NewShift() {
  const w = useWorkspace();
  const router = useRouter();
  const client = useQueryClient();
  const [open, setOpen] = useState(false);
  const [mode, setMode] = useState('empty');
  const [source, setSource] = useState('');
  const [clockMode, setClockMode] = useState('DEMO');
  const [dateValue, setDateValue] = useState(nextShiftDate(w.date));
  const save = useMutation({
    mutationFn: (date: string) =>
      request<{ date: string }>(
        '',
        mode === 'copy' ? `/ui/v1/days/${source || w.date}/copy` : '/ui/v1/days',
        {
          method: 'POST',
          body: JSON.stringify(mode === 'copy' ? { date } : { date, clock_mode: clockMode }),
        },
      ),
    onSuccess: async (day) => {
      w.setDate(day.date);
      await client.invalidateQueries({ queryKey: ['workspace-days'] });
      setOpen(false);
      router.push('/app/dispatch');
    },
  });
  const next = nextShiftDate(w.date);
  return (
    <>
      <button
        className="secondary-button"
        onClick={() => {
          save.reset();
          setClockMode('DEMO');
          setDateValue(next);
          setOpen(true);
        }}
      >
        <CalendarPlus size={16} />
        Новая смена
      </button>
      {open && (
        <Modal title="Создать рабочую смену" onClose={() => setOpen(false)}>
          <form
            className="form-stack live-form"
            onSubmit={(e) => {
              e.preventDefault();
              save.mutate(String(new FormData(e.currentTarget).get('date')));
            }}
          >
            <label>
              Содержимое смены
              <select value={mode} onChange={(e) => setMode(e.target.value)}>
                <option value="empty">Пустая смена для новых обращений</option>
                <option value="copy">Копия набора на другую дату</option>
              </select>
            </label>
            {mode === 'empty' && (
              <label>
                Режим времени
                <select
                  value={clockMode}
                  onChange={(e) => {
                    setClockMode(e.target.value);
                    if (e.target.value === 'LIVE')
                      setDateValue(w.clock.data?.server_now.slice(0, 10) || ekaterinburgDate());
                  }}
                >
                  <option value="DEMO">Демонстрационный — управляемые часы</option>
                  <option value="LIVE">Рабочий — текущее время Екатеринбурга</option>
                </select>
              </label>
            )}
            {mode === 'copy' && (
              <label>
                Исходная смена
                <select
                  value={source || w.date}
                  onChange={(e) => setSource(e.target.value)}
                  required
                >
                  {w.days.data?.map((d) => (
                    <option key={d.date} value={d.date}>
                      {d.date} · {d.orders_total ?? '—'} заявок
                    </option>
                  ))}
                </select>
              </label>
            )}
            <p>
              {mode === 'copy'
                ? 'Наряды, окна и сроки будут перенесены на новую дату. Время суток сохраняется. События и фактическое выполнение остаются в исходной смене. План нужно построить заново.'
                : 'Новая смена использует текущих сотрудников, сервисные центры и дорожную матрицу. Обращения в неё добавляются после согласования.'}
            </p>
            <label>
              Дата смены
              <input
                name="date"
                type="date"
                required
                value={dateValue}
                onChange={(e) => setDateValue(e.target.value)}
              />
            </label>
            <p className="muted">
              {mode === 'empty'
                ? clockMode === 'LIVE'
                  ? 'Начальное время — текущее серверное, дата должна быть сегодняшней по Екатеринбургу. '
                  : 'Начальное демонстрационное время — 08:30. '
                : 'Копия создаётся в демонстрационном режиме. '}
              После создания нажмите «Построить план» в диспетчерской. Существующая смена на
              выбранную дату не будет заменена.
            </p>
            {save.error && (
              <p className="form-error" role="alert">
                {save.error.message}
              </p>
            )}
            <button className="primary-button" disabled={save.isPending}>
              {save.isPending ? 'Создаём…' : 'Создать смену'}
            </button>
          </form>
        </Modal>
      )}
    </>
  );
}
