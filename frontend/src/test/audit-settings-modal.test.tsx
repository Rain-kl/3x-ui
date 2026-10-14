import { fireEvent, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';

import { HttpUtil } from '@/utils';
import AuditSettingsModal from '@/pages/audit/AuditSettingsModal';
import type { AuditRetention } from '@/schemas/audit';
import { renderWithProviders } from './test-utils';

const policy: AuditRetention = {
  detailHours: 24,
  summaryDays: 30,
  connectionRateLimit: 10,
  siteRateLimit: 1,
};

function renderModal() {
  const onClose = vi.fn();
  const onChanged = vi.fn();
  const view = renderWithProviders(
    <AuditSettingsModal policy={policy} onClose={onClose} onChanged={onChanged} />,
  );
  return { ...view, onClose, onChanged };
}

describe('AuditSettingsModal', () => {
  it('saves the retention policy as JSON', async () => {
    const user = userEvent.setup();
    const post = vi.spyOn(HttpUtil, 'post').mockResolvedValue({ success: true, obj: {} } as never);
    const { onChanged } = renderModal();

    fireEvent.change(screen.getAllByRole('spinbutton')[0], { target: { value: '36' } });
    await user.click(screen.getByRole('button', { name: 'Save audit settings' }));

    await waitFor(() =>
      expect(post).toHaveBeenCalledWith(
        '/panel/api/audit/settings',
        { ...policy, detailHours: 36 },
        expect.objectContaining({ headers: { 'Content-Type': 'application/json' }, silent: true }),
      ),
    );
    expect(onChanged).toHaveBeenCalledWith({ ...policy, detailHours: 36 });
  });

  it('confirms clear-all before posting the destructive request', async () => {
    const user = userEvent.setup();
    const post = vi.spyOn(HttpUtil, 'post').mockResolvedValue({ success: true, obj: {} } as never);
    renderModal();

    await user.click(screen.getByRole('button', { name: 'Clear all logs' }));
    expect(
      screen.getByText(/Permanently delete all visit details and daily summaries/),
    ).toBeTruthy();
    expect(post).not.toHaveBeenCalled();

    const confirms = screen.getAllByRole('button', { name: 'Confirm' });
    await user.click(confirms.at(-1)!);
    await waitFor(() =>
      expect(post).toHaveBeenCalledWith(
        '/panel/api/audit/clear',
        { olderThanDays: 0 },
        expect.objectContaining({ headers: { 'Content-Type': 'application/json' }, silent: true }),
      ),
    );
  });

  it('shows a server failure without applying the policy', async () => {
    const user = userEvent.setup();
    vi.spyOn(HttpUtil, 'post').mockResolvedValue({
      success: false,
      msg: 'database unavailable',
    } as never);
    const { onChanged } = renderModal();

    fireEvent.change(screen.getAllByRole('spinbutton')[0], { target: { value: '36' } });
    await user.click(screen.getByRole('button', { name: 'Save audit settings' }));

    expect(await screen.findByText('database unavailable')).toBeTruthy();
    expect(onChanged).not.toHaveBeenCalled();
  });
});
