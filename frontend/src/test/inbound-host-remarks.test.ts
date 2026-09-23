import { describe, expect, it } from 'vitest';

import { buildHostRemarksByInboundId, formatRateLimit } from '@/pages/inbounds/list/helpers';

describe('buildHostRemarksByInboundId', () => {
  it('joins only host groups a client can reach (#6026)', () => {
    const map = buildHostRemarksByInboundId([
      { remark: 'USA IPv4', inboundIds: [1, 2], hosts: ['1.2.3.4:443'], isDisabled: false },
      { remark: 'USA IPv6', inboundIds: [1], hosts: ['[2001:db8::1]:443'], isDisabled: true },
      { remark: '', inboundIds: [2], hosts: ['', 'cdn.example.com:443'], isDisabled: false },
      { remark: 'USA IPv4', inboundIds: [2], hosts: ['5.6.7.8'] },
    ]);
    expect(map.get(1)).toEqual(['USA IPv4']);
    expect(map.get(2)).toEqual(['USA IPv4', 'cdn.example.com:443']);
    expect(map.has(3)).toBe(false);
  });
});

describe('formatRateLimit', () => {
  it('returns null when limits are zero or unset', () => {
    expect(formatRateLimit(0, 0)).toBeNull();
    expect(formatRateLimit(undefined, undefined)).toBeNull();
  });

  it('formats both inbound and client limits', () => {
    expect(formatRateLimit(100, 10)).toBe('⚡ 100M / 10M');
  });

  it('formats only inbound limit', () => {
    expect(formatRateLimit(100, 0)).toBe('⚡ 100M');
  });

  it('formats only client limit', () => {
    expect(formatRateLimit(0, 10)).toBe('⚡ ∞ / 10M');
  });
});
