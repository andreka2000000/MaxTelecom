'use client';
import { Navigation, Phone, UserRound } from 'lucide-react';
import { useWorkspace } from './context';
import { locationPoints, time, type ViewOrder } from './model';
import { navigatorURL, phoneURL } from './execution-model';
import type { VisitFact } from './types';

export function VisitTools({ order, contact = true }: { order: ViewOrder; contact?: boolean }) {
  const d = useWorkspace().data!;
  const customer = d.contacts?.find((item) => item.order_id === order.source.order_id);
  const destination = navigatorURL(locationPoints(d)[order.source.building_id]);
  const phone = customer && phoneURL(customer.phone);
  return (
    <div className="visit-tools">
      {destination ? (
        <a
          className="secondary-button"
          href={destination}
          target="_blank"
          rel="noopener noreferrer"
        >
          <Navigation size={16} /> Открыть в навигаторе
        </a>
      ) : (
        <p className="muted">Координаты адреса не указаны — уточните точку у диспетчера.</p>
      )}
      {contact && (
        <div className="visit-contact">
          {customer ? (
            <>
              <strong>
                <UserRound size={15} /> {customer.name || 'Абонент'}
              </strong>
              {phone ? (
                <a className="visit-phone" href={phone}>
                  <Phone size={16} /> {customer.phone}
                </a>
              ) : (
                <span>{customer.phone || 'Телефон не указан'}</span>
              )}
              {customer.description && <p>{customer.description}</p>}
            </>
          ) : (
            <p className="muted">
              Контакт абонента не указан. При необходимости обратитесь к диспетчеру.
            </p>
          )}
        </div>
      )}
    </div>
  );
}

export function VisitTimes({ fact }: { fact?: VisitFact }) {
  if (!fact) return null;
  const times = [
    ['Выехал', fact.departed_at],
    ['Прибыл', fact.arrived_at],
    ['Начал работу', fact.started_at],
    ['Закончил работу', fact.completed_at],
  ].filter((entry) => entry[1]);
  if (!times.length) return null;
  return (
    <dl className="visit-times" aria-label="Фактическое время визита">
      {times.map(([label, value]) => (
        <div key={label}>
          <dt>{label}</dt>
          <dd>{time(value!)}</dd>
        </div>
      ))}
    </dl>
  );
}
