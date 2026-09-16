import type { ComponentProps } from 'react';
import { fireEvent, screen, within } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

import NodeList from '@/pages/nodes/NodeList';
import type { NodeRecord } from '@/schemas/node';

import { renderWithProviders } from './test-utils';

const noop = () => {};

function nodes(): NodeRecord[] {
  return [
    { id: 1, name: 'online-node', enable: true, status: 'online', transitive: false },
    { id: 2, name: 'offline-node', enable: true, status: 'offline', transitive: false },
  ];
}

function renderList(overrides: Partial<ComponentProps<typeof NodeList>> = {}) {
  const onProbe = vi.fn();
  const onEdit = vi.fn();
  const onDelete = vi.fn();
  const onRestartNode = vi.fn();
  const onUpdateNode = vi.fn();
  const onUpdateSelected = vi.fn();
  const onRestartSelected = vi.fn();
  const onDeleteSelected = vi.fn();
  const onSelectionChange = vi.fn();
  renderWithProviders(
    <NodeList
      nodes={nodes()}
      isMobile={false}
      selectedIds={[]}
      onSelectionChange={onSelectionChange}
      onAdd={noop}
      onMtls={noop}
      onEdit={onEdit}
      onDelete={onDelete}
      onProbe={onProbe}
      onToggleEnable={noop}
      onUpdateNode={onUpdateNode}
      onRestartNode={onRestartNode}
      onUpdateSelected={onUpdateSelected}
      onRestartSelected={onRestartSelected}
      onDeleteSelected={onDeleteSelected}
      {...overrides}
    />,
  );
  return {
    onProbe,
    onEdit,
    onDelete,
    onRestartNode,
    onUpdateNode,
    onUpdateSelected,
    onRestartSelected,
    onDeleteSelected,
    onSelectionChange,
  };
}

describe('NodeList row and batch actions', () => {
  it('keeps probe, restart, edit and delete inside the more menu on desktop', () => {
    const { onProbe, onEdit, onDelete, onRestartNode } = renderList();

    expect(screen.queryByRole('button', { name: 'Probe Now' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Restart Panel' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Edit' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Delete' })).toBeNull();

    const more = screen.getAllByRole('button', { name: /more/i });
    expect(more.length).toBeGreaterThan(0);
    fireEvent.click(more[0]);

    fireEvent.click(screen.getByRole('menuitem', { name: /Probe Now/ }));
    expect(onProbe).toHaveBeenCalledTimes(1);

    fireEvent.click(more[0]);
    fireEvent.click(screen.getByRole('menuitem', { name: /Restart Panel/ }));
    expect(onRestartNode).toHaveBeenCalledTimes(1);

    fireEvent.click(more[0]);
    fireEvent.click(screen.getByRole('menuitem', { name: /Edit/ }));
    expect(onEdit).toHaveBeenCalledTimes(1);

    fireEvent.click(more[0]);
    fireEvent.click(screen.getByRole('menuitem', { name: /Delete/ }));
    expect(onDelete).toHaveBeenCalledTimes(1);
  });

  it('keeps the update-panel icon on an online enabled node', () => {
    const { onUpdateNode } = renderList();
    const update = screen.getByRole('button', { name: 'Update Panel' });
    fireEvent.click(update);
    expect(onUpdateNode).toHaveBeenCalledTimes(1);
  });

  it('lets an offline node be selected and shows batch delete, restart and update', () => {
    const onDeleteSelected = vi.fn();
    const onRestartSelected = vi.fn();
    const onUpdateSelected = vi.fn();
    renderList({
      selectedIds: [1, 2],
      onDeleteSelected,
      onRestartSelected,
      onUpdateSelected,
    });

    expect(screen.getByRole('button', { name: /Update Selected/ })).toBeTruthy();
    expect(screen.getByRole('button', { name: /Restart Selected/ })).toBeTruthy();
    expect(screen.getByRole('button', { name: /Delete Selected/ })).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: /Delete Selected/ }));
    expect(onDeleteSelected).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByRole('button', { name: /Restart Selected/ }));
    expect(onRestartSelected).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByRole('button', { name: /Update Selected/ }));
    expect(onUpdateSelected).toHaveBeenCalledTimes(1);

    const table = screen.getByRole('table');
    const rowChecks = within(table).getAllByRole('checkbox');
    // header + two data rows; offline row must not be disabled
    expect(rowChecks.length).toBeGreaterThanOrEqual(3);
    expect((rowChecks[2] as HTMLInputElement).disabled).toBe(false);
  });
});
