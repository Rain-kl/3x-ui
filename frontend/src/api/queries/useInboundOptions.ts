import { useQuery } from '@tanstack/react-query';

import { HttpUtil } from '@/utils';
import { parseMsg } from '@/utils/zodValidate';
import { keys } from '@/api/queryKeys';
import { InboundOptionsSchema, type InboundOption } from '@/schemas/client';

async function fetchInboundOptions(): Promise<InboundOption[]> {
  const msg = await HttpUtil.get('/panel/api/inbounds/options', undefined, { silent: true });
  if (!msg?.success) throw new Error(msg?.msg || 'Failed to fetch inbound options');
  const validated = parseMsg(msg, InboundOptionsSchema, 'inbounds/options');
  return Array.isArray(validated.obj) ? validated.obj : [];
}

// FORK(audit-workbench): Audit history and child views should not fetch unused inbound metadata.
export function useInboundOptions(enabled = true) {
  return useQuery({
    queryKey: keys.inbounds.options(),
    queryFn: fetchInboundOptions,
    enabled,
    staleTime: Infinity,
  });
}
