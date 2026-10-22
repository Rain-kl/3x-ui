import { useEffect, useState } from 'react';
import { FormProvider } from 'react-hook-form';
import { useInfiniteQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Alert, Button, Form, Input, Modal, Select } from 'antd';
import { z } from 'zod';
import { FormField, useZodForm } from '@/components/form/rhf';
import { telegramApi } from './api';

const ManualBindingSchema = z.object({
  clientId: z.number().int().positive('pages.telegram.required'),
  tgId: z
    .string()
    .trim()
    .refine(
      (value) => /^\d+$/.test(value) && Number.isSafeInteger(Number(value)) && Number(value) > 0,
      'pages.telegram.manualBindInvalidId',
    ),
});

export default function TelegramManualBindingModal({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation();
  const cache = useQueryClient();
  const methods = useZodForm(ManualBindingSchema, { defaultValues: { tgId: '' } });
  const [search, setSearch] = useState('');
  const [debounced, setDebounced] = useState('');
  useEffect(() => {
    const timer = setTimeout(() => setDebounced(search), 300);
    return () => clearTimeout(timer);
  }, [search]);
  const clients = useInfiniteQuery({
    queryKey: ['telegram', 'clients', debounced],
    initialPageParam: 1,
    queryFn: ({ pageParam }) => telegramApi.clients(debounced, pageParam),
    getNextPageParam: (last, pages) => (last.length === 50 ? pages.length + 1 : undefined),
  });
  const bind = useMutation({
    mutationFn: telegramApi.manualBind,
    onSuccess: async () => {
      await cache.invalidateQueries({ queryKey: ['telegram'] });
      onClose();
    },
  });
  const submit = methods.handleSubmit((value) => {
    bind.mutate({ clientId: value.clientId, tgId: Number(value.tgId) });
  });
  return (
    <Modal
      open
      title={t('pages.telegram.manualBind')}
      footer={null}
      onCancel={onClose}
      closable={!bind.isPending}
      keyboard={!bind.isPending}
      maskClosable={!bind.isPending}
    >
      <FormProvider {...methods}>
        <Form layout="vertical" onFinish={submit}>
          <Alert
            type="info"
            showIcon
            title={t('pages.telegram.manualBindHelp')}
            style={{ marginBottom: 16 }}
          />
          {(bind.error || clients.error) && (
            <Alert
              type="error"
              title={(bind.error || clients.error)?.message}
              style={{ marginBottom: 12 }}
            />
          )}
          <FormField name="clientId" label={t('pages.telegram.client')}>
            <Select
              disabled={bind.isPending}
              placeholder={t('pages.telegram.selectClient')}
              showSearch={{ filterOption: false, onSearch: setSearch }}
              options={(clients.data?.pages.flat() ?? []).map((row) => ({
                value: row.id,
                label: row.email,
              }))}
              loading={clients.isFetching}
              popupRender={(menu) => (
                <>
                  {menu}
                  {clients.hasNextPage && (
                    <Button
                      block
                      type="text"
                      loading={clients.isFetchingNextPage}
                      onClick={() => void clients.fetchNextPage()}
                    >
                      {t('pages.telegram.loadMore')}
                    </Button>
                  )}
                </>
              )}
            />
          </FormField>
          <FormField name="tgId" label="Telegram ID" extra={t('pages.telegram.manualBindIdHelp')}>
            <Input inputMode="numeric" autoComplete="off" disabled={bind.isPending} />
          </FormField>
          <Button type="primary" htmlType="submit" loading={bind.isPending}>
            {t('pages.telegram.manualBind')}
          </Button>
        </Form>
      </FormProvider>
    </Modal>
  );
}
