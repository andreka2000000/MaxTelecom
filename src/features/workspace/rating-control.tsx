'use client';
import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { request } from '@/shared/api/client';
export function RatingControl({ id, saved }: { id: string; saved: number }) {
  const [score, setScore] = useState('5');
  const qc = useQueryClient();
  const save = useMutation({
    mutationFn: () =>
      request('', `/ui/v1/requests/${id}/rating`, {
        method: 'POST',
        body: JSON.stringify({ score: Number(score) }),
      }),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ['service-requests'] });
      await qc.invalidateQueries({ queryKey: ['workspace'] });
    },
  });
  return (
    <div className="request-reply">
      {saved ? (
        <p>Ваша оценка сотруднику: ★ {saved} из 5</p>
      ) : (
        <>
          <label>
            Оценка работы сотрудника
            <select value={score} onChange={(e) => setScore(e.target.value)}>
              {[5, 4, 3, 2, 1].map((n) => (
                <option key={n} value={n}>
                  {n} из 5
                </option>
              ))}
            </select>
          </label>
          <button
            className="primary-button"
            disabled={save.isPending}
            onClick={() => save.mutate()}
          >
            Оценить сотрудника
          </button>
        </>
      )}
      {save.error && <p role="alert">{save.error.message}</p>}
    </div>
  );
}
