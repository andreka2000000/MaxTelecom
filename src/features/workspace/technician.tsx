'use client';
import { useMemo, useRef, useState } from 'react';
import { Clock3, MapPin, Route as RouteIcon } from 'lucide-react';
import { PageHeader } from '@/components/layout/page-header';
import { OperationNotice, useWorkspace, WorkspaceGate } from './context';
import { countText, locationPoints, ordersView, profileNames, staffName, time } from './model';
import { LiveMap } from './map';
import { OrderDetail, RouteStops, Timeline } from './panels';
import { EventDialog } from './forms';
import { RoadPanel } from './road-routing';
import { useIdentity } from '@/features/auth/context';
import { ExecutionPanel } from './execution';
import { nextVisit } from './execution-model';
import { VisitTimes, VisitTools } from './visit-tools';
export function LiveTechnician() {
  const w = useWorkspace();
  return (
    <>
      <PageHeader title="Маршрут мастера" subtitle="Адрес, время и следующий шаг" />
      <WorkspaceGate>
        <TechnicianContent key={w.date} />
      </WorkspaceGate>
    </>
  );
}
function TechnicianContent() {
  const w = useWorkspace();
  const d = w.data!;
  const user = useIdentity();
  const [selected, setSelected] = useState<string | null>(null);
  const [event, setEvent] = useState(false);
  const [detailOpen, setDetailOpen] = useState(false);
  const mapSection = useRef<HTMLDivElement>(null);
  const staffId = user.staff_id;
  const person = d.reference.staff.find((s) => s.staff_id === staffId)!;
  const route = d.plan?.routes.find((r) => r.staff_id === staffId);
  const all = useMemo(() => ordersView(d), [d]);
  const orders = all.filter((o) => o.staffId === staffId);
  const points = useMemo(() => locationPoints(d), [d]);
  const current = nextVisit(orders);
  const mapOrder = selected || current?.source.order_id;
  const showDetails = (id: string) => {
    setSelected(id);
    setDetailOpen(true);
  };
  const showOnMap = (id: string) => {
    setSelected(id);
    setDetailOpen(false);
    mapSection.current?.scrollIntoView({ behavior: 'smooth', block: 'center' });
  };
  const detail = all.find((o) => o.source.order_id === selected);
  if (!person)
    return (
      <div className="page-content">
        <section className="live-panel state-panel">
          <h2>Профиль сотрудника не найден в этой смене</h2>
          <p>Попросите диспетчера проверить привязку аккаунта к справочнику.</p>
        </section>
      </div>
    );
  return (
    <div className="page-content live-content">
      <div className="shift-toolbar">
        <span className="eyebrow">МОЙ МАРШРУТ · {staffId}</span>
        <span className="subtle-chip">
          {time(d.day.now)} · версия {d.plan?.plan_version || '—'}
        </span>
      </div>
      <OperationNotice />
      <section className="technician-hero live-panel">
        <span className="eyebrow">НА СВЯЗИ С КОМАНДОЙ</span>
        <h2>{staffName(person)}</h2>
        <p>
          {person.profiles.map((p) => profileNames[p]).join(' · ')} ·{' '}
          {person.technologies.join(' / ')}
        </p>
        <div className="technician-stats">
          <span>
            <RouteIcon size={17} />
            {countText(route?.totals?.visits || 0, 'визит', 'визита', 'визитов')}
          </span>
          <span>
            <Clock3 size={17} />
            {person.shift.start}–{person.shift.end}
          </span>
          <span>
            <MapPin size={17} />
            {
              d.reference.service_centers.find(
                (c) => c.service_center_id === person.service_center_id,
              )?.name
            }
          </span>
        </div>
      </section>
      <nav className="technician-shortcuts" aria-label="Разделы маршрута">
        <a href="#route-map">Карта и адрес</a>
        <a href="#my-tasks">Этапы визита</a>
        <a href="#schedule">Расписание</a>
      </nav>
      <div className="technician-grid">
        <div className="technician-route-column">
          <RoadPanel route={route} targetOrderId={current?.source.order_id} />
          <section className="live-panel">
            <div className="live-section-title">
              <div>
                <span className="eyebrow">
                  {current &&
                  ['В пути', 'Прибыл на объект', 'Работа начата'].includes(current.status)
                    ? 'ТЕКУЩЕЕ ЗАДАНИЕ'
                    : 'СЛЕДУЮЩЕЕ ЗАДАНИЕ'}
                </span>
                <h2>{current?.title || 'Нет предстоящих визитов'}</h2>
              </div>
            </div>
            {current && (
              <>
                <p className="next-address">{current.source.address}</p>
                <span className="state-badge violet">
                  {time(current.stop!.start)}–{time(current.stop!.end)}
                </span>
                <span className={`state-badge ${current.tone}`}>{current.status}</span>
                <p className="equipment-note">
                  Взять с собой:{' '}
                  {Object.entries(current.source.required_equipment)
                    .filter(([, n]) => n > 0)
                    .map(([k, n]) => `${k} × ${n}`)
                    .join(' · ') || 'Дополнительное оборудование не требуется'}
                </p>
                <button
                  className="primary-button"
                  onClick={() => showDetails(current.source.order_id)}
                >
                  Детали визита
                </button>
                <button
                  className="secondary-button"
                  onClick={() => showOnMap(current.source.order_id)}
                >
                  <MapPin size={16} /> Показать точку на карте
                </button>
                <VisitTools order={current} />
                <VisitTimes fact={d.facts?.find((f) => f.order_id === current.source.order_id)} />
                <a className="text-button" href="#my-tasks">
                  К этапам выполнения визита
                </a>
              </>
            )}
            <details className="route-list-collapse">
              <summary>Все остановки смены</summary>
              <RouteStops route={route} onSelect={showDetails} />
            </details>
            <button
              className="secondary-button"
              disabled={w.busy || !d.plan}
              onClick={() => {
                w.event.reset();
                setEvent(true);
              }}
            >
              Сообщить об изменении
            </button>
          </section>
        </div>
        <div ref={mapSection} className="technician-map-section" id="route-map">
          <LiveMap
            orders={orders}
            points={points}
            route={route}
            selected={mapOrder}
            onSelect={showDetails}
          />
          <p className="muted">
            Точки пронумерованы по порядку визитов. Светлая линия — путь к выбранной точке.
            Положение мастера не отслеживается.
          </p>
        </div>
      </div>
      <ExecutionPanel key={d.day.date} onMap={showOnMap} />
      <section id="schedule">
        <Timeline staffId={staffId} onSelect={showDetails} />
      </section>
      {detail && detailOpen && !event && (
        <OrderDetail
          order={detail}
          onClose={() => setDetailOpen(false)}
          onEvent={() => {
            w.event.reset();
            setEvent(true);
          }}
        />
      )}
      {event && (
        <EventDialog
          kind={detail ? 'VISIT_DELAYED' : 'STAFF_UNAVAILABLE'}
          orderId={detail?.source.order_id}
          staffId={staffId}
          onClose={() => setEvent(false)}
        />
      )}
    </div>
  );
}
