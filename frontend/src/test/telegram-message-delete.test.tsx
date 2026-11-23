import { fireEvent, screen, waitFor } from '@testing-library/react';
import { beforeEach, expect, it, vi } from 'vitest';
import TelegramMessages from '@/pages/telegram/TelegramMessages';
import { telegramApi } from '@/pages/telegram/api';
import { makeTestQueryClient, renderWithProviders } from './test-utils';

beforeEach(() => {
  vi.restoreAllMocks();
});

it('deletes an individual non-pending message and disables pending deletion', async () => {
  vi.spyOn(telegramApi, 'conversations').mockResolvedValue({
    items: [
      {
        tgId: 265758444,
        username: 'castaway27',
        name: 'Alice',
        clientId: 1,
        clientEmail: 'us-isp-castaway',
        lastMessageAt: 2000,
        unread: 0,
      },
    ],
    total: 1,
  });
  vi.spyOn(telegramApi, 'messages').mockResolvedValue({
    items: [
      {
        id: 11,
        tgId: 265758444,
        clientId: 1,
        direction: 'outgoing',
        kind: 'text',
        text: 'pending-msg',
        fileName: '',
        mime: '',
        fileSize: 0,
        sourceBot: 'user',
        deliveryBot: 'user',
        externalId: 0,
        replyId: 0,
        role: 'web',
        status: 'pending',
        attempts: 0,
        nextAttemptAt: 0,
        error: '',
        noticeId: 0,
        createdAt: 2000,
        updatedAt: 2000,
      },
      {
        id: 10,
        tgId: 265758444,
        clientId: 1,
        direction: 'incoming',
        kind: 'text',
        text: 'received-msg',
        fileName: '',
        mime: '',
        fileSize: 0,
        sourceBot: 'user',
        deliveryBot: 'user',
        externalId: 1,
        replyId: 0,
        role: 'chat',
        status: 'received',
        attempts: 0,
        nextAttemptAt: 0,
        error: '',
        noticeId: 0,
        createdAt: 1000,
        updatedAt: 1000,
      },
    ],
    total: 2,
  });
  vi.spyOn(telegramApi, 'read').mockResolvedValue({});
  const delMsg = vi.spyOn(telegramApi, 'deleteMessage').mockResolvedValue({ deletedId: 10 });

  const cache = makeTestQueryClient();
  const invalidate = vi.spyOn(cache, 'invalidateQueries');
  renderWithProviders(<TelegramMessages tgId={265758444} onSelect={vi.fn()} />, {
    queryClient: cache,
  });

  expect(await screen.findByText('received-msg')).toBeTruthy();
  const deleteButtons = screen.getAllByRole('button', { name: 'Delete' });
  expect(deleteButtons).toHaveLength(2);
  // Display order is reversed (oldest first), so index 0 is received (id 10) and index 1 is pending (id 11).
  expect((deleteButtons[0] as HTMLButtonElement).disabled).toBe(false);
  expect((deleteButtons[1] as HTMLButtonElement).disabled).toBe(true);

  fireEvent.click(deleteButtons[0]);
  fireEvent.click(await screen.findByRole('button', { name: 'OK' }));

  await waitFor(() => expect(delMsg).toHaveBeenCalledWith(10));
  expect(invalidate).toHaveBeenCalledWith({ queryKey: ['telegram'] });
  expect(await screen.findByText('Message deleted')).toBeTruthy();
});

it('clears selection when deleting active conversation and preserves pending count when kept', async () => {
  vi.spyOn(telegramApi, 'conversations').mockResolvedValue({
    items: [
      {
        tgId: 265758444,
        username: 'castaway27',
        name: 'Alice',
        clientId: 1,
        clientEmail: 'us-isp-castaway',
        lastMessageAt: 2000,
        unread: 0,
      },
    ],
    total: 1,
  });
  vi.spyOn(telegramApi, 'messages').mockResolvedValue({ items: [], total: 0 });
  const delConv = vi
    .spyOn(telegramApi, 'deleteConversation')
    .mockResolvedValueOnce({
      tgId: 265758444,
      deletedMessages: 4,
      preservedPending: 1,
      conversationDeleted: false,
    })
    .mockResolvedValueOnce({
      tgId: 265758444,
      deletedMessages: 1,
      preservedPending: 0,
      conversationDeleted: true,
    });

  const onSelect = vi.fn();
  const cache = makeTestQueryClient();
  renderWithProviders(<TelegramMessages tgId={265758444} onSelect={onSelect} />, {
    queryClient: cache,
  });

  await screen.findByText('us-isp-castaway');
  const clearButtons = screen.getAllByRole('button', { name: 'Delete conversation messages' });
  // Trigger from the conversation list row action button.
  fireEvent.click(clearButtons[0]);
  fireEvent.click(await screen.findByRole('button', { name: 'OK' }));

  await waitFor(() => expect(delConv).toHaveBeenNthCalledWith(1, 265758444));
  expect(onSelect).not.toHaveBeenCalled();
  expect(await screen.findByText('History cleared; 1 pending message(s) preserved')).toBeTruthy();

  // Trigger from the Thread card header button.
  fireEvent.click(clearButtons[1]);
  const okButtons = await screen.findAllByRole('button', { name: 'OK' });
  fireEvent.click(okButtons[okButtons.length - 1]);

  await waitFor(() => expect(delConv).toHaveBeenNthCalledWith(2, 265758444));
  expect(onSelect).toHaveBeenCalledWith(null);
  expect(await screen.findByText('Conversation messages deleted')).toBeTruthy();
});
