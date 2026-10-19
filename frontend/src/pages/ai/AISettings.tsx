import { useEffect } from 'react';
import { FormProvider } from 'react-hook-form';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
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
import AIProviderEditor from './AIProviderEditor';
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
    onSuccess: (saved) => {
      methods.reset(saved);
      void cache.invalidateQueries({ queryKey: ['ai'] });
    },
  });
  const deployment = useQuery({
    queryKey: ['ai', 'deployment'],
    queryFn: aiApi.deployment,
    refetchInterval: (query) => (query.state.data?.state === 'running' ? 1000 : false),
  });
  const deploy = useMutation({
    mutationFn: aiApi.deploy,
    onSuccess: (state) => {
      cache.setQueryData(['ai', 'deployment'], state);
    },
  });
  const reconnect = useMutation({
    mutationFn: aiApi.reconnect,
    onSuccess: () => {
      void cache.invalidateQueries({ queryKey: ['ai', 'status'] });
      void cache.invalidateQueries({ queryKey: ['ai', 'models'] });
    },
  });
  useEffect(() => {
    if (deployment.data?.state === 'completed') void cache.invalidateQueries({ queryKey: ['ai'] });
  }, [deployment.data?.state, cache]);
  const installing = deploy.isPending || deployment.data?.state === 'running';
  return (
    <FormProvider {...methods}>
      <Space orientation="vertical" size="large" style={{ width: '100%' }}>
        <div>
          <Button type="primary" loading={installing} onClick={() => deploy.mutate()}>
            {t('pages.ai.deploy')}
          </Button>
          {deployment.data?.state === 'running' && (
            <Typography.Text style={{ marginInlineStart: 12 }}>
              {t(`pages.ai.deployPhase.${deployment.data.phase}`)}
            </Typography.Text>
          )}
          {deployment.data?.state === 'completed' && (
            <Alert type="success" title={t('pages.ai.deployed')} style={{ marginTop: 12 }} />
          )}
          {(deploy.error || deployment.error || deployment.data?.error) && (
            <Alert
              type="error"
              title={deploy.error?.message || deployment.error?.message || deployment.data?.error}
              style={{ marginTop: 12 }}
            />
          )}
        </div>
        <Form layout="vertical" onFinish={methods.handleSubmit((data) => save.mutate(data))}>
          <Alert
            type="info"
            title={t('pages.ai.settingsDescription')}
            style={{ marginBottom: 16 }}
          />
          {(save.error || reconnect.error) && (
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
            {/* FORK(panel-ai): CLI deployment removes the need to prepare an external service. */}
            <Col xs={24} md={12}>
              <FormField name="binary" label={t('pages.ai.binary')}>
                <Input placeholder="/usr/local/bin/opencode" />
              </FormField>
            </Col>
            <Col xs={24} md={12}>
              <FormField name="directory" label={t('pages.ai.directory')}>
                <Input placeholder={t('pages.ai.directoryHint')} />
              </FormField>
            </Col>
            <Col xs={24} md={12}>
              <FormField name="model" label={t('pages.ai.model')}>
                <Select
                  showSearch
                  optionFilterProp="label"
                  options={[
                    { value: '', label: t('pages.ai.openCodeDefault') },
                    ...models.map((item) => ({
                      value: item.id,
                      label: `${item.name} (${item.id})`,
                    })),
                  ]}
                />
              </FormField>
            </Col>
          </Row>
          <Space wrap>
            <Button type="primary" htmlType="submit" loading={save.isPending} disabled={installing}>
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
        <AIProviderEditor key={value.directory} />
      </Space>
    </FormProvider>
  );
}
