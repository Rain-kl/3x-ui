import { fireEvent, screen, waitFor } from '@testing-library/react';
import { expect, it, vi } from 'vitest';
import TelegramManualBindingModal from '@/pages/telegram/TelegramManualBindingModal';
import { telegramApi } from '@/pages/telegram/api';
import { makeTestQueryClient, renderWithProviders } from './test-utils';

it('validates a numeric Telegram ID, preserves server errors and refreshes binding state on success', async () => {
  vi.spyOn(telegramApi, 'clients').mockResolvedValue([
    { id: 17, email: 'manual-alice', group: '' },
  ]);
  const bind = vi
    .spyOn(telegramApi, 'manualBind')
    .mockRejectedValueOnce(new Error('account or client is already bound'))
    .mockResolvedValueOnce({});
  const cache = makeTestQueryClient();
  const invalidate = vi.spyOn(cache, 'invalidateQueries');
  const close = vi.fn();
  renderWithProviders(<TelegramManualBindingModal onClose={close} />, { queryClient: cache });
  const select = screen.getByRole('combobox');
  fireEvent.mouseDown(select.parentElement ?? select);
  fireEvent.click(await screen.findByText('manual-alice'));
  const account = screen.getByLabelText('Telegram ID');
  fireEvent.change(account, { target: { value: '@alice' } });
  fireEvent.click(screen.getByRole('button', { name: 'Manual binding' }));
  expect(
    await screen.findByText('Enter a valid positive numeric Telegram account ID.'),
  ).toBeTruthy();
  expect(bind).not.toHaveBeenCalled();
  fireEvent.change(account, { target: { value: '123456789' } });
  fireEvent.click(screen.getByRole('button', { name: 'Manual binding' }));
  expect(await screen.findByText('account or client is already bound')).toBeTruthy();
  expect(close).not.toHaveBeenCalled();
  expect(bind).toHaveBeenCalledWith({ clientId: 17, tgId: 123456789 }, expect.anything());
  fireEvent.click(screen.getByRole('button', { name: 'Manual binding' }));
  await waitFor(() => expect(close).toHaveBeenCalledOnce());
  expect(invalidate).toHaveBeenCalledWith({ queryKey: ['telegram'] });
});
