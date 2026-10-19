import { FormProvider } from 'react-hook-form';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Alert, Button, Card, Form, Space, Typography } from 'antd';
import { z } from 'zod';
import JsonEditor from '@/components/form/JsonEditor';
import { FormField, useZodForm } from '@/components/form/rhf';
import { aiApi } from './api';

const ConfigSchema = z.object({
  config: z
    .string()
    .max(131072)
    .refine((value) => {
      try {
        return z.record(z.string(), z.unknown()).safeParse(JSON.parse(value)).success;
      } catch {
        return false;
      }
    }, 'pages.ai.invalidJSON'),
});

function Editor({ initial, path }: { initial: string; path: string }) {
  const { t } = useTranslation();
  const cache = useQueryClient();
  const methods = useZodForm(ConfigSchema, { defaultValues: { config: initial } });
  const save = useMutation({
    mutationFn: (data: z.infer<typeof ConfigSchema>) => aiApi.saveProviderConfig({ ...data, path }),
    onSuccess: (saved) => {
      methods.reset({ config: saved.config });
      void cache.invalidateQueries({ queryKey: ['ai', 'models'] });
      void cache.invalidateQueries({ queryKey: ['ai', 'status'] });
      cache.setQueryData(['ai', 'provider-config'], saved);
    },
  });
  function addProvider() {
    try {
      const config = z
        .record(z.string(), z.unknown())
        .parse(JSON.parse(methods.getValues('config')));
      const providers = z.record(z.string(), z.unknown()).parse(config.providers ?? {});
      let id = 'my-provider';
      for (let n = 2; providers[id]; n++) id = `my-provider-${n}`;
      config.providers = {
        ...providers,
        [id]: {
          package: '@opencode/ai/providers/openai-compatible',
          name: id,
          settings: { baseURL: 'https://api.example.com/v1', apiKey: '' },
          models: { 'my-model': { name: 'My model' } },
        },
      };
      methods.setValue('config', JSON.stringify(config, null, 2), {
        shouldDirty: true,
        shouldValidate: true,
      });
    } catch {
      methods.setError('config', { message: 'pages.ai.invalidJSON' });
    }
  }
  return (
    <FormProvider {...methods}>
      <Form layout="vertical" onFinish={methods.handleSubmit((data) => save.mutate(data))}>
        <Typography.Paragraph type="secondary">{t('pages.ai.providerHint')}</Typography.Paragraph>
        <Typography.Paragraph code>{path}</Typography.Paragraph>
        {save.error && (
          <Alert type="error" title={save.error.message} style={{ marginBottom: 12 }} />
        )}
        {save.isSuccess && (
          <Alert type="success" title={t('pages.ai.saved')} style={{ marginBottom: 12 }} />
        )}
        <FormField name="config" label={t('pages.ai.providerConfig')}>
          <JsonEditor value="" minHeight="360px" maxHeight="65vh" />
        </FormField>
        <Space wrap>
          <Button onClick={addProvider}>{t('pages.ai.addProvider')}</Button>
          <Button type="primary" htmlType="submit" loading={save.isPending}>
            {t('pages.ai.saveProviderConfig')}
          </Button>
        </Space>
      </Form>
    </FormProvider>
  );
}
export default function AIProviderEditor() {
  const { t } = useTranslation();
  const query = useQuery({
    queryKey: ['ai', 'provider-config'],
    queryFn: aiApi.providerConfig,
    staleTime: Infinity,
    refetchOnWindowFocus: false,
  });
  return (
    <Card title={t('pages.ai.providerConfig')} loading={query.isPending}>
      {query.error && <Alert type="error" title={query.error.message} />}
      {query.data && <Editor initial={query.data.config} path={query.data.path} />}
    </Card>
  );
}
