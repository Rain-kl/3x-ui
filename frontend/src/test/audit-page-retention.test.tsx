import { fireEvent, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

import { HttpUtil } from '@/utils';
import AuditPage from '@/pages/audit/AuditPage';
import { renderWithProviders } from './test-utils';

vi.mock('@/layouts/AppSidebar', () => ({ default: () => null }));

describe('AuditPage retention windows', () => {
  it('limits available windows to the configured detail and summary retention', async () => {
    vi.spyOn(HttpUtil, 'get').mockImplementation(async (url: string) => {
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
      if (url.endsWith('/audit/settings')) {
        return { success: true, obj: { detailHours: 6, summaryDays: 3 } } as never;
      }
      if (url.endsWith('/audit/summary')) {
        return {
          success: true,
          obj: {
            centralized: false,
            dailyTotal: 0,
            days: [],
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
      }
      return { success: true, obj: {} } as never;
    });

    renderWithProviders(<AuditPage />);
    await waitFor(() =>
      expect(screen.getByText('Audit settings').closest('button')?.hasAttribute('disabled')).toBe(
        false,
      ),
    );
    const hoursInput = screen.getAllByRole('combobox')[0];
    fireEvent.mouseDown(hoursInput.parentElement ?? hoursInput);

    expect(await screen.findByText('3 Hour')).toBeTruthy();
    expect(screen.getAllByText('6 Hour')).toHaveLength(2);
    expect(screen.getByText('3 Day')).toBeTruthy();
    expect(screen.queryByText('12 Hour')).toBeNull();
    expect(screen.queryByText('7 Day')).toBeNull();
  });

  it('disables today minus N days and allows the Nth retained UTC date', async () => {
    vi.spyOn(HttpUtil, 'get').mockImplementation(async (url: string) => {
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
      if (url.endsWith('/audit/settings')) {
        return { success: true, obj: { detailHours: 6, summaryDays: 3 } } as never;
      }
      if (url.endsWith('/audit/summary')) {
        return {
          success: true,
          obj: {
            centralized: false,
            dailyTotal: 0,
            days: [],
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
      }
      return { success: true, obj: {} } as never;
    });

    renderWithProviders(<AuditPage />);
    await waitFor(() =>
      expect(screen.getByText('Audit settings').closest('button')?.hasAttribute('disabled')).toBe(
        false,
      ),
    );

    const today = new Date().toISOString().slice(0, 10);
    const cutoff = new Date(Date.now() - 3 * 86_400_000).toISOString().slice(0, 10);
    const oldestIncluded = new Date(Date.now() - 2 * 86_400_000).toISOString().slice(0, 10);
    const datePicker = document.querySelector('.ant-picker')!;
    fireEvent.mouseDown(datePicker);
    fireEvent.click(datePicker.querySelector('input')!);

    const disabledDate = await screen.findByTitle(cutoff);
    const includedDate = screen.getByTitle(oldestIncluded);
    expect(disabledDate.closest('td')?.classList.contains('ant-picker-cell-disabled')).toBe(true);
    expect(includedDate.closest('td')?.classList.contains('ant-picker-cell-disabled')).toBe(false);
    expect(
      screen.getByTitle(today).closest('td')?.classList.contains('ant-picker-cell-disabled'),
    ).toBe(false);
  });
});
