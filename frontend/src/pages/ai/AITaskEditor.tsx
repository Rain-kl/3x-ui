import { FormProvider } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { Alert, Button, Col, Form, Input, InputNumber, Modal, Row, Select, Switch } from 'antd';
import { FormField, useZodForm } from '@/components/form/rhf';
import { TaskFormSchema, type AIModelOption } from './api';
import type { z } from 'zod';

export default function AITaskEditor({
  value,
  models,
  saving,
  error,
  onClose,
  onSave,
}: {
  value: z.infer<typeof TaskFormSchema>;
  models: AIModelOption[];
  saving: boolean;
  error?: string;
  onClose: () => void;
  onSave: (value: z.infer<typeof TaskFormSchema>) => void;
}) {
  const { t } = useTranslation();
  const methods = useZodForm(TaskFormSchema, { defaultValues: value });
  return (
    <Modal title={t('pages.ai.editTask')} open width={800} onCancel={onClose} footer={null}>
      {error && <Alert type="error" title={error} style={{ marginBottom: 16 }} />}
      <FormProvider {...methods}>
        <Form layout="vertical" onFinish={methods.handleSubmit(onSave)}>
          <FormField name="name" label={t('pages.ai.taskName')}>
            <Input maxLength={200} />
          </FormField>
          <FormField name="prompt" label={t('pages.ai.goal')}>
            <Input.TextArea
              autoSize={{ minRows: 4, maxRows: 12 }}
              maxLength={16000}
              placeholder={t('pages.ai.taskPlaceholder')}
            />
          </FormField>
          <Row gutter={24}>
            <Col xs={24} md={12}>
              <FormField name="intervalHours" label={t('pages.ai.intervalHours')}>
                <InputNumber min={1} max={8760} />
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
            <Col xs={12}>
              <FormField name="enabled" label={t('pages.ai.enabled')} valueProp="checked">
                <Switch />
              </FormField>
            </Col>
            <Col xs={12}>
              <FormField name="telegram" label={t('pages.ai.telegram')} valueProp="checked">
                <Switch />
              </FormField>
            </Col>
          </Row>
          <Button type="primary" htmlType="submit" loading={saving}>
            {t('pages.ai.save')}
          </Button>
        </Form>
      </FormProvider>
    </Modal>
  );
}
