'use client';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { MapPin, CheckCircle2, Play } from 'lucide-react';
import { useWorkspace } from './context';
import { request } from '@/shared/api/client';
import { ordersView, time } from './model';
import { visitQueue } from './execution-model';
export function ExecutionPanel({ onMap }: { onMap: (id: string) => void }) {
  const w = useWorkspace();
  const client = useQueryClient();
  const d = w.data!;
  const orders = visitQueue(ordersView(d));
  const active = d.facts?.find((f) => f.status === 'IN_PROGRESS');
  const save = useMutation({
    mutationFn: (input: { id: string; action: string; report: string }) =>
      request('', `/ui/v1/days/${d.day.date}/orders/${encodeURIComponent(input.id)}/execution`, {
        method: 'POST',
        body: JSON.stringify({ action: input.action, report: input.report }),
      }),
    onSuccess: async () => {
      await Promise.all([
        w.refresh(),
        client.invalidateQueries({ queryKey: ['service-requests'] }),
      ]);
    },
  });
  return (
    <section className="live-panel execution-panel" id="my-tasks">
      <span className="eyebrow">ФАКТИЧЕСКОЕ ВЫПОЛНЕНИЕ</span>
      <h2>Мои задания</h2>
      <p className="muted">
        Подтверждайте начало и результат работы. Модельное время смены: {time(d.day.now)}.
      </p>
      {orders.map((o) => {
        const fact = d.facts?.find((f) => f.order_id === o.source.order_id);
        const started = fact?.status === 'IN_PROGRESS';
        const done = fact?.status === 'COMPLETED';
        const ready = Date.parse(d.day.now) >= Date.parse(o.stop!.start);
        const blocked = !!active && active.order_id !== o.source.order_id;
        return (
          <form
            className={`request-decision live-form execution-task ${done ? 'task-done' : ''}`}
            key={o.source.order_id}
            onSubmit={(e) => {
              e.preventDefault();
              const f = new FormData(e.currentTarget);
              save.mutate({
                id: o.source.order_id,
                action: started ? 'COMPLETE' : 'START',
                report: String(f.get('report') || ''),
              });
            }}
          >
            <div className="live-section-title">
              <strong>{o.title}</strong>
              <span className={`state-badge ${o.tone}`}>{o.status}</span>
            </div>
            <p>{o.source.address}</p>
            <small>
              {o.source.order_id} · {time(o.stop!.start)}–{time(o.stop!.end)}
            </small>
            <button className="text-button" type="button" onClick={() => onMap(o.source.order_id)}>
              <MapPin size={15} /> Показать точку на карте
            </button>
            {fact && (
              <p className="muted">
                Начал: {time(fact.started_at)}
                {fact.completed_at ? ` · Закончил: ${time(fact.completed_at)}` : ''}
              </p>
            )}
            {done ? (
              <p className="execution-report">
                <CheckCircle2 size={17} />
                {fact.report}
              </p>
            ) : (
              <>
                {started && (
                  <label>
                    Результат работы
                    <textarea name="report" required minLength={3} maxLength={2000} rows={2} />
                  </label>
                )}
                <button
                  className="primary-button"
                  disabled={save.isPending || w.busy || !ready || blocked}
                >
                  {started ? <CheckCircle2 size={16} /> : <Play size={16} />}
                  {save.isPending && save.variables?.id === o.source.order_id
                    ? 'Сохраняем…'
                    : started
                      ? 'Закончил работу'
                      : 'Начал работу'}
                </button>
                {blocked && <small className="muted">Сначала завершите текущее задание.</small>}
                {!ready && (
                  <small className="muted">
                    Доступно с {time(o.stop!.start)} по времени смены. Для демонстрации диспетчер
                    может нажать «К началу визита» в обращении абонента.
                  </small>
                )}
              </>
            )}
          </form>
        );
      })}
      {!orders.length && <p className="muted">Назначенных заданий пока нет.</p>}
      {save.error && (
        <p className="form-error" role="alert">
          {save.error.message}
        </p>
      )}
    </section>
  );
}
