import { describe, it, expect, vi } from 'vitest';
import { render, fireEvent, waitFor, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

import { ThemeProvider } from '@/hooks/useTheme';
import ClientFormModal from '@/pages/clients/ClientFormModal';
import type { ClientRecord, InboundOption } from '@/hooks/useClients';

function makeQC() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

const REALITY_INBOUND = {
  id: 4,
  port: 10443,
  protocol: 'vless',
  tag: 'in-10443-tcp',
  tlsFlowCapable: true,
  enable: true,
} as unknown as InboundOption;

const CLIENT = {
  email: 'testuser',
  flow: 'xtls-rprx-vision',
  uuid: '11111111-1111-1111-1111-111111111111',
  subId: 'subid123',
  enable: true,
} as unknown as ClientRecord;

function savedFlow(save: ReturnType<typeof vi.fn>): unknown {
  return (save.mock.calls[0][0] as Record<string, unknown>).flow;
}

function createdFlow(save: ReturnType<typeof vi.fn>): unknown {
  const payload = save.mock.calls[0][0] as { client: Record<string, unknown> };
  return payload.client.flow;
}

function pickSelectOption(labelText: string, optionText: string) {
  const label = Array.from(document.querySelectorAll('.ant-form-item-label label')).find(
    (l) => (l.textContent ?? '').trim() === labelText,
  );
  const item = label?.closest('.ant-form-item');
  const select = item?.querySelector('.ant-select') as HTMLElement | null;
  if (!select) throw new Error(`Select '${labelText}' not found`);
  fireEvent.mouseDown((select.querySelector('.ant-select-selector') ?? select) as HTMLElement);
  const option = Array.from(document.querySelectorAll('.ant-select-item-option')).find(
    (o) => (o.getAttribute('title') ?? o.textContent ?? '').trim() === optionText,
  );
  if (!option) throw new Error(`Option '${optionText}' not found for '${labelText}'`);
  fireEvent.click(option);
  fireEvent.keyDown(select, { key: 'Escape' });
}

function openCredentialsTab() {
  const tab = Array.from(document.querySelectorAll('.ant-tabs-tab')).find(
    (t) => (t.textContent ?? '').trim() === 'Credentials',
  );
  if (!tab) throw new Error('Credentials tab not found');
  fireEvent.click(tab);
}

function renderAddModal(inbounds: InboundOption[]) {
  const qc = makeQC();
  const save = vi.fn().mockResolvedValue({ success: true });
  render(
    <ThemeProvider>
      <QueryClientProvider client={qc}>
        <ClientFormModal
          open
          mode="add"
          client={null}
          inbounds={inbounds}
          save={save}
          onOpenChange={() => {}}
        />
      </QueryClientProvider>
    </ThemeProvider>,
  );
  return save;
}

function clickCreate() {
  const button = document.querySelector('.ant-modal-footer .ant-btn-primary') as HTMLElement | null;
  if (!button) throw new Error('Create button missing');
  fireEvent.click(button);
}

describe('ClientFormModal — Vision flow preservation', () => {
  it('keeps xtls-rprx-vision with a stable Reality inbound', async () => {
    const qc = makeQC();
    const save = vi.fn().mockResolvedValue({ success: true });
    render(
      <ThemeProvider>
        <QueryClientProvider client={qc}>
          <ClientFormModal
            open
            mode="edit"
            client={CLIENT}
            inbounds={[REALITY_INBOUND]}
            attachedIds={[4]}
            save={save}
            onOpenChange={() => {}}
          />
        </QueryClientProvider>
      </ThemeProvider>,
    );
    fireEvent.click(await screen.findByRole('button', { name: /save/i }));
    await waitFor(() => expect(save).toHaveBeenCalled());
    expect(savedFlow(save)).toBe('xtls-rprx-vision');
  });

  it('does not drop a selected Vision flow while the inbound options momentarily reload', async () => {
    const qc = makeQC();
    const save = vi.fn().mockResolvedValue({ success: true });
    const tree = (inbounds: InboundOption[]) => (
      <ThemeProvider>
        <QueryClientProvider client={qc}>
          <ClientFormModal
            open
            mode="edit"
            client={CLIENT}
            inbounds={inbounds}
            attachedIds={[4]}
            save={save}
            onOpenChange={() => {}}
          />
        </QueryClientProvider>
      </ThemeProvider>
    );
    // Options loaded -> reloading (inboundOptionsQuery.data ?? [] === []) -> loaded again.
    const { rerender } = render(tree([REALITY_INBOUND]));
    await screen.findByRole('button', { name: /save/i });
    rerender(tree([]));
    rerender(tree([REALITY_INBOUND]));

    fireEvent.click(await screen.findByRole('button', { name: /save/i }));
    await waitFor(() => expect(save).toHaveBeenCalled());
    expect(savedFlow(save)).toBe('xtls-rprx-vision');
  });
});

describe('ClientFormModal — new client Vision default', () => {
  it('defaults a new client to xtls-rprx-vision on a flow-capable inbound', async () => {
    const save = renderAddModal([REALITY_INBOUND]);
    pickSelectOption('Attached inbounds', 'in-10443-tcp');
    clickCreate();
    await waitFor(() => expect(save).toHaveBeenCalled());
    expect(createdFlow(save)).toBe('xtls-rprx-vision');
  });

  it('keeps an explicit none choice instead of re-defaulting', async () => {
    const save = renderAddModal([REALITY_INBOUND]);
    pickSelectOption('Attached inbounds', 'in-10443-tcp');
    openCredentialsTab();
    await waitFor(() => expect(document.body.textContent).toContain('xtls-rprx-vision'));
    pickSelectOption('Flow', 'None');
    clickCreate();
    await waitFor(() => expect(save).toHaveBeenCalled());
    expect(createdFlow(save)).toBe('');
  });

  it('leaves the flow empty when the inbound is not flow-capable', async () => {
    const plain = {
      ...REALITY_INBOUND,
      id: 5,
      tag: 'in-plain-tcp',
      tlsFlowCapable: false,
    } as unknown as InboundOption;
    const save = renderAddModal([plain]);
    pickSelectOption('Attached inbounds', 'in-plain-tcp');
    clickCreate();
    await waitFor(() => expect(save).toHaveBeenCalled());
    expect(createdFlow(save)).toBe('');
  });

  it('does not fill Vision when editing a client whose flow is empty', async () => {
    const save = vi.fn().mockResolvedValue({ success: true });
    const qc = makeQC();
    render(
      <ThemeProvider>
        <QueryClientProvider client={qc}>
          <ClientFormModal
            open
            mode="edit"
            client={{ ...CLIENT, flow: '' } as unknown as ClientRecord}
            inbounds={[REALITY_INBOUND]}
            attachedIds={[4]}
            save={save}
            onOpenChange={() => {}}
          />
        </QueryClientProvider>
      </ThemeProvider>,
    );
    fireEvent.click(await screen.findByRole('button', { name: /save/i }));
    await waitFor(() => expect(save).toHaveBeenCalled());
    expect(savedFlow(save)).toBe('');
  });
});
