import { fireEvent, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';

import RoutingTab from '@/pages/xray/routing/RoutingTab';
import type { XraySettingsValue } from '@/hooks/useXraySetting';
import { HttpUtil, Msg } from '@/utils';

import { renderWithProviders } from './test-utils';

const LOCAL_RULE = { type: 'field', outboundTag: 'local-direct', domain: ['geosite:cn'] };
const REMOTE_RULE = {
  type: 'field',
  outboundTag: 'remote-proxy',
  domain: ['geosite:google'],
};

function localSettings(): XraySettingsValue {
  return {
    routing: { rules: [LOCAL_RULE] },
    outbounds: [{ tag: 'local-only', protocol: 'freedom' }],
  } as unknown as XraySettingsValue;
}

function renderTab() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithProviders(
    <QueryClientProvider client={queryClient}>
      <RoutingTab
        templateSettings={localSettings()}
        setTemplateSettings={vi.fn()}
        inboundTags={[]}
        clientReverseTags={[]}
        isMobile={false}
      />
    </QueryClientProvider>,
  );
}

async function openRulesTab() {
  fireEvent.click(screen.getByRole('tab', { name: /Routing Rules/ }));
  await screen.findByRole('combobox', { name: 'Node' });
}

async function selectWorkerNode() {
  const select = screen.getByRole('combobox', { name: 'Node' });
  fireEvent.mouseDown(select);
  const option = await screen.findByText(/worker \(10\.0\.0\.2\)/);
  fireEvent.click(option);
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe('RoutingTab remote node rules', () => {
  it('shows the child node rules returned as a JSON-string xray/ envelope', async () => {
    vi.spyOn(HttpUtil, 'get').mockImplementation(async (url: string) => {
      if (url.includes('/panel/api/nodes/list')) {
        return new Msg(true, '', [{ id: 7, name: 'worker', address: '10.0.0.2' }]);
      }
      if (url.includes('/panel/api/inbounds/options')) {
        return new Msg(true, '', []);
      }
      return new Msg(true, '', {});
    });
    vi.spyOn(HttpUtil, 'post').mockImplementation(async (url: string) => {
      if (url === '/panel/api/xray/') {
        return new Msg(
          true,
          '',
          JSON.stringify({
            xraySetting: { routing: { rules: [REMOTE_RULE] } },
            inboundTags: [],
            clientReverseTags: [],
            outboundTags: ['direct', 'remote-proxy', 'worker-extra'],
          }),
        );
      }
      return new Msg(true, '');
    });

    renderTab();
    await openRulesTab();
    expect(screen.getByText('local-direct')).toBeTruthy();

    await selectWorkerNode();

    await waitFor(() => {
      expect(screen.getByText('remote-proxy')).toBeTruthy();
    });
    expect(screen.queryByText('local-direct')).toBeNull();
  });

  it('saves the loaded child rules instead of an empty list that would overwrite them', async () => {
    const posts: Array<{ url: string; data: unknown }> = [];
    vi.spyOn(HttpUtil, 'get').mockImplementation(async (url: string) => {
      if (url.includes('/panel/api/nodes/list')) {
        return new Msg(true, '', [{ id: 7, name: 'worker', address: '10.0.0.2' }]);
      }
      if (url.includes('/panel/api/inbounds/options')) {
        return new Msg(true, '', []);
      }
      return new Msg(true, '', {});
    });
    vi.spyOn(HttpUtil, 'post').mockImplementation(async (url: string, data?: unknown) => {
      posts.push({ url, data });
      if (url === '/panel/api/xray/') {
        return new Msg(
          true,
          '',
          JSON.stringify({
            xraySetting: { routing: { rules: [REMOTE_RULE] } },
            inboundTags: [],
            clientReverseTags: [],
          }),
        );
      }
      return new Msg(true, 'ok');
    });

    renderTab();
    await openRulesTab();
    await selectWorkerNode();
    await waitFor(() => expect(screen.getByText('remote-proxy')).toBeTruthy());

    const save = screen.getByRole('button', { name: /save/i });
    expect((save as HTMLButtonElement).disabled).toBe(false);
    fireEvent.click(save);

    await waitFor(() => {
      expect(posts.some((p) => p.url === '/panel/api/xray/update')).toBe(true);
    });
    const update = posts.find((p) => p.url === '/panel/api/xray/update');
    const payload = update?.data as { nodeId?: number; xraySetting?: string };
    expect(payload.nodeId).toBe(7);
    const setting = JSON.parse(payload.xraySetting || '{}') as {
      routing?: { rules?: Array<{ outboundTag?: string }> };
    };
    expect(setting.routing?.rules?.map((r) => r.outboundTag)).toEqual(['remote-proxy']);
  });

  it('does not save child rules until the remote config has actually loaded', async () => {
    vi.spyOn(HttpUtil, 'get').mockImplementation(async (url: string) => {
      if (url.includes('/panel/api/nodes/list')) {
        return new Msg(true, '', [{ id: 7, name: 'worker', address: '10.0.0.2' }]);
      }
      return new Msg(true, '', []);
    });
    vi.spyOn(HttpUtil, 'post').mockImplementation(async (url: string) => {
      if (url === '/panel/api/xray/') {
        return new Msg(true, '', '{}');
      }
      return new Msg(true, '');
    });

    renderTab();
    await openRulesTab();
    await selectWorkerNode();

    await waitFor(() => {
      const save = screen.queryByRole('button', { name: /save/i });
      expect(save).toBeTruthy();
      expect((save as HTMLButtonElement).disabled).toBe(true);
    });
  });

  it('lists the child node outbound tags in the rule editor, not the local ones', async () => {
    vi.spyOn(HttpUtil, 'get').mockImplementation(async (url: string) => {
      if (url.includes('/panel/api/nodes/list')) {
        return new Msg(true, '', [{ id: 7, name: 'worker', address: '10.0.0.2' }]);
      }
      if (url.includes('/panel/api/inbounds/options')) {
        return new Msg(true, '', []);
      }
      return new Msg(true, '', {});
    });
    vi.spyOn(HttpUtil, 'post').mockImplementation(async (url: string) => {
      if (url === '/panel/api/xray/') {
        return new Msg(
          true,
          '',
          JSON.stringify({
            xraySetting: { routing: { rules: [REMOTE_RULE] } },
            inboundTags: [],
            clientReverseTags: [],
            outboundTags: ['direct', 'remote-proxy', 'worker-extra'],
          }),
        );
      }
      return new Msg(true, '');
    });

    renderTab();
    await openRulesTab();
    await selectWorkerNode();
    await waitFor(() => expect(screen.getByText('remote-proxy')).toBeTruthy());

    fireEvent.click(screen.getByRole('button', { name: /edit/i }));
    const outbound = await screen.findByLabelText('Outbound tag');
    fireEvent.mouseDown(outbound);

    await waitFor(() => {
      expect(screen.getByRole('option', { name: 'worker-extra' })).toBeTruthy();
    });
    expect(screen.queryByRole('option', { name: 'local-only' })).toBeNull();
  });

  it('saves child inbound tags without the central n<id>- prefix', async () => {
    const posts: Array<{ url: string; data: unknown }> = [];
    vi.spyOn(HttpUtil, 'get').mockImplementation(async (url: string) => {
      if (url.includes('/panel/api/nodes/list')) {
        return new Msg(true, '', [{ id: 7, name: 'worker', address: '10.0.0.2' }]);
      }
      if (url.includes('/panel/api/inbounds/options')) {
        return new Msg(true, '', [
          { id: 11, tag: 'n7-in-443-tcp', remark: 'worker in', nodeId: 7 },
        ]);
      }
      return new Msg(true, '', {});
    });
    vi.spyOn(HttpUtil, 'post').mockImplementation(async (url: string, data?: unknown) => {
      posts.push({ url, data });
      if (url === '/panel/api/xray/') {
        return new Msg(
          true,
          '',
          JSON.stringify({
            xraySetting: {
              routing: {
                rules: [{ ...REMOTE_RULE, inboundTag: ['n7-in-443-tcp'] }],
              },
            },
            inboundTags: ['n7-in-443-tcp'],
            clientReverseTags: [],
            outboundTags: ['direct'],
          }),
        );
      }
      return new Msg(true, 'ok');
    });

    renderTab();
    await openRulesTab();
    await selectWorkerNode();
    await waitFor(() => expect(screen.getByText('remote-proxy')).toBeTruthy());

    fireEvent.click(screen.getByRole('button', { name: /save/i }));
    await waitFor(() => {
      expect(posts.some((p) => p.url === '/panel/api/xray/update')).toBe(true);
    });
    const update = posts.find((p) => p.url === '/panel/api/xray/update');
    const payload = update?.data as { xraySetting?: string };
    const setting = JSON.parse(payload.xraySetting || '{}') as {
      routing?: { rules?: Array<{ inboundTag?: string[] }> };
    };
    expect(setting.routing?.rules?.[0]?.inboundTag).toEqual(['in-443-tcp']);
  });

  it('lists native child inbound tags in the rule editor, not the central n<id>- alias', async () => {
    vi.spyOn(HttpUtil, 'get').mockImplementation(async (url: string) => {
      if (url.includes('/panel/api/nodes/list')) {
        return new Msg(true, '', [{ id: 7, name: 'worker', address: '10.0.0.2' }]);
      }
      if (url.includes('/panel/api/inbounds/options')) {
        return new Msg(true, '', [
          { id: 11, tag: 'n7-in-443-tcp', remark: 'worker in', nodeId: 7 },
        ]);
      }
      return new Msg(true, '', {});
    });
    vi.spyOn(HttpUtil, 'post').mockImplementation(async (url: string) => {
      if (url === '/panel/api/xray/') {
        return new Msg(
          true,
          '',
          JSON.stringify({
            xraySetting: { routing: { rules: [REMOTE_RULE] } },
            inboundTags: ['n7-in-443-tcp'],
            clientReverseTags: [],
            outboundTags: ['direct'],
          }),
        );
      }
      return new Msg(true, '');
    });

    renderTab();
    await openRulesTab();
    await selectWorkerNode();
    await waitFor(() => expect(screen.getByText('remote-proxy')).toBeTruthy());

    fireEvent.click(screen.getByRole('button', { name: /edit/i }));
    const inbound = await screen.findByLabelText('Inbound tags');
    fireEvent.mouseDown(inbound);

    await waitFor(() => {
      expect(screen.getByRole('option', { name: 'in-443-tcp (worker in · worker)' })).toBeTruthy();
    });
    expect(screen.queryByRole('option', { name: /n7-in-443-tcp/ })).toBeNull();
  });
});
