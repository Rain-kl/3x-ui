import { memo } from 'react';
import { useTranslation } from 'react-i18next';
import { Button, Popover, Space, Tag, Tooltip } from 'antd';
import {
  DeleteOutlined,
  EditOutlined,
  InfoCircleOutlined,
  QrcodeOutlined,
  RetweetOutlined,
  SyncOutlined, // FORK(telegram-reset-revoke): Distinguish link rotation from traffic reset.
} from '@ant-design/icons';

import { formatInboundLabel } from '@/lib/inbounds/label';
import type { InboundOption } from '@/hooks/useClients';

const ICON_BUTTON_STYLE = { fontSize: 16 } as const;

interface ClientRowActionsProps {
  email: string;
  onShowQr: (email: string) => void;
  onShowInfo: (email: string) => void;
  onResetTraffic: (email: string) => void;
  // FORK(telegram-reset-revoke): Include credential revocation in the row actions.
  onResetSubscription: (email: string) => void;
  onEdit: (email: string) => void;
  onDelete: (email: string) => void;
}

// FORK(telegram-reset-revoke): Keep tooltip alignment independent of traffic updates.
// Row actions are memoized by email because traffic updates replace row objects.
export const ClientRowActions = memo(function ClientRowActions({
  email,
  onShowQr,
  onShowInfo,
  onResetTraffic,
  // FORK(telegram-reset-revoke): Use the same confirmation as the mobile menu.
  onResetSubscription,
  onEdit,
  onDelete,
}: ClientRowActionsProps) {
  const { t } = useTranslation();
  return (
    <Space size={4}>
      <Tooltip title={t('pages.clients.qrCode')}>
        <Button
          size="small"
          type="text"
          style={ICON_BUTTON_STYLE}
          icon={<QrcodeOutlined aria-hidden />}
          aria-label={t('pages.clients.qrCode')}
          onClick={() => onShowQr(email)}
        />
      </Tooltip>
      {/* FORK-BEGIN(telegram-reset-revoke): Place revocation beside the subscription QR. */}
      <Tooltip title={t('pages.clients.resetSubscription')}>
        <Button
          size="small"
          type="text"
          danger
          style={ICON_BUTTON_STYLE}
          icon={<SyncOutlined aria-hidden />}
          aria-label={t('pages.clients.resetSubscription')}
          onClick={() => onResetSubscription(email)}
        />
      </Tooltip>
      {/* FORK-END(telegram-reset-revoke) */}
      <Tooltip title={t('pages.clients.clientInfo')}>
        <Button
          size="small"
          type="text"
          style={ICON_BUTTON_STYLE}
          icon={<InfoCircleOutlined aria-hidden />}
          aria-label={t('pages.clients.clientInfo')}
          onClick={() => onShowInfo(email)}
        />
      </Tooltip>
      <Tooltip title={t('pages.inbounds.resetTraffic')}>
        <Button
          size="small"
          type="text"
          style={ICON_BUTTON_STYLE}
          icon={<RetweetOutlined aria-hidden />}
          aria-label={t('pages.inbounds.resetTraffic')}
          onClick={() => onResetTraffic(email)}
        />
      </Tooltip>
      <Tooltip title={t('edit')}>
        <Button
          size="small"
          type="text"
          style={ICON_BUTTON_STYLE}
          icon={<EditOutlined aria-hidden />}
          aria-label={t('edit')}
          onClick={() => onEdit(email)}
        />
      </Tooltip>
      <Tooltip title={t('delete')}>
        <Button
          size="small"
          type="text"
          danger
          style={ICON_BUTTON_STYLE}
          icon={<DeleteOutlined aria-hidden />}
          aria-label={t('delete')}
          onClick={() => onDelete(email)}
        />
      </Tooltip>
    </Space>
  );
});

const CHIP_STYLE = { margin: 2 } as const;
const OVERFLOW_CHIP_STYLE = { margin: 2, cursor: 'pointer' } as const;
const OVERFLOW_LIST_STYLE = {
  display: 'flex',
  flexDirection: 'column' as const,
  gap: 4,
  maxWidth: 280,
  maxHeight: 280,
  overflowY: 'auto' as const,
};

interface ClientInboundChipsProps {
  ids: number[];
  inboundsById: Record<number, InboundOption>;
  protocolColors: Record<string, string>;
  chipLimit: number;
}

// Attachments never change on a traffic push either, so the same memoisation
// applies: one Tooltip per visible chip plus a Popover for the overflow.
export const ClientInboundChips = memo(function ClientInboundChips({
  ids,
  inboundsById,
  protocolColors,
  chipLimit,
}: ClientInboundChipsProps) {
  if (ids.length === 0) return <span className="cell-empty">—</span>;

  const label = (id: number) => {
    const ib = inboundsById[id];
    return formatInboundLabel(ib?.tag, ib?.remark);
  };
  const chip = (id: number) => {
    const proto = (inboundsById[id]?.protocol || '').toLowerCase();
    return (
      <Tooltip key={id} title={label(id)}>
        <Tag color={protocolColors[proto] ?? 'default'} style={CHIP_STYLE}>
          {label(id)}
        </Tag>
      </Tooltip>
    );
  };

  const visible = ids.slice(0, chipLimit);
  const overflow = ids.slice(chipLimit);
  return (
    <>
      {visible.map(chip)}
      {overflow.length > 0 && (
        <Popover
          trigger="click"
          placement="bottomRight"
          content={<div style={OVERFLOW_LIST_STYLE}>{overflow.map(chip)}</div>}
        >
          <Tag color="default" style={OVERFLOW_CHIP_STYLE}>
            +{overflow.length}
          </Tag>
        </Popover>
      )}
    </>
  );
});
