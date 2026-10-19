import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Alert, Card, ConfigProvider, Layout, Space, Tabs, Tag, Typography } from 'antd';
import AppSidebar from '@/layouts/AppSidebar';
import { useTheme } from '@/hooks/useTheme';
import { usePageTitle } from '@/hooks/usePageTitle';
import { aiApi } from './api';
import AIChat from './AIChat';
import AITasks from './AITasks';
import AISettings from './AISettings';
import './AIPage.css';

export default function AIPage() {
  usePageTitle();
  const { t } = useTranslation();
  const { isDark, isUltra, antdThemeConfig } = useTheme();
  const settings = useQuery({ queryKey: ['ai', 'settings'], queryFn: aiApi.settings });
  const enabled = !!settings.data?.enabled;
  const status = useQuery({
    queryKey: ['ai', 'status', enabled],
    queryFn: aiApi.status,
    enabled: !!settings.data,
    retry: false,
  });
  const models = useQuery({
    queryKey: ['ai', 'models', enabled],
    queryFn: aiApi.models,
    enabled,
    retry: false,
  });
  const error = settings.error || status.error || models.error;
  return (
    <ConfigProvider theme={antdThemeConfig}>
      <Layout className={`ai-page${isDark ? ' is-dark' : ''}${isUltra ? ' is-ultra' : ''}`}>
        <AppSidebar />
        <Layout className="content-shell">
          <Layout.Content id="content-layout" className="content-area">
            <Card title={t('menu.ai')}>
              <Space wrap style={{ marginBottom: 12 }}>
                <Tag color={status.data?.connected ? 'success' : 'default'}>
                  {t(status.data?.connected ? 'pages.ai.connected' : 'pages.ai.disconnected')}
                </Tag>
                <Typography.Text type="secondary">{status.data?.version}</Typography.Text>
              </Space>
              {(error || status.data?.error) && (
                <Alert
                  type="error"
                  title={error?.message || status.data?.error}
                  style={{ marginBottom: 16 }}
                />
              )}
              {!enabled && (
                <Alert
                  type="info"
                  title={t('pages.ai.disabledHint')}
                  style={{ marginBottom: 16 }}
                />
              )}
              <Tabs
                destroyOnHidden
                items={[
                  {
                    key: 'chat',
                    label: t('pages.ai.chat'),
                    children: (
                      <AIChat
                        models={models.data ?? []}
                        defaultModel={settings.data?.model ?? ''}
                        enabled={enabled}
                      />
                    ),
                  },
                  {
                    key: 'tasks',
                    label: t('pages.ai.tasks'),
                    children: <AITasks models={models.data ?? []} enabled={enabled} />,
                  },
                  {
                    key: 'settings',
                    label: t('pages.ai.settings'),
                    children: settings.data && (
                      <>
                        <AISettings
                          key={JSON.stringify(settings.data)}
                          value={settings.data}
                          models={models.data ?? []}
                        />
                        <Typography.Paragraph type="secondary" className="ai-text">
                          {status.data?.binary} {status.data?.directory}
                        </Typography.Paragraph>
                      </>
                    ),
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
