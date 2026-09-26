import { describe, expect, it } from 'vitest';

import { filterLimitedNodes, hasLimitedNodes, nodePercent } from '@/pages/sub/subPageModel';
import type { SubNodeLimit } from '@/pages/sub/subPageModel';

describe('subPageModel limited nodes', () => {
  const sampleNodes: SubNodeLimit[] = [
    {
      name: 'HongKong-VIP',
      used: '25.00GB',
      total: '50.00GB',
      remained: '25.00GB',
      percent: 50.0,
      depleted: false,
    },
    {
      name: 'Tokyo-Special',
      used: '100.00GB',
      total: '100.00GB',
      remained: '0.00B',
      percent: 100.0,
      depleted: true,
    },
  ];

  describe('filterLimitedNodes', () => {
    it('returns empty array for null or undefined', () => {
      expect(filterLimitedNodes(null)).toEqual([]);
      expect(filterLimitedNodes(undefined)).toEqual([]);
    });

    it('filters out invalid or empty entries', () => {
      const mixed = [
        ...sampleNodes,
        null as unknown as SubNodeLimit,
        { name: '', percent: 0 } as SubNodeLimit,
      ];
      const res = filterLimitedNodes(mixed);
      expect(res.length).toBe(3);
    });

    it('returns all valid limited nodes', () => {
      expect(filterLimitedNodes(sampleNodes)).toHaveLength(2);
    });
  });

  describe('hasLimitedNodes', () => {
    it('returns false for empty or non-array', () => {
      expect(hasLimitedNodes(null)).toBe(false);
      expect(hasLimitedNodes(undefined)).toBe(false);
      expect(hasLimitedNodes([])).toBe(false);
    });

    it('returns true when non-empty array exists', () => {
      expect(hasLimitedNodes(sampleNodes)).toBe(true);
    });
  });

  describe('nodePercent', () => {
    it('returns 0 for NaN or Infinity', () => {
      expect(nodePercent(NaN)).toBe(0);
      expect(nodePercent(Infinity)).toBe(0);
    });

    it('clamps negative percent to 0', () => {
      expect(nodePercent(-10)).toBe(0);
    });

    it('clamps overflow percent to 100', () => {
      expect(nodePercent(145.6)).toBe(100);
    });

    it('rounds float percent to nearest integer', () => {
      expect(nodePercent(49.6)).toBe(50);
      expect(nodePercent(49.2)).toBe(49);
    });
  });
});
