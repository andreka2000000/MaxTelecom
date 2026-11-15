import Link from 'next/link';
import { ArrowUpRight, Wifi, Wrench, Tv, ShieldCheck } from 'lucide-react';
import s from './landing.module.css';
const services = [
  {
    id: 'INTERNET',
    title: 'Подключение интернета',
    description:
      'Оставьте заявку на подключение домашнего интернета. Диспетчер уточнит адрес и согласует визит специалиста.',
    Icon: Wifi,
  },
  {
    id: 'REPAIR',
    title: 'Устранение неисправности',
    description:
      'Если связь пропала или работает нестабильно, сообщите о проблеме. Специалист проверит подключение и восстановит услугу.',
    Icon: Wrench,
  },
  {
    id: 'TV',
    title: 'Подключение телевидения',
    description:
      'Подайте заявку на подключение телевидения. Команда согласует необходимые работы и удобное окно визита.',
    Icon: Tv,
  },
];
export function RoleShowcase() {
  return (
    <>
      <div className={s.serviceGrid}>
        {services.map(({ id, title, description, Icon }) => (
          <article className={s.serviceCard} key={id}>
            <Icon size={28} />
            <h3>{title}</h3>
            <p>{description}</p>
            <Link href={`/order/${id}`} className={s.primary}>
              Заказать услугу <ArrowUpRight size={16} />
            </Link>
          </article>
        ))}
      </div>
      <p className={s.serviceAuth}>
        <ShieldCheck size={16} /> Для оформления заявки необходимо войти в аккаунт абонента.
      </p>
    </>
  );
}
