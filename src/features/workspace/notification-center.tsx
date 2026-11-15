'use client';
import { useState } from 'react';
import { useIdentity } from '@/features/auth/context';
import { useWorkspace } from './context';
import { noticeScope, shiftNotices } from './notices-model';
import { time } from './model';
export function NotificationCenter({ onClose }: { onClose: () => void }) {
  const w = useWorkspace();
  const user = useIdentity();
  const d = w.data!;
  const key = noticeScope(user.id, d);
  const items = shiftNotices(d);
  const [seen, setSeen] = useState<string[]>(() => {
    try {
      const v = JSON.parse(localStorage.getItem(key) || '[]');
      return Array.isArray(v) ? v.filter((x) => typeof x === 'string') : [];
    } catch {
      return [];
    }
  });
  const [onlyNew, setOnlyNew] = useState(false);
  const [limit, setLimit] = useState(50);
  const unread = items.filter((n) => !seen.includes(n.id));
  const filtered = onlyNew ? unread : items;
  function mark(ids: string[]) {
    const next = [...new Set([...seen, ...ids])];
    setSeen(next);
    try {
      localStorage.setItem(key, JSON.stringify(next));
    } catch {
      /* Reading still works without storage. */
    }
  }
  return (
    <section className="notifications notice-center" aria-label="Уведомления и история">
      <div className="notice-heading">
        <strong>Уведомления и история</strong>
        <button className="text-button" onClick={onClose}>
          Закрыть
        </button>
      </div>
      <p>
        Непрочитанных: {unread.length} · записей: {items.length}
      </p>
      <div className="notice-actions">
        <button
          className="secondary-button"
          aria-pressed={!onlyNew}
          onClick={() => {
            setOnlyNew(false);
            setLimit(50);
          }}
        >
          Вся история
        </button>
        <button
          className="secondary-button"
          aria-pressed={onlyNew}
          onClick={() => {
            setOnlyNew(true);
            setLimit(50);
          }}
        >
          Непрочитанные
        </button>
        <button
          className="secondary-button"
          disabled={!unread.length}
          onClick={() => mark(items.map((n) => n.id))}
        >
          Прочитать все
        </button>
      </div>
      <p className="muted">
        Выбранная смена · время Екатеринбурга. Прочитанное сохраняется в этом браузере.
      </p>
      <div className="notice-items">
        {filtered.slice(0, limit).map((n) => (
          <article key={n.id} className={seen.includes(n.id) ? 'notice-read' : 'notice-unread'}>
            <strong>{n.title}</strong>
            <time dateTime={n.at}>{time(n.at)}</time>
            <p>{n.detail || 'Изменение сохранено в смене.'}</p>
            {!seen.includes(n.id) && (
              <button className="text-button" onClick={() => mark([n.id])}>
                Прочитано
              </button>
            )}
          </article>
        ))}
      </div>
      {!filtered.length && <p>Новых записей нет.</p>}
      {filtered.length > limit && (
        <button className="secondary-button" onClick={() => setLimit(limit + 50)}>
          Показать ещё
        </button>
      )}
    </section>
  );
}
