import { requireUser } from '@/features/auth/server';
import { RequestsPage } from '@/features/workspace/requests';
export const metadata = { title: 'Личный кабинет' };
export default async function Page({
  searchParams,
}: {
  searchParams: Promise<{ service?: string }>;
}) {
  await requireUser(['SUBSCRIBER']);
  const { service } = await searchParams;
  const selected = service && ['INTERNET', 'REPAIR', 'TV'].includes(service) ? service : undefined;
  return <RequestsPage key={selected || 'default'} initialService={selected} />;
}
