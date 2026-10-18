import { useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Alert, Button, Popconfirm, Space, Table, Tag, Typography, message } from 'antd';
import { telegramApi } from './api';

const invitationStates: Record<string, string> = {
  active: 'pages.telegram.invitation_active',
  used: 'pages.telegram.invitation_used',
  expired: 'pages.telegram.invitation_expired',
  revoked: 'pages.telegram.invitation_revoked',
  legacy: 'pages.telegram.invitation_legacy',
};
export default function TelegramInvitations() {
  const { t } = useTranslation();
  const cache = useQueryClient();
  const [page, setPage] = useState(1);
  const [busy, setBusy] = useState(0);
  const [messages, holder] = message.useMessage();
  const query = useQuery({
    queryKey: ['telegram', 'invitations', page],
    queryFn: () => telegramApi.invitations(page),
    refetchInterval: 15000,
  });
  async function change(id: number, revoke: boolean) {
    setBusy(id);
    try {
      if (revoke) await telegramApi.revokeInvitation(id);
      else await telegramApi.bindCode(id);
      await cache.invalidateQueries({ queryKey: ['telegram', 'invitations'] });
    } catch (error) {
      messages.error(error instanceof Error ? error.message : t('somethingWentWrong'));
    } finally {
      setBusy(0);
    }
  }
  return (
    <>
      {holder}
      <Typography.Title level={5}>{t('pages.telegram.invitations')}</Typography.Title>
      {query.error && <Alert type="error" title={query.error.message} />}
      <Table
        size="small"
        rowKey="clientId"
        loading={query.isFetching}
        dataSource={query.data?.items ?? []}
        scroll={{ x: 900 }}
        columns={[
          { title: t('pages.telegram.client'), dataIndex: 'email' },
          {
            title: t('pages.telegram.invitationCode'),
            dataIndex: 'code',
            render: (value: string) =>
              value ? (
                <Typography.Text copyable code>
                  {value}
                </Typography.Text>
              ) : (
                '—'
              ),
          },
          {
            title: t('pages.telegram.invitationLink'),
            dataIndex: 'url',
            render: (value: string) =>
              value ? (
                <Typography.Text copyable style={{ overflowWrap: 'anywhere' }}>
                  {value}
                </Typography.Text>
              ) : (
                '—'
              ),
          },
          {
            title: t('pages.telegram.invitationState'),
            dataIndex: 'status',
            render: (value: string) => (
              <Tag>{t(invitationStates[value] ?? 'pages.telegram.invitation_legacy')}</Tag>
            ),
          },
          {
            title: t('pages.telegram.codeExpires'),
            dataIndex: 'expiresAt',
            render: (value: number) => new Date(value).toLocaleString(),
          },
          {
            title: t('pages.telegram.actions'),
            render: (_, row) => (
              <Space>
                <Popconfirm
                  title={t('pages.telegram.rotateInvitationConfirm')}
                  onConfirm={() => change(row.clientId, false)}
                >
                  <Button size="small" loading={busy === row.clientId}>
                    {t('pages.telegram.rotateInvitation')}
                  </Button>
                </Popconfirm>
                <Popconfirm
                  title={t('pages.telegram.revokeInvitationConfirm')}
                  onConfirm={() => change(row.clientId, true)}
                >
                  <Button
                    size="small"
                    danger
                    disabled={!['active', 'legacy'].includes(row.status)}
                    loading={busy === row.clientId}
                  >
                    {t('pages.telegram.revokeInvitation')}
                  </Button>
                </Popconfirm>
              </Space>
            ),
          },
        ]}
        pagination={{
          current: page,
          pageSize: 20,
          total: query.data?.total ?? 0,
          onChange: setPage,
          showSizeChanger: false,
        }}
      />
    </>
  );
}
