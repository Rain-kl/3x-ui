import { FormProvider } from 'react-hook-form';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import {
  Alert,
  Button,
  Col,
  Form,
  Input,
  InputNumber,
  Row,
  Select,
  Space,
  Switch,
  Typography,
} from 'antd';
import { FormField, useZodForm } from '@/components/form/rhf';
import { aiApi, SettingsFormSchema, type AIModelOption } from './api';
import type { z } from 'zod';

export default function AISettings({
  value,
  models,
}: {
  value: z.infer<typeof SettingsFormSchema>;
  models: AIModelOption[];
}) {
  const { t } = useTranslation();
  const cache = useQueryClient();
  const methods = useZodForm(SettingsFormSchema, { defaultValues: value });
  const save = useMutation({
    mutationFn: aiApi.saveSettings,
    onSuccess: () => {
      void cache.invalidateQueries({ queryKey: ['ai'] });
    },
  });
  const reconnect = useMutation({
    mutationFn: aiApi.reconnect,
    onSuccess: () => {
      void cache.invalidateQueries({ queryKey: ['ai', 'status'] });
      void cache.invalidateQueries({ queryKey: ['ai', 'models'] });
    },
  });
  return (
    <FormProvider {...methods}>
      <Form layout="vertical" onFinish={methods.handleSubmit((data) => save.mutate(data))}>
        <Alert type="info" title={t('pages.ai.settingsDescription')} style={{ marginBottom: 16 }} />
        {save.error && (
          <Alert
            type="error"
            title={(save.error || reconnect.error)?.message}
            style={{ marginBottom: 16 }}
          />
        )}
        {save.isSuccess && (
          <Alert type="success" title={t('pages.ai.saved')} style={{ marginBottom: 16 }} />
        )}
        <Row gutter={[24, 0]}>
          <Col xs={24} md={12}>
            <FormField name="enabled" label={t('pages.ai.enabled')} valueProp="checked">
              <Switch />
            </FormField>
          </Col>
          <Col xs={24} md={12}>
            <FormField name="retentionDays" label={t('pages.ai.retentionDays')}>
              <InputNumber min={1} max={3650} />
            </FormField>
          </Col>
          <Col xs={24} md={12}>
            <FormField name="url" label={t('pages.ai.url')}>
              <Input placeholder="http://127.0.0.1:4096" />
            </FormField>
          </Col>
          <Col xs={24} md={12}>
            <FormField name="model" label={t('pages.ai.model')}>
              <Select
                showSearch
                optionFilterProp="label"
                options={[
                  { value: '', label: t('pages.ai.openCodeDefault') },
                  ...models.map((item) => ({ value: item.id, label: `${item.name} (${item.id})` })),
                ]}
              />
            </FormField>
          </Col>
        </Row>
        <Space wrap>
          <Button type="primary" htmlType="submit" loading={save.isPending}>
            {t('pages.ai.save')}
          </Button>
          <Button loading={reconnect.isPending} onClick={() => reconnect.mutate()}>
            {t('pages.ai.refreshModels')}
          </Button>
        </Space>
        <Typography.Paragraph type="secondary" style={{ marginTop: 16 }}>
          {t('pages.ai.discoveryHint')}
        </Typography.Paragraph>
      </Form>
    </FormProvider>
  );
}
