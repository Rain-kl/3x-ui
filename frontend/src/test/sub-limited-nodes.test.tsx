import { describe, expect, it } from 'vitest';
import { screen } from '@testing-library/react';

import SubLimitedNodes from '@/pages/sub/SubLimitedNodes';
import type { SubNodeLimit } from '@/pages/sub/subPageModel';
import { renderWithProviders } from './test-utils';

describe('<SubLimitedNodes />', () => {
  const nodes: SubNodeLimit[] = [
    {
      name: 'HongKong-VIP',
      used: '25.00GB',
      total: '50.00GB',
      remained: '25.00GB',
      percent: 50.0,
      depleted: false,
    },
    {
      name: 'Tokyo-Depleted',
      used: '100.00GB',
      total: '100.00GB',
      remained: '0.00B',
      percent: 100.0,
      depleted: true,
    },
  ];

  it('renders nothing when limitedNodes is empty or undefined', () => {
    const { container: c1 } = renderWithProviders(<SubLimitedNodes />);
    expect(c1.firstChild).toBeNull();

    const { container: c2 } = renderWithProviders(<SubLimitedNodes limitedNodes={[]} />);
    expect(c2.firstChild).toBeNull();
  });

  it('renders node names, usage, and depleted tag', () => {
    renderWithProviders(<SubLimitedNodes limitedNodes={nodes} />);

    expect(screen.getByText('HongKong-VIP')).toBeTruthy();
    expect(screen.getByText('Tokyo-Depleted')).toBeTruthy();
    expect(screen.getByText('25.00GB / 50.00GB')).toBeTruthy();
    expect(screen.getByText('100.00GB / 100.00GB')).toBeTruthy();
    expect(screen.getByText('Data used up')).toBeTruthy();
  });
});
