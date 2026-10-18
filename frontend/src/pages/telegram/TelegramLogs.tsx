import { useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import {
  Alert,
  Button,
  Card,
  Input,
  InputNumber,
  Popconfirm,
  Select,
  Space,
  Table,
  Typography,
  message,
} from 'antd';
import { telegramApi } from './api';

export default function TelegramLogs({ onChat }: { onChat: (tgId: number) => void }) {
  const { t } = useTranslation();
  const client = useQueryClient();
  const [page, setPage] = useState(1);
  const [size, setSize] = useState(20);
  const [event, setEvent] = useState('');
  const [tgId, setTgId] = useState('');
  const [days, setDays] = useState(30);
  const [kind, setKind] = useState('logs');
  const [busy, setBusy] = useState(false);
  const [messageApi, messageHolder] = message.useMessage();
  const query = useQuery({
    queryKey: ['telegram', 'logs', page, size, event, tgId],
    queryFn: () => telegramApi.logs(page, size, event, tgId),
    refetchInterval: 5000,
  });
  const cleanup = async (olderDays: number) => {
    setBusy(true);
    try {
      await telegramApi.clear(kind, olderDays);
      await client.invalidateQueries({ queryKey: ['telegram'] });
      setPage(1);
      messageApi.success(t('pages.telegram.cleaned'));
    } catch (error) {
      messageApi.error(error instanceof Error ? error.message : t('somethingWentWrong'));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Card>
      {messageHolder}
      <Space wrap style={{ marginBottom: 16 }}>
        <Input.Search
          placeholder="Telegram ID"
          allowClear
          onSearch={(value) => {
            setTgId(value.trim());
            setPage(1);
          }}
          style={{ width: 200 }}
        />
        <Input.Search
          placeholder={t('pages.telegram.eventFilter')}
          allowClear
          onSearch={(value) => {
            setEvent(value.trim());
            setPage(1);
          }}
          style={{ width: 260 }}
        />
        <Button onClick={() => void query.refetch()}>{t('refresh')}</Button>
      </Space>
      {query.error && <Alert type="error" title={query.error.message} />}
      <Table
        rowKey="id"
        loading={query.isLoading}
        dataSource={query.data?.items ?? []}
        scroll={{ x: 900 }}
        columns={[
          {
            title: t('pages.telegram.time'),
            dataIndex: 'createdAt',
            width: 200,
            render: (value: number) => new Date(value).toLocaleString(),
          },
          {
            title: 'Telegram ID',
            dataIndex: 'tgId',
            width: 160,
            render: (value: number) =>
              value > 0 ? (
                <Button type="link" onClick={() => onChat(value)}>
                  {value}
                </Button>
              ) : (
                '—'
              ),
          },
          { title: t('pages.telegram.event'), dataIndex: 'event', width: 220 },
          { title: t('pages.telegram.messageId'), dataIndex: 'messageId', width: 100 },
          {
            title: t('pages.telegram.details'),
            dataIndex: 'detail',
            render: (value: string) => (
              <Typography.Paragraph
                ellipsis={{ rows: 2, expandable: true }}
                style={{ margin: 0, whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}
              >
                {value}
              </Typography.Paragraph>
            ),
          },
        ]}
        pagination={{
          current: page,
          pageSize: size,
          total: query.data?.total ?? 0,
          showSizeChanger: true,
          pageSizeOptions: [20, 50, 100],
          onChange: (next, count) => {
            setPage(count === size ? next : 1);
            setSize(count);
          },
        }}
      />
      <Alert
        type="warning"
        showIcon
        title={t('pages.telegram.cleanupHelp')}
        style={{ marginBlock: 16 }}
      />
      <Space wrap>
        <Select
          value={kind}
          onChange={setKind}
          options={[
            { value: 'logs', label: t('pages.telegram.logs') },
            { value: 'messages', label: t('pages.telegram.messageHistory') },
          ]}
          style={{ width: 180 }}
        />
        <InputNumber
          min={1}
          max={3650}
          precision={0}
          value={days}
          onChange={(value) => setDays(value ?? 30)}
          addonAfter={t('pages.telegram.days')}
        />
        <Popconfirm title={t('pages.telegram.cleanupConfirm')} onConfirm={() => cleanup(days)}>
          <Button danger loading={busy}>
            {t('pages.telegram.clearOlder')}
          </Button>
        </Popconfirm>
        <Popconfirm title={t('pages.telegram.cleanupConfirm')} onConfirm={() => cleanup(0)}>
          <Button danger loading={busy}>
            {t('pages.telegram.clearAll')}
          </Button>
        </Popconfirm>
      </Space>
    </Card>
  );
}
