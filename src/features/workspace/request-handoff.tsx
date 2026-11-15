'use client';
import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import Link from 'next/link';
import { request } from '@/shared/api/client';
import type { User } from '@/features/auth/types';
import { Modal } from '@/components/ui/modal';
import { useOptionalWorkspace } from './context';
import { time } from './model';

export function RequestHandoff({
  order,
}: {
  order: { id: string; date: string; state: string; staff_id?: string; planned_start?: string };
}) {
  const w = useOptionalWorkspace();
  const client = useQueryClient();
  const [confirm, setConfirm] = useState(false);
  const accounts = useQuery({
    queryKey: ['handoff-accounts'],
    queryFn: () => request<User[]>('', '/ui/v1/users'),
    enabled: !!order.staff_id,
    staleTime: 0,
  });
  const advance = useMutation({
    mutationFn: () =>
      request('', `/ui/v1/days/${order.date}/events`, {
        method: 'POST',
        body: JSON.stringify({
          event_id: crypto.randomUUID(),
          type: 'TIME_TICK',
          timestamp: order.planned_start,
        }),
      }),
    onSuccess: async () => {
      await Promise.all([
        w?.refresh(),
        client.invalidateQueries({ queryKey: ['service-requests'] }),
      ]);
      setConfirm(false);
    },
  });
  if (!w || !order.staff_id) return null;
  const sameDay = w.data?.day.date === order.date;
  const demo = w.clock.data?.mode === 'DEMO';
  const future =
    sameDay && order.planned_start && Date.parse(order.planned_start) > Date.parse(w.data!.day.now);
  const account = accounts.data?.find((u) => u.staff_id === order.staff_id && !u.disabled);
  return (
    <div className="request-handoff">
      {account ? (
        <p>
          Логин исполнителя: <strong>{account.username}</strong>
        </p>
      ) : accounts.isPending ? (
        <p className="muted">Проверяем доступ исполнителя…</p>
      ) : accounts.error ? (
        <p role="alert">Не удалось проверить доступ исполнителя.</p>
      ) : (
        <p>
          Для {order.staff_id} нужен аккаунт:{' '}
          <Link href="/app/admin">Центр управления → Доступы</Link>.
        </p>
      )}
      {!sameDay ? (
        <button className="secondary-button" onClick={() => w.setDate(order.date)}>
          Выбрать смену {order.date}
        </button>
      ) : demo && future && ['SCHEDULED', 'EN_ROUTE', 'ARRIVED'].includes(order.state) ? (
        <>
          <button
            className="secondary-button"
            onClick={() => {
              advance.reset();
              setConfirm(true);
            }}
          >
            К началу визита
          </button>
          <p className="muted">
            Для демонстрации: перевести время всей смены к {time(order.planned_start!)}.
          </p>
        </>
      ) : null}
      {confirm && demo && (
        <Modal
          title="Перевести время смены"
          onClose={() => {
            if (!advance.isPending) setConfirm(false);
          }}
        >
          <p>
            Время смены {order.date} станет {time(order.planned_start!)} (UTC+5). Это затронет
            расписание и риски всех сотрудников. Назначенный исполнитель сможет нажать «Начал
            работу».
          </p>
          {advance.error && (
            <p className="form-error" role="alert">
              {advance.error.message}
            </p>
          )}
          <button
            className="primary-button"
            disabled={advance.isPending || w.busy}
            onClick={() => advance.mutate()}
          >
            {advance.isPending ? 'Обновляем смену…' : 'Перевести смену к визиту'}
          </button>
        </Modal>
      )}
    </div>
  );
}
