import { useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Alert, Button, Card, Popconfirm, Space, Table, message } from 'antd';
import { telegramApi } from './api';
// FORK(telegram-invite-management): Invitation history must remain visible after the creation dialog closes.
import TelegramInvitations from './TelegramInvitations';
import TelegramInvitationModal from './TelegramInvitationModal';
import TelegramManualBindingModal from './TelegramManualBindingModal';

export default function TelegramBindings({ onChat }: { onChat: (tgId: number) => void }) {
  const { t } = useTranslation();
  const client = useQueryClient();
  const [page, setPage] = useState(1);
  const [open, setOpen] = useState(false);
  const [manualOpen, setManualOpen] = useState(false);
  const [messageApi, messageHolder] = message.useMessage();
  const bindings = useQuery({
    queryKey: ['telegram', 'bindings', page],
    queryFn: () => telegramApi.bindings(page),
    refetchInterval: 10000,
  });
  return (
    <Card
      size="small"
      title={t('pages.telegram.bindings')}
      extra={
        <Space wrap>
          <Button onClick={() => setOpen(true)}>{t('pages.telegram.createCode')}</Button>
          <Button type="primary" onClick={() => setManualOpen(true)}>
            {t('pages.telegram.manualBind')}
          </Button>
        </Space>
      }
    >
      {messageHolder}
      <Alert
        type="info"
        showIcon
        title={t('pages.telegram.bindingHelp')}
        style={{ marginBottom: 16 }}
      />
      {bindings.error && <Alert type="error" title={bindings.error.message} />}
      <Table
        rowKey="id"
        loading={bindings.isFetching}
        dataSource={bindings.data?.items ?? []}
        scroll={{ x: 720 }}
        columns={[
          { title: t('pages.telegram.client'), dataIndex: 'email' },
          { title: 'Telegram ID', dataIndex: 'tgId' },
          { title: t('username'), dataIndex: 'username' },
          {
            title: t('pages.telegram.actions'),
            render: (_, row) => (
              <Space>
                <Button size="small" onClick={() => onChat(row.tgId)}>
                  {t('pages.telegram.openChat')}
                </Button>
                <Popconfirm
                  title={t('pages.telegram.unbindConfirm')}
                  onConfirm={async () => {
                    try {
                      await telegramApi.unbind(row.tgId);
                      await client.invalidateQueries({ queryKey: ['telegram'] });
                    } catch (error) {
                      messageApi.error(
                        error instanceof Error ? error.message : t('somethingWentWrong'),
                      );
                    }
                  }}
                >
                  <Button danger size="small">
                    {t('pages.telegram.unbind')}
                  </Button>
                </Popconfirm>
              </Space>
            ),
          },
        ]}
        pagination={{
          current: page,
          pageSize: 20,
          total: bindings.data?.total ?? 0,
          onChange: setPage,
          showSizeChanger: false,
        }}
      />
      {manualOpen && <TelegramManualBindingModal onClose={() => setManualOpen(false)} />}
      <TelegramInvitations />
      {open && <TelegramInvitationModal onClose={() => setOpen(false)} />}
    </Card>
  );
}
