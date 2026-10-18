import { useEffect, useMemo, useState } from 'react';
import { useInfiniteQuery, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import {
  Alert,
  Button,
  Card,
  Modal,
  Popconfirm,
  Select,
  Space,
  Table,
  Typography,
  message,
} from 'antd';
import type { z } from 'zod';
import { BindCodeSchema } from '@/generated/zod';
import { telegramApi } from './api';
// FORK(telegram-invite-management): Invitation history must remain visible after the creation dialog closes.
import TelegramInvitations from './TelegramInvitations';

export default function TelegramBindings({ onChat }: { onChat: (tgId: number) => void }) {
  const { t } = useTranslation();
  const client = useQueryClient();
  const [page, setPage] = useState(1);
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState('');
  const [debounced, setDebounced] = useState('');
  const [selected, setSelected] = useState<number>();
  const [code, setCode] = useState<z.infer<typeof BindCodeSchema> | null>(null);
  const [busy, setBusy] = useState(false);
  const [messageApi, messageHolder] = message.useMessage();
  const bindings = useQuery({
    queryKey: ['telegram', 'bindings', page],
    queryFn: () => telegramApi.bindings(page),
    refetchInterval: 10000,
  });
  useEffect(() => {
    const timer = setTimeout(() => setDebounced(search), 300);
    return () => clearTimeout(timer);
  }, [search]);
  const clients = useInfiniteQuery({
    queryKey: ['telegram', 'clients', debounced],
    initialPageParam: 1,
    queryFn: ({ pageParam }) => telegramApi.clients(debounced, pageParam),
    getNextPageParam: (last, pages) => (last.length === 50 ? pages.length + 1 : undefined),
    enabled: open,
  });
  const options = useMemo(
    () => (clients.data?.pages.flat() ?? []).map((row) => ({ value: row.id, label: row.email })),
    [clients.data],
  );
  return (
    <Card
      size="small"
      title={t('pages.telegram.bindings')}
      extra={
        <Button
          onClick={() => {
            setOpen(true);
            setCode(null);
            setSelected(undefined);
          }}
        >
          {t('pages.telegram.createCode')}
        </Button>
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
      <TelegramInvitations />
      <Modal
        open={open}
        title={t('pages.telegram.createCode')}
        onCancel={() => setOpen(false)}
        footer={null}
      >
        <Space orientation="vertical" style={{ width: '100%' }}>
          {clients.error && <Alert type="error" title={clients.error.message} />}
          <Select
            style={{ width: '100%' }}
            placeholder={t('pages.telegram.selectClient')}
            value={selected}
            onChange={(value) => {
              setSelected(value);
              setCode(null);
            }}
            showSearch={{ filterOption: false, onSearch: setSearch }}
            options={options}
            loading={clients.isFetching}
            popupRender={(menu) => (
              <>
                {menu}
                {clients.hasNextPage && (
                  <Button
                    block
                    type="text"
                    loading={clients.isFetchingNextPage}
                    onClick={() => void clients.fetchNextPage()}
                  >
                    {t('pages.telegram.loadMore')}
                  </Button>
                )}
              </>
            )}
          />
          <Button
            type="primary"
            disabled={!selected}
            loading={busy}
            onClick={async () => {
              if (!selected) return;
              setBusy(true);
              try {
                setCode(await telegramApi.bindCode(selected));
                await client.invalidateQueries({ queryKey: ['telegram', 'invitations'] });
              } catch (error) {
                messageApi.error(error instanceof Error ? error.message : t('somethingWentWrong'));
              } finally {
                setBusy(false);
              }
            }}
          >
            {t('pages.telegram.generate')}
          </Button>
          {code && (
            <Alert
              type="success"
              title={t('pages.telegram.codeReady')}
              description={
                <>
                  <Typography.Paragraph copyable code>
                    {code.code}
                  </Typography.Paragraph>
                  {code.url && (
                    <Typography.Paragraph copyable style={{ overflowWrap: 'anywhere' }}>
                      {code.url}
                    </Typography.Paragraph>
                  )}
                  <Typography.Text>
                    {t('pages.telegram.codeExpires')} {new Date(code.expiresAt).toLocaleString()}
                  </Typography.Text>
                </>
              }
            />
          )}
        </Space>
      </Modal>
    </Card>
  );
}
