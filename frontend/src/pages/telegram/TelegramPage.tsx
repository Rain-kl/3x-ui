import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Alert, Card, ConfigProvider, Layout, Space, Tabs, Tag } from 'antd';
import AppSidebar from '@/layouts/AppSidebar';
import { usePageTitle } from '@/hooks/usePageTitle';
import { useTheme } from '@/hooks/useTheme';
import { telegramApi } from './api';
import TelegramMessages from './TelegramMessages';
import TelegramLogs from './TelegramLogs';
import TelegramSettings from './TelegramSettings';

export default function TelegramPage() {
  usePageTitle();
  const { t } = useTranslation();
  const { isDark, isUltra, antdThemeConfig } = useTheme();
  const [tab, setTab] = useState('messages');
  const [tgId, setTgId] = useState<number | null>(null);
  const status = useQuery({
    queryKey: ['telegram', 'status'],
    queryFn: telegramApi.status,
    refetchInterval: 10000,
  });
  const openChat = (id: number) => {
    setTgId(id);
    setTab('messages');
  };
  return (
    <ConfigProvider theme={antdThemeConfig}>
      <Layout
        className={['telegram-page', isDark && 'is-dark', isUltra && 'is-ultra']
          .filter(Boolean)
          .join(' ')}
      >
        <AppSidebar />
        <Layout className="content-shell">
          <Layout.Content id="content-layout" className="content-area">
            <Card title={t('menu.telegram')}>
              <Space wrap style={{ marginBottom: 12 }}>
                <Tag color={status.data?.running ? 'success' : 'default'}>
                  {t(status.data?.running ? 'pages.telegram.running' : 'pages.telegram.stopped')}
                </Tag>
                {status.data?.botUsername && (
                  <a
                    href={`https://t.me/${status.data.botUsername}`}
                    target="_blank"
                    rel="noopener noreferrer"
                  >
                    @{status.data.botUsername}
                  </a>
                )}
              </Space>
              {(status.error || status.data?.lastError) && (
                <Alert
                  type="error"
                  title={status.error?.message || status.data?.lastError}
                  style={{ marginBottom: 12 }}
                />
              )}
              <Tabs
                activeKey={tab}
                onChange={setTab}
                destroyOnHidden
                items={[
                  {
                    key: 'messages',
                    label: t('pages.telegram.messages'),
                    children: <TelegramMessages tgId={tgId} onSelect={setTgId} />,
                  },
                  {
                    key: 'logs',
                    label: t('pages.telegram.logs'),
                    children: <TelegramLogs onChat={openChat} />,
                  },
                  {
                    key: 'settings',
                    label: t('pages.telegram.settings'),
                    children: <TelegramSettings onChat={openChat} />,
                  },
                ]}
              />
            </Card>
          </Layout.Content>
        </Layout>
      </Layout>
    </ConfigProvider>
  );
}
