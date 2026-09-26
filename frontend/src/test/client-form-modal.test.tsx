import { describe, it, expect, vi } from 'vitest';
import { fireEvent, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

import ClientFormModal from '@/pages/clients/ClientFormModal';
import { renderWithProviders } from './test-utils';

// ClientFormModal reads server state via react-query (useFail2banStatusQuery),
// so it needs a QueryClientProvider on top of the shared ThemeProvider wrapper.
function renderModal() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  renderWithProviders(
    <QueryClientProvider client={queryClient}>
      <ClientFormModal
        open
        mode="add"
        client={null}
        inbounds={[]}
        save={vi.fn().mockResolvedValue(null)}
        onOpenChange={() => {}}
      />
    </QueryClientProvider>,
  );
}

function openCredentialsTab() {
  const tab = Array.from(document.querySelectorAll('.ant-tabs-tab')).find(
    (t) => (t.textContent ?? '').trim() === 'Credentials',
  );
  if (!tab) throw new Error('Credentials tab not found');
  fireEvent.click(tab);
}

function tooltipIconForLabel(label: string): HTMLElement {
  const labelEl = Array.from(document.querySelectorAll('.ant-form-item-label label')).find(
    (l) => (l.textContent ?? '').trim() === label,
  );
  const item = labelEl?.closest('.ant-form-item') as HTMLElement | null;
  if (!item) throw new Error(`Form item not found for label: ${label}`);
  const tip = item.querySelector('.ant-form-item-tooltip') as HTMLElement | null;
  if (!tip) throw new Error(`No tooltip on form item: ${label}`);
  return tip;
}

describe('ClientFormModal credential tooltips', () => {
  it('explains that the Password field is only consumed by Trojan/Shadowsocks', async () => {
    renderModal();
    openCredentialsTab();

    const tip = tooltipIconForLabel('Password');
    fireEvent.mouseEnter(tip);

    await waitFor(() => {
      expect(document.body.textContent).toContain(
        'Used by Trojan, Shadowsocks, and TUIC clients; ignored for VLESS, VMess, Hysteria, and WireGuard.',
      );
    });
  });

  it('explains that Hysteria Auth is the credential Hysteria actually uses', async () => {
    renderModal();
    openCredentialsTab();

    const tip = tooltipIconForLabel('Hysteria Auth');
    fireEvent.mouseEnter(tip);

    await waitFor(() => {
      expect(document.body.textContent).toContain(
        'Credential used only by Hysteria clients. Trojan and Shadowsocks use the Password field instead.',
      );
    });
  });
});

describe('ClientFormModal Traffic tab', () => {
  it('renders traffic tab with top card and inbound quotas table in edit mode', async () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const client = {
      id: 1,
      email: 'test@example.com',
      totalGB: 100 * 1024 * 1024 * 1024,
      enable: true,
      traffic: { up: 10 * 1024 * 1024 * 1024, down: 15 * 1024 * 1024 * 1024 },
      totalGBByInbound: { 1: 50 * 1024 * 1024 * 1024 },
      inboundTraffics: {
        1: {
          inboundId: 1,
          up: 5 * 1024 * 1024 * 1024,
          down: 10 * 1024 * 1024 * 1024,
          total: 50 * 1024 * 1024 * 1024,
          used: 15 * 1024 * 1024 * 1024,
          remained: 35 * 1024 * 1024 * 1024,
          depleted: false,
        },
      },
    };
    const inbounds = [{ id: 1, remark: 'Node-1', protocol: 'vless', port: 443, tag: 'in-vless' }];
    renderWithProviders(
      <QueryClientProvider client={queryClient}>
        <ClientFormModal
          open
          mode="edit"
          client={client}
          attachedIds={[1]}
          inbounds={inbounds}
          save={vi.fn().mockResolvedValue(null)}
          onOpenChange={() => {}}
        />
      </QueryClientProvider>,
    );

    const trafficTab = Array.from(document.querySelectorAll('.ant-tabs-tab')).find(
      (t) => (t.textContent ?? '').trim() === 'Traffic',
    );
    expect(trafficTab).toBeDefined();
    fireEvent.click(trafficTab!);

    await waitFor(() => {
      expect(document.body.textContent).toContain('Inbound Traffic Quotas');
      expect(document.body.textContent).toContain('Node-1');
      expect(document.body.textContent).toContain('15.00 GB');
      expect(document.body.textContent).toContain('35.00 GB');
    });
  });
});
