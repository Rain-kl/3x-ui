import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Alert, ConfigProvider, Layout, Tabs, Typography } from 'antd';
import AppSidebar from '@/layouts/AppSidebar';
import { useTheme } from '@/hooks/useTheme';
import { usePageTitle } from '@/hooks/usePageTitle';
import { aiApi } from './api';
import { withBasePath } from '@/api/http-init';
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
            {(error || status.data?.error) && (
              <Alert
                type="error"
                title={error?.message || status.data?.error}
                className="ai-banner"
              />
            )}
            {!enabled && (
              <Alert type="info" title={t('pages.ai.disabledHint')} className="ai-banner" />
            )}
            <Tabs
              destroyOnHidden
              className="ai-tabs"
              items={[
                {
                  key: 'chat',
                  label: t('pages.ai.chat'),
                  children: enabled ? (
                    <iframe
                      title="OpenCode"
                      src={withBasePath('/panel/ai/opencode/')}
                      className="ai-chat-frame"
                    />
                  ) : null,
                },
                {
                  key: 'tasks',
                  label: t('pages.ai.tasks'),
                  children: (
                    <div className="ai-tab-body">
                      <AITasks models={models.data ?? []} enabled={enabled} />
                    </div>
                  ),
                },
                {
                  key: 'settings',
                  label: t('pages.ai.settings'),
                  children: (
                    <div className="ai-tab-body">
                      {settings.data && (
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
                      )}
                    </div>
                  ),
                },
              ]}
            />
          </Layout.Content>
        </Layout>
      </Layout>
    </ConfigProvider>
  );
}
