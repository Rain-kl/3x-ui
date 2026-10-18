import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import {
  Alert,
  Button,
  DatePicker,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Space,
  Table,
  Tag,
  Typography,
  message,
} from 'antd';
import { SubscriptionAccessPageSchema } from '@/generated/zod';
import { HttpUtil } from '@/utils';

export default function SubscriptionAccessModal({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation();
  const [client, setClient] = useState('');
  const [ip, setIP] = useState('');
  const [day, setDay] = useState('');
  const [page, setPage] = useState(1);
  const [size, setSize] = useState(20);
  const [days, setDays] = useState(30);
  const [messages, holder] = message.useMessage();
  const query = useQuery({
    queryKey: ['audit', 'subscription-access', client, ip, day, page, size],
    queryFn: async () => {
      const response = await HttpUtil.get(
        '/panel/api/audit/subscription-access',
        { client, ip, day, page, pageSize: size },
        { silent: true },
      );
      if (!response.success) throw new Error(response.msg);
      return SubscriptionAccessPageSchema.parse(response.obj);
    },
    refetchInterval: 15000,
  });
  async function post(path: string, body: unknown) {
    try {
      const response = await HttpUtil.post(`/panel/api/audit/subscription-access${path}`, body, {
        headers: { 'Content-Type': 'application/json' },
        silent: true,
      });
      if (!response.success) throw new Error(response.msg);
      await query.refetch();
    } catch (error) {
      messages.error(error instanceof Error ? error.message : t('somethingWentWrong'));
    }
  }
  const date = (value: number) => (value ? new Date(value).toLocaleString() : '—');
  return (
    <Modal
      open
      title={t('pages.audit.subscriptionAccessTitle')}
      onCancel={onClose}
      footer={null}
      width={1100}
    >
      {holder}
      <Alert
        type="info"
        showIcon
        title={t('pages.audit.subscriptionSecurityHelp')}
        style={{ marginBottom: 16 }}
      />
      <Space wrap style={{ marginBottom: 16 }}>
        <Input.Search
          allowClear
          placeholder={t('pages.audit.client')}
          onSearch={(value) => {
            setClient(value);
            setPage(1);
          }}
        />
        <Input.Search
          allowClear
          placeholder={t('pages.audit.subscriptionIP')}
          onSearch={(value) => {
            setIP(value);
            setPage(1);
          }}
        />
        <DatePicker
          placeholder={t('pages.audit.dayFilter')}
          onChange={(value) => {
            setDay(value?.format('YYYY-MM-DD') ?? '');
            setPage(1);
          }}
        />
        <Button onClick={() => query.refetch()}>{t('refresh')}</Button>
      </Space>
      {query.error && <Alert type="error" title={query.error.message} />}
      <Table
        size="small"
        rowKey="id"
        loading={query.isFetching}
        dataSource={query.data?.items ?? []}
        scroll={{ x: 850, y: 360 }}
        columns={[
          { title: t('pages.audit.client'), dataIndex: 'client' },
          { title: t('pages.audit.subscriptionIP'), dataIndex: 'ip' },
          { title: t('pages.audit.time'), dataIndex: 'time', render: date },
          { title: 'HTTP', render: (_, row) => `${row.method} ${row.status}` },
          { title: t('pages.audit.subscriptionFormat'), dataIndex: 'format' },
          {
            title: t('pages.audit.subscriptionStatus'),
            render: (_, row) =>
              row.blocked ? <Tag color="error">{t('pages.audit.subscriptionBlocked')}</Tag> : '—',
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
      <Space wrap style={{ marginTop: 12 }}>
        <InputNumber
          min={0}
          max={3650}
          value={days}
          onChange={(value) => setDays(value ?? 30)}
          suffix={t('pages.audit.subscriptionDays')}
        />
        <Popconfirm
          title={t('pages.audit.subscriptionClearConfirm')}
          onConfirm={() => post('/clear', { days })}
        >
          <Button danger>{t('pages.audit.subscriptionClear')}</Button>
        </Popconfirm>
      </Space>
      <Typography.Title level={5}>{t('pages.audit.subscriptionRisk')}</Typography.Title>
      <Table
        size="small"
        rowKey="episode"
        dataSource={query.data?.risks ?? []}
        scroll={{ x: 850, y: 220 }}
        pagination={false}
        columns={[
          { title: t('pages.audit.client'), dataIndex: 'clientNames' },
          { title: t('pages.audit.time'), dataIndex: 'detectedAt', render: date },
          { title: t('pages.audit.subscriptionIPCount'), dataIndex: 'ipCount' },
          {
            title: t('pages.audit.subscriptionStatus'),
            render: (_, row) =>
              t(
                row.resolvedAt
                  ? 'pages.audit.subscriptionResolved'
                  : row.disabledAt
                    ? 'pages.audit.subscriptionDisabled'
                    : 'pages.audit.subscriptionPending',
              ),
          },
          { title: t('pages.audit.subscriptionError'), dataIndex: 'error' },
        ]}
      />
      <Typography.Title level={5}>{t('pages.audit.subscriptionAlerts')}</Typography.Title>
      <Table
        size="small"
        rowKey="id"
        dataSource={query.data?.alerts ?? []}
        pagination={false}
        scroll={{ x: 850, y: 200 }}
        columns={[
          { title: 'Bot', dataIndex: 'bot' },
          { title: 'Telegram ID', dataIndex: 'chatId' },
          {
            title: t('pages.audit.subscriptionStatus'),
            dataIndex: 'state',
            render: (value: string) =>
              t(
                value === 'failed'
                  ? 'pages.audit.subscriptionAlertFailed'
                  : 'pages.audit.subscriptionPending',
              ),
          },
          { title: t('pages.audit.subscriptionError'), dataIndex: 'error' },
          {
            title: t('pages.telegram.actions'),
            render: (_, row) => (
              <Button
                size="small"
                disabled={row.state !== 'failed'}
                onClick={() => post(`/alerts/${row.id}/retry`, {})}
              >
                {t('pages.audit.subscriptionRetry')}
              </Button>
            ),
          },
        ]}
      />
    </Modal>
  );
}
