'use client';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { MapPin, CheckCircle2, Play, Navigation, Flag } from 'lucide-react';
import { useWorkspace } from './context';
import { request } from '@/shared/api/client';
import { ordersView, time } from './model';
import {
  executionSteps,
  isActiveVisit,
  nextExecutionStep,
  plannedDeparture,
  visitQueue,
} from './execution-model';
import { VisitTimes, VisitTools } from './visit-tools';
export function ExecutionPanel({ onMap }: { onMap: (id: string) => void }) {
  const w = useWorkspace();
  const client = useQueryClient();
  const d = w.data!;
  const orders = visitQueue(ordersView(d));
  const active = d.facts?.find((f) => isActiveVisit(f.status));
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
        Подтверждайте выезд, прибытие, начало и результат работы. Время смены: {time(d.day.now)}.
        Статусы отмечаются вручную, положение мастера по GPS не отслеживается.
      </p>
      {orders.map((o, index) => {
        const fact = d.facts?.find((f) => f.order_id === o.source.order_id);
        const started = fact?.status === 'IN_PROGRESS';
        const done = fact?.status === 'COMPLETED';
        const step = nextExecutionStep(fact?.status);
        const departure = plannedDeparture(o.stop!);
        const ready =
          step?.action === 'DEPART'
            ? Date.parse(d.day.now) >= Date.parse(departure)
            : step?.action !== 'START' || Date.parse(d.day.now) >= Date.parse(o.stop!.start);
        const previous =
          !fact &&
          orders
            .slice(0, index)
            .some(
              (previousOrder) =>
                !d.facts?.some(
                  (f) => f.order_id === previousOrder.source.order_id && f.status === 'COMPLETED',
                ),
            );
        const blocked = (!!active && active.order_id !== o.source.order_id) || previous;
        const currentStep = executionSteps.findIndex((item) => item.status === fact?.status);
        const recorded = [
          fact?.departed_at,
          fact?.arrived_at,
          fact?.started_at,
          fact?.completed_at,
        ];
        return (
          <form
            className={`request-decision live-form execution-task ${done ? 'task-done' : ''}`}
            key={o.source.order_id}
            onSubmit={(e) => {
              e.preventDefault();
              if (!step || !ready || blocked || save.isPending || w.busy) return;
              const f = new FormData(e.currentTarget);
              save.mutate({
                id: o.source.order_id,
                action: step.action,
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
            <small className="muted">Выезд по плану с {time(departure)}</small>
            <ol className="visit-progress" aria-label="Этапы визита">
              {executionSteps.map((item, itemIndex) => (
                <li
                  key={item.action}
                  className={recorded[itemIndex] ? 'is-confirmed' : ''}
                  aria-current={itemIndex === currentStep ? 'step' : undefined}
                >
                  <span>{recorded[itemIndex] ? <CheckCircle2 size={14} /> : itemIndex + 1}</span>
                  {item.label}
                </li>
              ))}
            </ol>
            <button className="text-button" type="button" onClick={() => onMap(o.source.order_id)}>
              <MapPin size={15} /> Показать точку на карте
            </button>
            <VisitTools order={o} />
            <VisitTimes fact={fact} />
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
                  {started ? (
                    <CheckCircle2 size={16} />
                  ) : step?.action === 'DEPART' ? (
                    <Navigation size={16} />
                  ) : step?.action === 'ARRIVE' ? (
                    <Flag size={16} />
                  ) : (
                    <Play size={16} />
                  )}
                  {save.isPending && save.variables?.id === o.source.order_id
                    ? 'Сохраняем…'
                    : step?.label}
                </button>
                {blocked && (
                  <small className="muted">Сначала завершите предыдущее задание по маршруту.</small>
                )}
                {!ready && (
                  <small className="muted">
                    {step?.action === 'DEPART'
                      ? `Выезд доступен с ${time(departure)} по времени смены.`
                      : `Начать работу можно с ${time(o.stop!.start)} по времени смены.`}{' '}
                    В деморежиме диспетчер может перевести время к началу визита.
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
