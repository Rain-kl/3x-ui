import { fireEvent, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

import { HttpUtil } from '@/utils';
import AuditPage from '@/pages/audit/AuditPage';
import type { AuditSummary } from '@/schemas/audit';
import { renderWithProviders } from './test-utils';

vi.mock('@/layouts/AppSidebar', () => ({ default: () => null }));

const events: AuditSummary['events'] = [
  [0, 'in-42342-tcp'],
  [7, 'in-42342-tcp'],
  [8, 'in-42342-tcp'],
  [7, 'empty'],
  [7, 'deleted'],
  [0, 'n7-literal'],
].map(([nodeId, inbound], index) => ({
  id: String(index),
  nodeId: Number(nodeId),
  inbound: String(inbound),
  time: '2026-10-14T00:00:00Z',
  outbound: 'direct',
  client: 'alice',
  source: '192.0.2.1',
  destination: 'example.com',
  host: 'example.com',
  site: 'example.com',
  uplink: 0,
  downlink: 0,
}));

const summary: AuditSummary = {
  centralized: false,
  dailyTotal: 0,
  days: [],
  connections: events.length,
  uplink: 0,
  downlink: 0,
  measured: events.length,
  sites: [],
  clients: [],
  alerts: [],
  hours: [],
  events,
  total: 200,
  nodes: [
    { id: 7, name: 'edge-a' },
    { id: 8, name: 'edge-b' },
  ],
};

const inbounds = [
  { id: 1, nodeId: null, tag: 'in-42342-tcp', remark: '本机入口' },
  { id: 2, nodeId: 7, tag: 'n7-in-42342-tcp', remark: '子节点 A' },
  { id: 3, nodeId: 8, tag: 'n8-in-42342-tcp', remark: '子节点 B' },
  { id: 4, nodeId: 7, tag: 'n7-empty', remark: '  ' },
  { id: 5, nodeId: null, tag: 'n7-literal', remark: '本机原始标签' },
];

function auditResponse(url: string) {
  if (url.endsWith('/audit/settings'))
    return { success: true, obj: { detailHours: 24, summaryDays: 30 } };
  if (url.endsWith('/audit/summary')) return { success: true, obj: summary };
  throw new Error(`Unexpected per-row or other request: ${url}`);
}

describe('Audit inbound remarks', () => {
  it('renders logs immediately, maps node-scoped remarks from one list, and reuses it across pages', async () => {
    let resolveOptions!: (value: never) => void;
    const options = new Promise<never>((resolve) => {
      resolveOptions = resolve;
    });
    const get = vi.spyOn(HttpUtil, 'get').mockImplementation(async (url: string) => {
      if (url.endsWith('/inbounds/options')) return options;
      return auditResponse(url) as never;
    });
    renderWithProviders(<AuditPage />);
    await waitFor(() => expect(screen.getAllByText('in-42342-tcp')).toHaveLength(3));
    resolveOptions({ success: true, obj: inbounds } as never);
    expect(await screen.findByText('in-42342-tcp（本机入口）')).toBeTruthy();
    expect(screen.getByText('in-42342-tcp（子节点 A）')).toBeTruthy();
    expect(screen.getByText('in-42342-tcp（子节点 B）')).toBeTruthy();
    expect(screen.getByText('n7-literal（本机原始标签）')).toBeTruthy();
    expect(screen.getByText('empty')).toBeTruthy();
    expect(screen.getByText('deleted')).toBeTruthy();
    fireEvent.click(screen.getByTitle('2'));
    await waitFor(() =>
      expect(
        get.mock.calls.some(
          ([url, params]) =>
            url.endsWith('/audit/summary') && (params as { offset?: number })?.offset === 100,
        ),
      ).toBe(true),
    );
    expect(get.mock.calls.filter(([url]) => url.includes('/inbounds/'))).toHaveLength(1);
    get.mockImplementation(async (url: string) => {
      if (url.endsWith('/inbounds/options')) {
        return {
          success: true,
          obj: inbounds.map((inbound) =>
            inbound.id === 2 ? { ...inbound, remark: '新的子节点备注' } : inbound,
          ),
        } as never;
      }
      return auditResponse(url) as never;
    });
    const refresh = screen.getByRole('button', { name: /Refresh/i });
    await waitFor(() => expect(refresh.classList.contains('ant-btn-loading')).toBe(false));
    fireEvent.click(refresh);
    expect(await screen.findByText('in-42342-tcp（新的子节点备注）')).toBeTruthy();
    expect(get.mock.calls.filter(([url]) => url.includes('/inbounds/'))).toHaveLength(2);
  });

  it('keeps raw tags and audit records visible when the inbound list fails', async () => {
    const get = vi.spyOn(HttpUtil, 'get').mockImplementation(async (url: string) => {
      if (url.endsWith('/inbounds/options')) return { success: false, msg: 'unavailable' } as never;
      return auditResponse(url) as never;
    });
    renderWithProviders(<AuditPage />);
    await waitFor(() => expect(screen.getAllByText('in-42342-tcp')).toHaveLength(3));
    expect(screen.getByText('deleted')).toBeTruthy();
    expect(get.mock.calls.filter(([url]) => url.includes('/inbounds/'))).toHaveLength(1);
  });
});
