import { expect, it } from 'vitest';
import { screen } from '@testing-library/react';
import SubMixedNodes from '@/pages/sub/SubMixedNodes';
import { renderWithProviders } from './test-utils';

it('shows mixed credentials and both manual URLs with a valid IPv6 endpoint', () => {
  renderWithProviders(
    <SubMixedNodes
      nodes={[
        {
          name: 'Mixed child',
          address: '2001:db8::1',
          port: 1080,
          username: 'my-login',
          password: 'my-password',
          http: 'http://my-login:my-password@[2001:db8::1]:1080',
          socks5: 'socks5://my-login:my-password@[2001:db8::1]:1080',
        },
      ]}
    />,
  );
  expect(screen.getByText('Mixed child')).toBeTruthy();
  expect(screen.getByText('[2001:db8::1]:1080')).toBeTruthy();
  expect(screen.getByText('my-login')).toBeTruthy();
  expect(screen.getByText('my-password')).toBeTruthy();
  expect(screen.getByText(/mixed is excluded from automatic subscriptions/)).toBeTruthy();
  expect(screen.getByText('http://my-login:my-password@[2001:db8::1]:1080')).toBeTruthy();
  expect(screen.getByText('socks5://my-login:my-password@[2001:db8::1]:1080')).toBeTruthy();
});
