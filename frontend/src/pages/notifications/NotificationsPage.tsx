import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Alert,
  Button,
  Card,
  ConfigProvider,
  Layout,
  Popconfirm,
  Space,
  Table,
  Tag,
  Typography,
  message,
} from 'antd';
import type { TableColumnsType } from 'antd';
import { PlusOutlined } from '@ant-design/icons';
import AppSidebar from '@/layouts/AppSidebar';
import { useTheme } from '@/hooks/useTheme';
import { usePageTitle } from '@/hooks/usePageTitle';
import NotificationEditor from './NotificationEditor';
import {
  deleteNotification,
  emptyNotification,
  notificationList,
  saveNotification,
  type Notification,
} from './api';

export default function NotificationsPage() {
  usePageTitle();
  const { t } = useTranslation();
  const { isDark, isUltra, antdThemeConfig } = useTheme();
  const queryClient = useQueryClient();
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [editing, setEditing] = useState<Notification | null>(null);
  const [messageApi, messageHolder] = message.useMessage();
  const query = useQuery({
    queryKey: ['notifications', page, pageSize],
    queryFn: () => notificationList(page, pageSize),
  });
  const invalidate = () => queryClient.invalidateQueries({ queryKey: ['notifications'] });
  const save = useMutation({ mutationFn: saveNotification, onSuccess: () => invalidate() });
  const remove = useMutation({ mutationFn: deleteNotification, onSuccess: () => invalidate() });
  const reportError = (error: unknown) =>
    messageApi.error(error instanceof Error ? error.message : t('somethingWentWrong'));
  const columns: TableColumnsType<Notification> = [
    { title: t('pages.notifications.order'), dataIndex: 'sortIndex', width: 100 },
    {
      title: t('pages.notifications.title'),
      dataIndex: 'title',
      width: 260,
      render: (title: string, row) => (
        <Space orientation="vertical" size={0}>
          <Typography.Text strong>{title}</Typography.Text>
          <Typography.Paragraph
            ellipsis={{ rows: 2, expandable: true }}
            style={{ margin: 0, whiteSpace: 'pre-wrap' }}
          >
            {row.body}
          </Typography.Paragraph>
        </Space>
      ),
    },
    {
      title: t('pages.notifications.audience'),
      render: (_, row) =>
        [
          row.targetType !== 'groups' && row.clientIds.length > 0
            ? t('pages.notifications.clientCount', { count: row.clientIds.length })
            : '',
          row.targetType !== 'clients' ? row.groups.join(', ') : '',
        ]
          .filter(Boolean)
          .join(' · '),
    },
    {
      title: t('pages.notifications.status'),
      width: 140,
      render: (_, row) => (
        <Tag color={row.published ? 'success' : 'default'}>
          {t(row.published ? 'pages.notifications.published' : 'pages.notifications.draft')}
        </Tag>
      ),
    },
    {
      title: t('pages.notifications.toNodes'),
      width: 140,
      render: (_, row) => (row.publishToNodes ? t('enable') : t('disabled')),
    },
    {
      title: t('pages.notifications.actions'),
      width: 260,
      render: (_, row) => (
        <Space wrap>
          <Button size="small" onClick={() => setEditing(row)}>
            {t('edit')}
          </Button>
          <Button
            size="small"
            loading={save.isPending}
            onClick={async () => {
              try {
                await save.mutateAsync({ ...row, published: !row.published });
              } catch (error) {
                reportError(error);
              }
            }}
          >
            {t(row.published ? 'pages.notifications.withdraw' : 'pages.notifications.publish')}
          </Button>
          <Popconfirm
            title={t('pages.notifications.deleteConfirm')}
            onConfirm={async () => {
              try {
                await remove.mutateAsync(row.id);
                if (query.data?.items.length === 1 && page > 1) setPage(page - 1);
              } catch (error) {
                reportError(error);
              }
            }}
          >
            <Button danger size="small" loading={remove.isPending}>
              {t('delete')}
            </Button>
          </Popconfirm>
        </Space>
      ),
    },
  ];
  return (
    <ConfigProvider theme={antdThemeConfig}>
      {messageHolder}
      <Layout
        className={['notifications-page', isDark && 'is-dark', isUltra && 'is-ultra']
          .filter(Boolean)
          .join(' ')}
      >
        <AppSidebar />
        <Layout className="content-shell">
          <Layout.Content id="content-layout" className="content-area">
            <Card
              title={t('menu.notifications')}
              extra={
                <Button
                  type="primary"
                  icon={<PlusOutlined />}
                  onClick={() => setEditing({ ...emptyNotification })}
                >
                  {t('pages.notifications.create')}
                </Button>
              }
            >
              <Alert
                type="info"
                showIcon
                title={t('pages.notifications.help')}
                style={{ marginBottom: 16 }}
              />
              {query.error && (
                <Alert
                  type="error"
                  showIcon
                  title={query.error.message}
                  action={<Button onClick={() => void query.refetch()}>{t('refresh')}</Button>}
                />
              )}
              <Table
                rowKey="id"
                columns={columns}
                dataSource={query.data?.items ?? []}
                loading={query.isFetching}
                scroll={{ x: 1100 }}
                pagination={{
                  current: page,
                  pageSize,
                  total: query.data?.total ?? 0,
                  showSizeChanger: true,
                  pageSizeOptions: [20, 50, 100],
                  onChange: (next, size) => {
                    setPage(size === pageSize ? next : 1);
                    setPageSize(size);
                  },
                }}
              />
            </Card>
            {editing && (
              <NotificationEditor
                key={`${editing.id}-${editing.updatedAt}`}
                value={editing}
                saving={save.isPending}
                onClose={() => setEditing(null)}
                onSave={async (value) => {
                  try {
                    await save.mutateAsync(value);
                    setEditing(null);
                    messageApi.success(
                      t(
                        value.published
                          ? 'pages.notifications.published'
                          : 'pages.notifications.draftSaved',
                      ),
                    );
                  } catch (error) {
                    reportError(error);
                  }
                }}
              />
            )}
          </Layout.Content>
        </Layout>
      </Layout>
    </ConfigProvider>
  );
}
