import { redirect, notFound } from 'next/navigation';
import { currentUser } from '@/features/auth/server';
export default async function Page({ params }: { params: Promise<{ service: string }> }) {
  const { service } = await params;
  if (!['INTERNET', 'REPAIR', 'TV'].includes(service)) notFound();
  const user = await currentUser();
  if (!user) redirect(`/login?service=${service}`);
  if (user.role !== 'SUBSCRIBER') redirect('/app/forbidden');
  redirect(`/app/subscriber?service=${service}`);
}
