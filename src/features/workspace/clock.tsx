'use client';
import { useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Clock3 } from 'lucide-react';
import { Modal } from '@/components/ui/modal';
import { request } from '@/shared/api/client';
import { useWorkspace } from './context';
import { useIdentity } from '@/features/auth/context';
import { fromLocal, localInput, time } from './model';
export type ShiftClock = {
  date: string;
  generation: number;
  revision: number;
  plan_version: number;
  mode: 'DEMO' | 'LIVE';
  model_now: string;
  server_now: string;
  state: 'PAUSED' | 'RUNNING' | 'ENDED' | 'AHEAD' | 'BLOCKED' | 'LAGGING';
  error?: string;
  synced_at?: string;
};
type Change = {
  request_id: string;
  actor_name: string;
  from_mode: string;
  to_mode: string;
  as_of: string;
  changed_at: string;
};
const states = {
  PAUSED: 'Время на паузе',
  RUNNING: 'Время обновляется сервером',
  ENDED: 'Рабочая дата завершена',
  AHEAD: 'Часы опережают сервер',
  BLOCKED: 'Синхронизация приостановлена',
  LAGGING: 'Время отстаёт от сервера',
};
export function ClockControl() {
  const w = useWorkspace();
  const user = useIdentity();
  const [open, setOpen] = useState(false);
  const clock = w.clock.data;
  return (
    <div className="shift-clock-bar">
      <Clock3 size={17} />
      <div>
        <strong>
          {clock
            ? `${clock.mode === 'DEMO' ? 'Демонстрационный' : 'Рабочий'} режим · ${time(clock.model_now)}`
            : 'Проверяем часы смены…'}
        </strong>
        <small>
          {clock ? `${states[clock.state]} · Екатеринбург, UTC+5` : 'Режим задаётся сервером'}
        </small>
      </div>
      <button
        type="button"
        className="secondary-button"
        onClick={() => setOpen(true)}
        disabled={!clock || w.busy}
      >
        {user.role === 'DISPATCHER' ? 'Время и режим' : 'О часах смены'}
      </button>
      {clock && ['BLOCKED', 'LAGGING', 'AHEAD'].includes(clock.state) && (
        <p role="alert" className="clock-warning">
          {clock.error || 'На экране последнее подтверждённое время. Проверьте синхронизацию.'}
        </p>
      )}
      {w.clock.error && (
        <p role="alert" className="clock-warning">
          Не удалось проверить режим времени. {w.clock.error.message}
        </p>
      )}
      {open && clock && (
        <ClockDialog key={`${w.date}:${clock.mode}`} onClose={() => setOpen(false)} />
      )}
    </div>
  );
}
function ClockDialog({ onClose }: { onClose: () => void }) {
  const w = useWorkspace();
  const d = w.data!;
  const clock = w.clock.data!;
  const user = useIdentity();
  const client = useQueryClient();
  const manager = user.role === 'DISPATCHER';
  const [at, setAt] = useState(localInput(clock.model_now));
  const intent = useRef({ signature: '', id: '' });
  const history = useQuery({
    queryKey: ['clock-history', w.date],
    queryFn: () => request<Change[]>('', `/ui/v1/days/${w.date}/clock/history`),
    enabled: manager,
  });
  const update = useMutation({
    mutationFn: async (action: {
      type: 'step' | 'mode' | 'sync';
      value?: string;
      id: string;
      generation: number;
      revision: number;
      mode: string;
    }) => {
      const path = `/ui/v1/days/${w.date}`;
      if (action.type === 'sync') return request('', path + '/clock/sync', { method: 'POST' });
      if (action.type === 'mode')
        return request('', path + '/clock', {
          method: 'PATCH',
          body: JSON.stringify({
            mode: action.mode,
            generation: action.generation,
            revision: action.revision,
            request_id: action.id,
          }),
        });
      if (!d.plan) throw new Error('Для шага времени сначала постройте план.');
      return request('', path + '/events', {
        method: 'POST',
        body: JSON.stringify({ event_id: action.id, type: 'TIME_TICK', timestamp: action.value }),
      });
    },
    onSuccess: async (_data, action) => {
      if (action.type === 'step' && action.value) setAt(localInput(action.value));
      intent.current = { signature: '', id: '' };
      await Promise.all([
        w.refresh(),
        client.invalidateQueries({ queryKey: ['clock-history', w.date] }),
        client.invalidateQueries({ queryKey: ['service-requests'] }),
      ]);
    },
  });
  const send = (type: 'step' | 'mode' | 'sync', value?: string) => {
    const action = {
      type,
      value,
      generation: clock.generation,
      revision: clock.revision,
      mode: clock.mode === 'DEMO' ? 'LIVE' : 'DEMO',
    };
    const signature = JSON.stringify(action);
    if (signature !== intent.current.signature)
      intent.current = { signature, id: crypto.randomUUID() };
    update.mutate({ ...action, id: intent.current.id });
  };
  const next = d.plan?.routes
    .flatMap((r) => r.stops)
    .filter((s) => s.type === 'VISIT' && Date.parse(s.start) > Date.parse(clock.model_now))
    .sort((a, b) => Date.parse(a.start) - Date.parse(b.start))[0];
  const currentDay = clock.server_now.slice(0, 10);
  const canLive =
    w.date === currentDay && Date.parse(clock.model_now) <= Date.parse(clock.server_now);
  const busy = update.isPending || w.busy;
  return (
    <Modal
      title="Время и режим смены"
      onClose={() => {
        if (!busy) onClose();
      }}
    >
      <div className="form-stack live-form clock-dialog">
        <p>
          <strong>{clock.mode === 'DEMO' ? 'Демонстрационный режим' : 'Рабочий режим'}</strong> ·{' '}
          {w.date}
        </p>
        <div className="clock-readings">
          <div>
            <small>Подтверждённое время смены</small>
            <strong>{time(clock.model_now)}</strong>
          </div>
          <div>
            <small>Сервер · Екатеринбург</small>
            <strong>{time(clock.server_now)}</strong>
            <small>{currentDay} · UTC+5</small>
          </div>
        </div>
        <p>
          {states[clock.state]}.{' '}
          {clock.mode === 'DEMO'
            ? 'Часы стоят, пока диспетчер не передвинет их. Начало и завершение визитов зависят от времени смены.'
            : 'Сервер обновляет смену каждую минуту, проверяя часы каждые 15 секунд, в том числе при закрытом браузере. Подтверждения выполнения используют серверное время до секунды.'}
        </p>
        {clock.error && (
          <p role="alert" className="form-error">
            {clock.error}
          </p>
        )}
        {manager && clock.mode === 'DEMO' && (
          <>
            <div className="clock-steps">
              {[5, 15, 60].map((minutes) => {
                const target = new Date(
                  Date.parse(clock.model_now) + minutes * 60000,
                ).toISOString();
                const inDay = localInput(target).slice(0, 10) === w.date;
                return (
                  <button
                    type="button"
                    className="secondary-button"
                    key={minutes}
                    disabled={busy || !d.plan || !inDay}
                    onClick={() => send('step', target)}
                  >
                    +{minutes} мин
                  </button>
                );
              })}
            </div>
            <button
              type="button"
              className="secondary-button"
              disabled={busy || !next}
              onClick={() => next && send('step', next.start)}
            >
              К следующему визиту{next ? ` · ${time(next.start)}` : ''}
            </button>
            <form
              onSubmit={(e) => {
                e.preventDefault();
                send('step', fromLocal(String(new FormData(e.currentTarget).get('model_time'))));
              }}
              className="form-stack"
            >
              <label>
                Установить время смены
                <input
                  type="datetime-local"
                  name="model_time"
                  step="1"
                  min={localInput(clock.model_now)}
                  max={`${w.date}T23:59:59`}
                  value={at}
                  required
                  onChange={(e) => {
                    setAt(e.target.value);
                    update.reset();
                  }}
                />
              </label>
              <button className="secondary-button" disabled={busy || !d.plan}>
                Применить время
              </button>
            </form>
            {!d.plan && (
              <p className="muted">
                Сначала постройте первоначальный план, затем используйте шаги времени.
              </p>
            )}
          </>
        )}
        {manager && (
          <>
            {clock.mode === 'LIVE' && (
              <button
                type="button"
                className="secondary-button"
                disabled={busy}
                onClick={() => send('sync')}
              >
                Синхронизировать сейчас
              </button>
            )}
            <p className="muted">
              {clock.mode === 'DEMO'
                ? 'Рабочий режим подхватит текущее время сервера и пересчитает план. Перевод часов назад запрещён.'
                : 'Деморежим остановит часы на текущем подтверждённом времени. Сохранённая история останется.'}
            </p>
            {clock.mode === 'DEMO' && !canLive && (
              <p className="muted">
                Рабочий режим доступен только сегодня, если часы смены не опережают сервер. Для
                нового рабочего дня: «Центр управления → Новая смена → Рабочий режим».
              </p>
            )}
            <button
              type="button"
              className="primary-button"
              disabled={busy || (clock.mode === 'DEMO' && !canLive)}
              onClick={() => send('mode')}
            >
              {update.isPending
                ? 'Обновляем…'
                : clock.mode === 'DEMO'
                  ? 'Включить рабочий режим'
                  : 'Перейти в деморежим'}
            </button>
          </>
        )}
        {!manager && (
          <p className="muted">
            Режим и часы меняет диспетчер. Начало и завершение работ подтверждает исполнитель.
          </p>
        )}
        {update.error && (
          <p className="form-error" role="alert">
            {update.error.message}
          </p>
        )}
        {update.isSuccess && <p role="status">Изменение времени сохранено.</p>}
        {manager && !!history.data?.length && (
          <details className="assignment-rejections">
            <summary>История режимов · {history.data.length}</summary>
            {history.data.map((c) => (
              <p key={c.request_id}>
                {c.actor_name}: {c.from_mode === 'DEMO' ? 'Демо' : 'Рабочий'} →{' '}
                {c.to_mode === 'DEMO' ? 'Демо' : 'Рабочий'} · время смены {time(c.as_of)}
              </p>
            ))}
          </details>
        )}
      </div>
    </Modal>
  );
}
