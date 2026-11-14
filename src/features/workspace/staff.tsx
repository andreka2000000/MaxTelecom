'use client';
import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Modal } from '@/components/ui/modal';
import { request } from '@/shared/api/client';
import { useWorkspace } from './context';
import { countText, initials, profileNames, staffName, time } from './model';
import type { Staff, StaffVisitChange } from './types';

type Settings = Pick<
  Staff,
  'profiles' | 'technologies' | 'height_permit' | 'shift' | 'equipment_start' | 'equipment_capacity'
> & { name: string; absent: boolean };
type Preview = {
  generation: number;
  revision: number;
  plan_version: number;
  has_plan: boolean;
  token: string;
  before: Staff;
  after: Staff;
  absent_before: boolean;
  absent_after: boolean;
  changes: StaffVisitChange[];
  visits_before: number;
  visits_after: number;
  unassigned_before: number;
  unassigned_after: number;
  travel_delta_sec: number;
};
const inventory = (v: Staff['equipment_start']) =>
  `ONT: ${v.ONT} · ROUTER: ${v.ROUTER} · STB: ${v.STB}`;
const shiftText = (v: Staff['shift']) =>
  `${v.start}–${v.end}; обед ${v.break_duration_min} мин, начало ${v.break_window.from}–${v.break_window.to}`;
function differences(before: Staff, after: Staff, absentBefore: boolean, absentAfter: boolean) {
  const fields = [
    ['Имя', staffName(before), staffName(after)],
    [
      'Специализации',
      before.profiles
        .map((p) => profileNames[p])
        .sort()
        .join(', '),
      after.profiles
        .map((p) => profileNames[p])
        .sort()
        .join(', '),
    ],
    [
      'Технологии',
      [...before.technologies].sort().join(', '),
      [...after.technologies].sort().join(', '),
    ],
    [
      'Допуск к высоте',
      before.height_permit ? 'Есть' : 'Нет',
      after.height_permit ? 'Есть' : 'Нет',
    ],
    ['Смена', shiftText(before.shift), shiftText(after.shift)],
    ['Начальный запас', inventory(before.equipment_start), inventory(after.equipment_start)],
    ['Вместимость', inventory(before.equipment_capacity), inventory(after.equipment_capacity)],
    [
      'Участие в смене',
      absentBefore ? 'Отсутствует' : 'На смене',
      absentAfter ? 'Отсутствует' : 'На смене',
    ],
  ];
  return fields.filter(([, a, b]) => a !== b);
}

export function StaffTeam() {
  const d = useWorkspace().data!;
  const [search, setSearch] = useState('');
  const [editing, setEditing] = useState<string | null>(null);
  const people = d.reference.staff.filter((s) =>
    `${staffName(s)} ${s.staff_id} ${s.technologies.join(' ')} ${s.profiles.map((p) => profileNames[p]).join(' ')}`
      .toLowerCase()
      .includes(search.toLowerCase()),
  );
  return (
    <>
      <div className="live-section-title">
        <h2>Команда МаксТелеком</h2>
        <span className="subtle-chip">
          {countText(d.reference.staff.length, 'сотрудник', 'сотрудника', 'сотрудников')}
        </span>
      </div>
      <p className="muted">
        Карточки и настройки смены {d.day.date}. Перед сохранением система покажет влияние на
        назначения. При копировании смены настройки сотрудников переходят в копию.
      </p>
      <label className="team-search">
        Найти сотрудника
        <input
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="Имя, код или технология"
        />
      </label>
      <div className="staff-card-grid">
        {people.map((s) => {
          const absent = d.day.absent_staff_ids?.includes(s.staff_id);
          const route = d.plan?.routes.find((r) => r.staff_id === s.staff_id);
          return (
            <article className="staff-card" key={s.staff_id}>
              <div className="staff-card-title">
                <span className="avatar">{initials(staffName(s))}</span>
                <div>
                  <h3>{staffName(s)}</h3>
                  <small>
                    {s.staff_id} ·{' '}
                    {d.reference.service_centers.find(
                      (c) => c.service_center_id === s.service_center_id,
                    )?.name || s.service_center_id}
                  </small>
                </div>
              </div>
              <span className={`state-badge ${absent ? 'amber' : 'green'}`}>
                {absent ? 'Отсутствует' : 'На смене'} ·{' '}
                {countText(route?.totals?.visits || 0, 'визит', 'визита', 'визитов')}
              </span>
              <p>
                {s.profiles.map((p) => profileNames[p]).join(' · ')}
                <br />
                {s.technologies.join(' · ')}
              </p>
              <dl>
                <div>
                  <dt>Смена</dt>
                  <dd>
                    {s.shift.start}–{s.shift.end}
                  </dd>
                </div>
                <div>
                  <dt>Высотный допуск</dt>
                  <dd>{s.height_permit ? 'Есть' : 'Нет'}</dd>
                </div>
              </dl>
              <p className="muted">Начальный запас: {inventory(s.equipment_start)}</p>
              <button
                className="secondary-button"
                aria-label={`Открыть карточку ${s.staff_id}`}
                onClick={() => setEditing(s.staff_id)}
              >
                Открыть карточку
              </button>
            </article>
          );
        })}
      </div>
      {!people.length && <p className="gentle-empty">Сотрудники не найдены. Измените поиск.</p>}
      {editing && (
        <StaffEditor
          key={`${d.day.date}:${editing}`}
          staffId={editing}
          onClose={() => setEditing(null)}
        />
      )}
    </>
  );
}

function StaffEditor({ staffId, onClose }: { staffId: string; onClose: () => void }) {
  const w = useWorkspace();
  const d = w.data!;
  const client = useQueryClient();
  const person = d.reference.staff.find((s) => s.staff_id === staffId)!;
  const [base] = useState({ generation: d.generation, revision: d.revision ?? 0 });
  const [settings, setSettings] = useState<Settings>(() => ({
    name: staffName(person),
    profiles: [...person.profiles],
    technologies: [...person.technologies],
    height_permit: person.height_permit,
    shift: structuredClone(person.shift),
    equipment_start: { ...person.equipment_start },
    equipment_capacity: { ...person.equipment_capacity },
    absent: !!d.day.absent_staff_ids?.includes(staffId),
  }));
  const [reason, setReason] = useState('');
  const [requestId, setRequestId] = useState(() => crypto.randomUUID());
  const path = `/ui/v1/days/${d.day.date}/staff/${staffId}`;
  const preview = useMutation({
    mutationFn: () =>
      request<Preview>('', path + '/preview', {
        method: 'POST',
        body: JSON.stringify({ ...base, settings }),
      }),
  });
  const save = useMutation({
    mutationFn: () =>
      request('', path, {
        method: 'PATCH',
        body: JSON.stringify({
          ...base,
          settings,
          reason,
          request_id: requestId,
          preview_token: preview.data?.token,
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
  const busy = preview.isPending || save.isPending;
  const stale = base.generation !== d.generation || base.revision !== (d.revision ?? 0);
  const locked =
    !!d.plan?.routes
      .find((r) => r.staff_id === staffId)
      ?.stops.some((s) => Date.parse(s.start) <= Date.parse(d.day.now)) ||
    !!d.facts?.some((f) => f.staff_id === staffId);
  const change = (next: Settings) => {
    setSettings(next);
    preview.reset();
    save.reset();
    setRequestId(crypto.randomUUID());
  };
  const name = (id?: string, updated = false) => {
    if (!id) return 'Без назначения';
    const s =
      updated && preview.data?.after.staff_id === id
        ? preview.data.after
        : d.reference.staff.find((s) => s.staff_id === id);
    return s ? `${staffName(s)} · ${id}` : id;
  };
  const history = d.staff_changes?.filter((c) => c.staff_id === staffId) || [];
  return (
    <Modal
      title="Карточка сотрудника"
      onClose={() => {
        if (!busy) onClose();
      }}
    >
      <form
        className="form-stack live-form staff-editor"
        onSubmit={(e) => {
          e.preventDefault();
          if (!busy && !stale && !locked) preview.mutate();
        }}
      >
        <p>
          <strong>{staffId}</strong> · смена {d.day.date} · время {time(d.day.now)}
        </p>
        <p className="muted">
          Изменения действуют в выбранной смене. Сервисный центр и код сотрудника сохраняются. Для
          использования этих настроек в другом дне создайте копию смены.
        </p>
        {locked && (
          <p className="detail-reason" role="status">
            Маршрут уже начался: настройки этой смены зафиксированы. Для выбытия используйте
            «Диспетчерская → Добавить событие → Недоступность мастера».
          </p>
        )}
        {stale && (
          <p className="form-error" role="alert">
            Смена изменилась во время редактирования. Закройте карточку, обновите смену и откройте
            её снова.
          </p>
        )}
        <fieldset disabled={busy || locked || stale} className="staff-fields">
          <legend>Данные и квалификация</legend>
          <label>
            Имя сотрудника
            <input
              value={settings.name}
              maxLength={160}
              required
              onChange={(e) => change({ ...settings, name: e.target.value })}
            />
          </label>
          <fieldset className="staff-checks">
            <legend>Специализации</legend>
            {(['LTP3', 'INSTALL'] as const).map((p) => (
              <label key={p}>
                <input
                  type="checkbox"
                  checked={settings.profiles.includes(p)}
                  onChange={(e) =>
                    change({
                      ...settings,
                      profiles: e.target.checked
                        ? [...settings.profiles, p]
                        : settings.profiles.filter((v) => v !== p),
                    })
                  }
                />
                {profileNames[p]}
              </label>
            ))}
          </fieldset>
          <fieldset className="staff-checks">
            <legend>Технологии</legend>
            {(['PON', 'FTTB', 'XDSL', 'DOCSIS'] as const).map((t) => (
              <label key={t}>
                <input
                  type="checkbox"
                  checked={settings.technologies.includes(t)}
                  onChange={(e) =>
                    change({
                      ...settings,
                      technologies: e.target.checked
                        ? [...settings.technologies, t]
                        : settings.technologies.filter((v) => v !== t),
                    })
                  }
                />
                {t}
              </label>
            ))}
          </fieldset>
          <label className="staff-check">
            <input
              type="checkbox"
              checked={settings.height_permit}
              onChange={(e) => change({ ...settings, height_permit: e.target.checked })}
            />
            Есть допуск к высотным работам
          </label>
          <label className="staff-check">
            <input
              type="checkbox"
              checked={settings.absent}
              onChange={(e) => change({ ...settings, absent: e.target.checked })}
            />
            Отсутствует в выбранной смене
          </label>
        </fieldset>
        <fieldset disabled={busy || locked || stale} className="staff-fields">
          <legend>Смена и обед · Екатеринбург, UTC+5</legend>
          <div className="staff-field-grid">
            <label>
              Начало смены
              <input
                type="time"
                required
                value={settings.shift.start}
                onChange={(e) =>
                  change({ ...settings, shift: { ...settings.shift, start: e.target.value } })
                }
              />
            </label>
            <label>
              Конец смены
              <input
                type="time"
                required
                value={settings.shift.end}
                onChange={(e) =>
                  change({ ...settings, shift: { ...settings.shift, end: e.target.value } })
                }
              />
            </label>
          </div>
          <div className="staff-field-grid">
            {(['from', 'to'] as const).map((k) => (
              <label key={k}>
                {k === 'from' ? 'Начало обеда не раньше' : 'Начало обеда не позже'}
                <input
                  type="time"
                  required
                  value={settings.shift.break_window[k]}
                  onChange={(e) =>
                    change({
                      ...settings,
                      shift: {
                        ...settings.shift,
                        break_window: { ...settings.shift.break_window, [k]: e.target.value },
                      },
                    })
                  }
                />
              </label>
            ))}
          </div>
          <label>
            Длительность обеда, минут
            <input
              type="number"
              required
              min={0}
              max={1440}
              step={1}
              value={settings.shift.break_duration_min}
              onChange={(e) =>
                change({
                  ...settings,
                  shift: { ...settings.shift, break_duration_min: Number(e.target.value) },
                })
              }
            />
          </label>
        </fieldset>
        <fieldset disabled={busy || locked || stale} className="staff-fields">
          <legend>Оборудование</legend>
          <p className="muted">
            Начальный запас — при выходе на смену. Вместимость — запас после пополнения в СЦ.
          </p>
          {(['ONT', 'ROUTER', 'STB'] as const).map((item) => (
            <div className="staff-field-grid" key={item}>
              {(['equipment_start', 'equipment_capacity'] as const).map((k) => (
                <label key={k}>
                  {item} · {k === 'equipment_start' ? 'начальный запас' : 'вместимость'}
                  <input
                    type="number"
                    required
                    min={0}
                    max={1000000}
                    step={1}
                    value={settings[k][item]}
                    onChange={(e) =>
                      change({
                        ...settings,
                        [k]: { ...settings[k], [item]: Number(e.target.value) },
                      })
                    }
                  />
                </label>
              ))}
            </div>
          ))}
        </fieldset>
        <button className="secondary-button" disabled={busy || locked || stale || w.busy}>
          {preview.isPending ? 'Проверяем план…' : 'Предпросмотр изменений'}
        </button>
        {preview.error && (
          <p className="form-error" role="alert">
            {preview.error.message}
          </p>
        )}
        {preview.data && (
          <section
            className="assignment-impact staff-impact"
            aria-label="Влияние изменений сотрудника"
          >
            <h3>После сохранения</h3>
            <ul>
              {differences(
                preview.data.before,
                preview.data.after,
                preview.data.absent_before,
                preview.data.absent_after,
              ).map(([label, a, b]) => (
                <li key={label}>
                  <strong>{label}</strong>
                  <br />
                  {a} → {b}
                </li>
              ))}
            </ul>
            {preview.data.has_plan ? (
              <>
                <p>
                  Назначено визитов: {preview.data.visits_before} → {preview.data.visits_after}
                </p>
                <p
                  className={
                    preview.data.unassigned_after > preview.data.unassigned_before
                      ? 'staff-warning'
                      : undefined
                  }
                >
                  Без назначения: {preview.data.unassigned_before} → {preview.data.unassigned_after}
                </p>
                <p>
                  Изменение переездов: {preview.data.travel_delta_sec > 0 ? '+' : ''}
                  {(preview.data.travel_delta_sec / 60).toLocaleString('ru-RU', {
                    maximumFractionDigits: 1,
                  })}{' '}
                  мин.
                </p>
                <p>
                  Затронутых нарядов: {preview.data.changes.length}. Новые ограничения могут
                  потребовать переназначения или оставить наряд без исполнителя.
                </p>
                <ul>
                  {preview.data.changes.map((c) => (
                    <li key={c.order_id}>
                      <strong>{c.order_id}</strong>
                      <br />
                      {name(c.before_staff)}
                      {c.before_start ? ` · ${time(c.before_start)}–${time(c.before_end!)}` : ''}
                      <br />→ {name(c.after_staff, true)}
                      {c.after_start ? ` · ${time(c.after_start)}–${time(c.after_end!)}` : ''}
                      {c.reason && <p className="staff-warning">{c.reason}</p>}
                    </li>
                  ))}
                </ul>
                {!preview.data.changes.length && <p>Назначения и интервалы визитов сохранятся.</p>}
              </>
            ) : (
              <p>
                Первоначальный план ещё не построен. Настройки сохранятся и будут учтены при первом
                расчёте.
              </p>
            )}
          </section>
        )}
        {!locked && (
          <label>
            Причина изменения
            <textarea
              value={reason}
              minLength={3}
              maxLength={2000}
              rows={2}
              disabled={save.isPending}
              placeholder="Например: изменение смены или уточнение допуска"
              onChange={(e) => setReason(e.target.value)}
            />
          </label>
        )}
        {save.error && (
          <p className="form-error" role="alert">
            {save.error.message}
          </p>
        )}
        {!locked && (
          <button
            type="button"
            className="primary-button"
            disabled={!preview.data || busy || stale || reason.trim().length < 3 || w.busy}
            onClick={() => save.mutate()}
          >
            {save.isPending ? 'Сохраняем…' : 'Подтвердить изменения'}
          </button>
        )}
        {!!history.length && (
          <details className="assignment-rejections">
            <summary>История карточки · {history.length}</summary>
            {history.map((c) => (
              <article className="staff-history-record" key={c.request_id}>
                <p>
                  <strong>{c.actor_name}</strong> · {time(c.as_of)} ·{' '}
                  {c.plan_version ? `версия плана ${c.plan_version}` : 'до первого расчёта'}
                </p>
                <p>{c.reason}</p>
                <ul>
                  {differences(c.before, c.after, c.absent_before, c.absent_after).map(
                    ([label, a, b]) => (
                      <li key={label}>
                        {label}: {a} → {b}
                      </li>
                    ),
                  )}
                </ul>
                <p>Затронуто нарядов: {c.changes.length}</p>
              </article>
            ))}
          </details>
        )}
      </form>
    </Modal>
  );
}
