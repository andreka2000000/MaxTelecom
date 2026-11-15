import { renderToString } from 'react-dom/server';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { expect, it, vi } from 'vitest';
import { RequestHandoff } from './request-handoff';

const { workspace } = vi.hoisted(() => ({
  workspace: {
    data: { day: { date: '2026-11-16', now: '2026-11-16T08:30:00+05:00' } },
    clock: { data: undefined as { mode: 'DEMO' | 'LIVE' } | undefined },
    refresh: vi.fn(),
    setDate: vi.fn(),
  },
}));
vi.mock('./context', () => ({ useOptionalWorkspace: () => workspace }));

it.each(['DEMO', 'LIVE', undefined] as const)(
  'shows the time jump only for confirmed DEMO mode: %s',
  (mode) => {
    workspace.clock.data = mode ? { mode } : undefined;
    const client = new QueryClient();
    client.setQueryData(
      ['handoff-accounts'],
      [{ username: 'master', staff_id: 'ST-001', disabled: false }],
    );
    const html = renderToString(
      <QueryClientProvider client={client}>
        <RequestHandoff
          order={{
            id: 'ORD-demo',
            date: '2026-11-16',
            staff_id: 'ST-001',
            state: 'ARRIVED',
            planned_start: '2026-11-16T14:00:00+05:00',
          }}
        />
      </QueryClientProvider>,
    );
    expect(html.includes('К началу визита')).toBe(mode === 'DEMO');
  },
);
