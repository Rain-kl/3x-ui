import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

import { QueryClientProvider } from '@tanstack/react-query';
import { ThemeProvider } from '@/hooks/useTheme';
import { HttpUtil } from '@/utils';
import AuditPage from '@/pages/audit/AuditPage';
import AuditSitesModal from '@/pages/audit/AuditSitesModal';
import { makeTestQueryClient, renderWithProviders } from './test-utils';

vi.mock('@/layouts/AppSidebar', () => ({ default: () => null }));

const filters = { hours: 168, day: '2026-10-12', nodeId: 7, client: 'alice', site: 'example.com' };

function sitePage(params: unknown) {
  const { page, pageSize } = params as { page: number; pageSize: number };
  return {
    success: true,
    obj: {
      items: [
        {
          name: `server-page-${page}-${pageSize}.example`,
          connections: 5,
          uplink: 10,
          downlink: 20,
          measured: 4,
        },
      ],
      total: 220,
      page,
      pageSize,
      centralized: false,
    },
  };
}

async function chooseSize(size: number) {
  const select = await screen.findByRole('combobox');
  fireEvent.mouseDown(select.parentElement ?? select);
  fireEvent.click(await screen.findByText(`${size} / page`));
}

describe('Audit full site rankings', () => {
  it('requests real pages with all filters, supports 20/50/100 and resets page on filter changes', async () => {
    const get = vi.spyOn(HttpUtil, 'get').mockImplementation(async (url: string, params) => {
      expect(url).toBe('/panel/api/audit/sites');
      return sitePage(params) as never;
    });
    const close = vi.fn();
    const queryClient = makeTestQueryClient();
    const view = renderWithProviders(<AuditSitesModal filters={filters} onClose={close} />, {
      queryClient,
    });
    expect(await screen.findByText('server-page-1-20.example')).toBeTruthy();
    expect(get.mock.calls[0][1]).toEqual({ ...filters, page: 1, pageSize: 20 });

    fireEvent.click(screen.getByTitle('2'));
    expect(await screen.findByText('server-page-2-20.example')).toBeTruthy();
    expect(get.mock.calls.at(-1)?.[1]).toEqual({ ...filters, page: 2, pageSize: 20 });
    await chooseSize(50);
    expect(await screen.findByText('server-page-1-50.example')).toBeTruthy();
    expect(get.mock.calls.at(-1)?.[1]).toEqual({ ...filters, page: 1, pageSize: 50 });
    fireEvent.click(screen.getByTitle('2'));
    expect(await screen.findByText('server-page-2-50.example')).toBeTruthy();

    const changed = { ...filters, hours: 24, day: '', nodeId: -1, client: 'bob', site: '' };
    // Keep the same providers and query cache while changing the parent filters.
    view.rerender(
      <QueryClientProvider client={queryClient}>
        <ThemeProvider>
          <AuditSitesModal filters={changed} onClose={close} />
        </ThemeProvider>
      </QueryClientProvider>,
    );
    await waitFor(() =>
      expect(get.mock.calls.at(-1)?.[1]).toEqual({ ...changed, page: 1, pageSize: 50 }),
    );
    expect(
      get.mock.calls.some(
        ([, params]) =>
          (params as typeof changed & { page: number }).client === 'bob' &&
          (params as { page: number }).page !== 1,
      ),
    ).toBe(false);
    expect(await screen.findByText('server-page-1-50.example')).toBeTruthy();
    await chooseSize(100);
    expect(await screen.findByText('server-page-1-100.example')).toBeTruthy();
    expect(get.mock.calls.at(-1)?.[1]).toEqual({ ...changed, page: 1, pageSize: 100 });
  });

  it('loads only after clicking More in the dashboard title and follows applied client/site filters', async () => {
    const get = vi.spyOn(HttpUtil, 'get').mockImplementation(async (url: string, params) => {
      if (url.endsWith('/audit/rates'))
        return {
          success: true,
          obj: {
            windowStart: '2026-10-14T00:00:00Z',
            windowEnd: '2026-10-14T00:01:00Z',
            current: { connections: 0, sites: 0 },
            alerts: [],
          },
        } as never;
      if (url.endsWith('/audit/rates/history'))
        return {
          success: true,
          obj: {
            windowStart: '2026-10-13T00:00:00Z',
            windowEnd: '2026-10-14T00:00:00Z',
            availableFrom: '2026-10-14T00:00:00Z',
            points: [],
          },
        } as never;
      if (url.endsWith('/audit/settings'))
        return { success: true, obj: { detailHours: 24, summaryDays: 30 } } as never;
      if (url.endsWith('/audit/summary'))
        return {
          success: true,
          obj: {
            connections: 0,
            uplink: 0,
            downlink: 0,
            measured: 0,
            sites: [],
            clients: [],
            alerts: [],
            hours: [],
            events: [],
            total: 0,
            nodes: [],
          },
        } as never;
      if (url.endsWith('/audit/sites')) return sitePage(params) as never;
      throw new Error(`Unexpected request: ${url}`);
    });
    renderWithProviders(<AuditPage />);
    const more = await screen.findByRole('button', { name: /^more$/i });
    expect(more.closest('.ant-card-extra')).toBeTruthy();
    expect(get.mock.calls.some(([url]) => url.endsWith('/audit/sites'))).toBe(false);
    const client = screen.getByPlaceholderText('Filter client email');
    fireEvent.change(client, { target: { value: 'alice' } });
    fireEvent.keyDown(client, { key: 'Enter', code: 'Enter' });
    const site = screen.getByPlaceholderText('Filter site');
    fireEvent.change(site, { target: { value: 'example.com' } });
    fireEvent.keyDown(site, { key: 'Enter', code: 'Enter' });
    await waitFor(() =>
      expect(
        get.mock.calls.some(
          ([url, params]) =>
            url.endsWith('/audit/summary') &&
            (params as { client?: string; site?: string })?.client === 'alice' &&
            (params as { site?: string })?.site === 'example.com',
        ),
      ).toBe(true),
    );
    fireEvent.click(await screen.findByRole('button', { name: /^more$/i }));
    expect(await screen.findByText('server-page-1-20.example')).toBeTruthy();
    const request = get.mock.calls.find(([url]) => url.endsWith('/audit/sites'));
    expect(request?.[1]).toEqual({
      hours: 24,
      day: '',
      nodeId: 0,
      client: 'alice',
      site: 'example.com',
      page: 1,
      pageSize: 20,
    });
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: /close/i }));
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  });
});

it('shows query failures with retry and handles an empty ranking', async () => {
  let fail = true;
  const get = vi.spyOn(HttpUtil, 'get').mockImplementation(async () =>
    fail
      ? ({ success: false, msg: 'ranking unavailable' } as never)
      : ({
          success: true,
          obj: { items: [], total: 0, page: 1, pageSize: 20, centralized: false },
        } as never),
  );
  renderWithProviders(<AuditSitesModal filters={filters} onClose={() => {}} />);
  expect(await screen.findByText('ranking unavailable')).toBeTruthy();
  fail = false;
  fireEvent.click(screen.getByRole('button', { name: /refresh/i }));
  await waitFor(() => expect(screen.queryByText('ranking unavailable')).toBeNull());
  expect(get).toHaveBeenCalledTimes(2);
  expect(screen.getByText('No data', { selector: '.ant-empty-description' })).toBeTruthy();
});
