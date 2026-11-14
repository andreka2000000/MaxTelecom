'use client';
import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Modal } from '@/components/ui/modal';
import { request } from '@/shared/api/client';
import { useWorkspace } from './context';
import { staffName, time } from './model';

type Change = {
  order_id: string;
  before_staff?: string;
  after_staff: string;
  before_start?: string;
  after_start: string;
  before_end?: string;
  after_end: string;
};
export type AssignmentOption = {
  staff_id: string;
  name: string;
  available: boolean;
  code: string;
  reason: string;
  start?: string;
  end?: string;
  travel_delta_sec: number;
  changes: Change[];
  token?: string;
};
export type AssignmentPreview = {
  generation: number;
  plan_version: number;
  order_id: string;
  current_staff?: string;
  allowed: boolean;
  reason?: string;
  options: AssignmentOption[];
};

export function AssignmentDialog({ orderId, onClose }: { orderId: string; onClose: () => void }) {
  const w = useWorkspace();
  const d = w.data!;
  const client = useQueryClient();
  const [staff, setStaff] = useState('');
  const [requestId, setRequestId] = useState(() => crypto.randomUUID());
  const preview = useQuery({
    queryKey: ['assignment-options', d.day.date, orderId],
    queryFn: ({ signal }) =>
      request<AssignmentPreview>(
        '',
        `/ui/v1/days/${d.day.date}/orders/${encodeURIComponent(orderId)}/assignment-options`,
        { signal },
      ),
    staleTime: 0,
    refetchOnWindowFocus: false,
    retry: false,
  });
  const selected = preview.data?.options.find((o) => o.staff_id === staff);
  const stale =
    !!preview.data &&
    (preview.data.generation !== d.generation ||
      preview.data.plan_version !== d.plan?.plan_version);
  const save = useMutation({
    mutationFn: (reason: string) =>
      request('', `/ui/v1/days/${d.day.date}/orders/${encodeURIComponent(orderId)}/assignment`, {
        method: 'POST',
        body: JSON.stringify({
          staff_id: staff,
          preview_token: selected?.token,
          request_id: requestId,
          reason,
        }),
      }),
    onSuccess: async () => {
      await Promise.all([
        w.refresh(),
        client.invalidateQueries({ queryKey: ['service-requests'] }),
        client.invalidateQueries({ queryKey: ['assignment-options'] }),
      ]);
      onClose();
    },
  });
  const name = (id: string) => {
    const s = d.reference.staff.find((s) => s.staff_id === id);
    return s ? `${staffName(s)} · ${id}` : id;
  };
  return (
    <Modal
      title="Назначение специалиста"
      onClose={() => {
        if (!save.isPending) onClose();
      }}
    >
      <form
        className="form-stack live-form assignment-form"
        onSubmit={(e) => {
          e.preventDefault();
          if (!selected?.available || stale || preview.isFetching) return;
          save.mutate(String(new FormData(e.currentTarget).get('reason') || ''));
        }}
      >
        <p>
          <strong>{orderId}</strong> · смена {d.day.date}
        </p>
        <p className="muted">
          Проверяем окно, смену, квалификацию, оборудование, дорогу и обед. Другие назначенные
          наряды не снимаются. Дальнейшие события могут изменить назначение при конфликте.
        </p>
        {preview.isPending ? (
          <p role="status">Проверяем сотрудников и расписание…</p>
        ) : preview.error ? (
          <p role="alert" className="form-error">
            {preview.error.message}
          </p>
        ) : !preview.data?.allowed ? (
          <p role="status">{preview.data?.reason}</p>
        ) : (
          <>
            <p>
              Сейчас:{' '}
              {preview.data.current_staff ? name(preview.data.current_staff) : 'Без назначения'}
            </p>
            <label>
              Новый исполнитель
              <select
                value={staff}
                required
                disabled={save.isPending || preview.isFetching}
                onChange={(e) => {
                  setStaff(e.target.value);
                  setRequestId(crypto.randomUUID());
                  save.reset();
                }}
              >
                <option value="">Выберите подходящего сотрудника</option>
                {preview.data.options.map((o) => (
                  <option key={o.staff_id} value={o.staff_id} disabled={!o.available}>
                    {o.name} · {o.staff_id} ·{' '}
                    {o.available ? `${time(o.start!)}–${time(o.end!)}` : o.reason}
                  </option>
                ))}
              </select>
            </label>
            {!preview.data.options.some((o) => o.available) && (
              <p role="status">
                Допустимых вариантов сейчас нет. Измените условия смены или согласуйте другое окно.
              </p>
            )}
            {selected?.available && (
              <section className="assignment-impact" aria-label="Влияние назначения">
                <h3>После подтверждения</h3>
                <p>
                  Визит:{' '}
                  <strong>
                    {time(selected.start!)}–{time(selected.end!)}
                  </strong>
                </p>
                <p>
                  Изменение общего времени переездов: {selected.travel_delta_sec > 0 ? '+' : ''}
                  {(selected.travel_delta_sec / 60).toLocaleString('ru-RU', {
                    maximumFractionDigits: 1,
                  })}{' '}
                  мин.
                </p>
                <p>Изменения по нарядам: {selected.changes.length}</p>
                <ul>
                  {selected.changes.map((c) => (
                    <li key={c.order_id}>
                      <strong>{c.order_id}</strong>
                      <br />
                      {c.before_staff
                        ? `${name(c.before_staff)} · ${time(c.before_start!)}–${time(c.before_end!)}`
                        : 'Без назначения'}
                      <br />→ {name(c.after_staff)} · {time(c.after_start)}–{time(c.after_end)}
                    </li>
                  ))}
                </ul>
              </section>
            )}
            <details className="assignment-rejections">
              <summary>Недоступные варианты и причины</summary>
              <ul>
                {preview.data.options
                  .filter((o) => !o.available)
                  .map((o) => (
                    <li key={o.staff_id}>
                      <strong>
                        {o.name} · {o.staff_id}
                      </strong>
                      <br />
                      {o.reason}
                    </li>
                  ))}
              </ul>
            </details>
            <label>
              Причина назначения / переназначения
              <textarea
                name="reason"
                required
                minLength={3}
                maxLength={2000}
                rows={2}
                placeholder="Например: согласовано с исполнителем, перераспределение нагрузки"
                disabled={save.isPending}
              />
            </label>
            {stale && (
              <p role="alert" className="form-error">
                План изменился. Обновите предпросмотр перед подтверждением.
              </p>
            )}
            {save.error && (
              <p role="alert" className="form-error">
                {save.error.message}
              </p>
            )}
            <button
              className="primary-button"
              disabled={
                !selected?.available || stale || preview.isFetching || save.isPending || w.busy
              }
            >
              {save.isPending ? 'Сохраняем назначение…' : 'Подтвердить назначение'}
            </button>
          </>
        )}
        <button
          className="secondary-button"
          type="button"
          disabled={preview.isFetching || save.isPending}
          onClick={() => {
            save.reset();
            setRequestId(crypto.randomUUID());
            void preview.refetch();
          }}
        >
          Обновить проверку
        </button>
      </form>
    </Modal>
  );
}
