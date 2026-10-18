import { FormProvider } from 'react-hook-form';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import {
  Alert,
  Button,
  Card,
  Col,
  Form,
  Input,
  InputNumber,
  Row,
  Space,
  Spin,
  Switch,
  message,
} from 'antd';
import { FormField, useZodForm } from '@/components/form/rhf';
import { TelegramSettingsFormSchema, telegramApi, type TelegramSettings as Settings } from './api';
import TelegramBindings from './TelegramBindings';

function SettingsEditor({ value }: { value: Settings }) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [messageApi, messageHolder] = message.useMessage();
  const methods = useZodForm(TelegramSettingsFormSchema, {
    defaultValues: { ...value, token: '' },
  });
  return (
    <FormProvider {...methods}>
      {messageHolder}
      <Form layout="vertical" component="div">
        <Alert
          showIcon
          type="info"
          title={t('pages.telegram.settingsHelp')}
          style={{ marginBottom: 16 }}
        />
        <Row gutter={24}>
          <Col xs={24} lg={12}>
            <FormField name="enabled" label={t('pages.telegram.enableBot')} valueProp="checked">
              <Switch />
            </FormField>
            <FormField
              name="token"
              label={t('pages.telegram.userToken')}
              extra={t(
                value.hasToken ? 'pages.telegram.tokenConfigured' : 'pages.telegram.tokenMissing',
              )}
            >
              <Input.Password
                autoComplete="new-password"
                placeholder={t('pages.telegram.tokenPlaceholder')}
              />
            </FormField>
            {value.hasToken && (
              <FormField
                name="clearToken"
                valueProp="checked"
                label={t('pages.telegram.clearToken')}
              >
                <Switch />
              </FormField>
            )}
            <FormField
              name="publicSubUrl"
              label={t('pages.telegram.publicURL')}
              extra={t('pages.telegram.publicURLHelp')}
            >
              <Input placeholder="https://example.com/sub/" />
            </FormField>
          </Col>
          <Col xs={24} lg={12}>
            <FormField
              name="forwardAdmins"
              label={t('pages.telegram.forwardAdmins')}
              valueProp="checked"
              extra={t('pages.telegram.adminHelp')}
            >
              <Switch />
            </FormField>
            <Row gutter={16}>
              <Col xs={24} sm={12}>
                <FormField name="logDays" label={t('pages.telegram.logDays')}>
                  <InputNumber min={1} max={3650} precision={0} style={{ width: '100%' }} />
                </FormField>
              </Col>
              <Col xs={24} sm={12}>
                <FormField name="messageDays" label={t('pages.telegram.messageDays')}>
                  <InputNumber min={1} max={3650} precision={0} style={{ width: '100%' }} />
                </FormField>
              </Col>
            </Row>
            <Alert type="info" title={t('pages.telegram.privacyHelp')} />
          </Col>
        </Row>
        <Button
          type="primary"
          loading={methods.formState.isSubmitting}
          onClick={() =>
            void methods.handleSubmit(async (data) => {
              try {
                const saved = await telegramApi.saveSettings(data);
                methods.reset({ ...saved, token: '' });
                await queryClient.invalidateQueries({ queryKey: ['telegram'] });
                messageApi.success(t('pages.telegram.settingsSaved'));
              } catch (error) {
                messageApi.error(error instanceof Error ? error.message : t('somethingWentWrong'));
              }
            })()
          }
        >
          {t('save')}
        </Button>
      </Form>
    </FormProvider>
  );
}

export default function TelegramSettings({ onChat }: { onChat: (tgId: number) => void }) {
  const { t } = useTranslation();
  const query = useQuery({ queryKey: ['telegram', 'settings'], queryFn: telegramApi.settings });
  return (
    <Space orientation="vertical" style={{ width: '100%' }}>
      <Card title={t('pages.telegram.settings')}>
        {query.error ? (
          <Alert type="error" title={query.error.message} />
        ) : query.data ? (
          <SettingsEditor value={query.data} />
        ) : (
          <Spin />
        )}
      </Card>
      <TelegramBindings onChat={onChat} />
    </Space>
  );
}
