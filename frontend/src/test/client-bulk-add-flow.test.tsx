import { describe, expect, it, vi } from 'vitest';
import { fireEvent, screen, waitFor } from '@testing-library/react';

import ClientBulkAddModal from '@/pages/clients/ClientBulkAddModal';
import { Msg } from '@/utils';
import { chooseSelectOption, renderWithProviders } from './test-utils';

const { bulkCreate } = vi.hoisted(() => ({ bulkCreate: vi.fn() }));
vi.mock('@/hooks/useClients', () => ({ useClients: () => ({ bulkCreate }) }));

const REALITY = { id: 1, protocol: 'vless', tag: 'reality', tlsFlowCapable: true };
const PLAIN = { id: 1, protocol: 'vless', tag: 'plain', tlsFlowCapable: false };

function renderModal(inbounds: (typeof REALITY)[]) {
  renderWithProviders(<ClientBulkAddModal open inbounds={inbounds} onOpenChange={() => {}} />);
}

function submit() {
  const button = document.querySelector('.ant-modal-footer .ant-btn-primary') as HTMLElement | null;
  if (!button) throw new Error('Create button missing');
  fireEvent.click(button);
}

function selectAll() {
  fireEvent.click(screen.getByRole('button', { name: 'Select all' }));
}

function chooseFlowOption(optionText: string) {
  const label = Array.from(document.querySelectorAll('.ant-form-item-label label')).find(
    (l) => (l.textContent ?? '').trim() === 'Flow',
  );
  const id = label?.getAttribute('for');
  if (!id) throw new Error('Flow field id missing');
  chooseSelectOption(id, optionText);
}

describe('ClientBulkAddModal — new client Vision default', () => {
  it('defaults bulk-created clients to xtls-rprx-vision on a flow-capable inbound', async () => {
    bulkCreate.mockResolvedValue(new Msg(true, '', { created: 1, skipped: [] }));
    renderModal([REALITY]);
    selectAll();
    await waitFor(() => expect(document.body.textContent).toContain('xtls-rprx-vision'));
    submit();
    await waitFor(() =>
      expect(bulkCreate).toHaveBeenCalledWith([
        expect.objectContaining({
          client: expect.objectContaining({ flow: 'xtls-rprx-vision' }),
        }),
      ]),
    );
  });

  it('keeps an explicit none choice instead of re-defaulting', async () => {
    bulkCreate.mockResolvedValue(new Msg(true, '', { created: 1, skipped: [] }));
    renderModal([REALITY]);
    selectAll();
    await waitFor(() => expect(document.body.textContent).toContain('xtls-rprx-vision'));
    chooseFlowOption('None');
    submit();
    await waitFor(() =>
      expect(bulkCreate).toHaveBeenCalledWith([
        expect.objectContaining({ client: expect.objectContaining({ flow: '' }) }),
      ]),
    );
  });

  it('leaves the flow empty when the inbound is not flow-capable', async () => {
    bulkCreate.mockResolvedValue(new Msg(true, '', { created: 1, skipped: [] }));
    renderModal([PLAIN]);
    selectAll();
    submit();
    await waitFor(() =>
      expect(bulkCreate).toHaveBeenCalledWith([
        expect.objectContaining({ client: expect.objectContaining({ flow: '' }) }),
      ]),
    );
  });
});
