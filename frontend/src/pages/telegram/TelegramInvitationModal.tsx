import { useEffect, useState } from 'react';
import { FormProvider, useWatch } from 'react-hook-form';
import { useInfiniteQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Alert, Button, Form, InputNumber, Modal, Select, Space, Switch, Typography } from 'antd';
import { z } from 'zod';
import { BindCodeSchema } from '@/generated/zod';
import { FormField, useZodForm } from '@/components/form/rhf';
import { telegramApi, type Invitation } from './api';

const factors = { minutes: 1, hours: 60, days: 1440 };
const InvitationFormSchema = z
  .object({
    clientId: z.number().int().positive('pages.telegram.required'),
    duration: z.number().nullish(),
    unit: z.enum(['minutes', 'hours', 'days']),
    neverExpires: z.boolean(),
  })
  .superRefine((value, ctx) => {
    if (
      !value.neverExpires &&
      (value.duration == null ||
        !Number.isInteger(value.duration) ||
        value.duration < 1 ||
        value.duration * factors[value.unit] > 5256000)
    )
      ctx.addIssue({
        code: 'custom',
        path: ['duration'],
        message: 'pages.telegram.inviteValidityLimit',
      });
  });

function initialLifetime(invitation?: Invitation) {
  const minutes =
    invitation && invitation.expiresAt > 0 && invitation.createdAt > 0
      ? Math.min(
          5256000,
          Math.max(1, Math.round((invitation.expiresAt - invitation.createdAt) / 60000)),
        )
      : 30;
  const unit = minutes % 1440 === 0 ? 'days' : minutes % 60 === 0 ? 'hours' : 'minutes';
  return {
    duration: minutes / factors[unit],
    unit,
    neverExpires: invitation?.expiresAt === 0,
  } as const;
}

export default function TelegramInvitationModal({
  invitation,
  onClose,
}: {
  invitation?: Invitation;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const cache = useQueryClient();
  const methods = useZodForm(InvitationFormSchema, {
    defaultValues: { clientId: invitation?.clientId, ...initialLifetime(invitation) },
  });
  const selectedClient = useWatch({ control: methods.control, name: 'clientId' });
  const [codeClient, setCodeClient] = useState<number>();
  const neverExpires = useWatch({ control: methods.control, name: 'neverExpires' });
  const [search, setSearch] = useState('');
  const [debounced, setDebounced] = useState('');
  const [code, setCode] = useState<z.infer<typeof BindCodeSchema> | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<Error | null>(null);
  useEffect(() => {
    const timer = setTimeout(() => setDebounced(search), 300);
    return () => clearTimeout(timer);
  }, [search]);
  const clients = useInfiniteQuery({
    queryKey: ['telegram', 'clients', debounced],
    initialPageParam: 1,
    queryFn: ({ pageParam }) => telegramApi.clients(debounced, pageParam),
    getNextPageParam: (last, pages) => (last.length === 50 ? pages.length + 1 : undefined),
    enabled: !invitation,
  });
  const options = invitation
    ? [{ value: invitation.clientId, label: invitation.email }]
    : (clients.data?.pages.flat() ?? []).map((row) => ({ value: row.id, label: row.email }));
  const submit = methods.handleSubmit(async (value) => {
    setBusy(true);
    setError(null);
    setCode(null);
    try {
      setCode(
        await telegramApi.bindCode({
          clientId: value.clientId,
          validityMinutes: value.neverExpires ? 0 : (value.duration ?? 0) * factors[value.unit],
        }),
      );
      setCodeClient(value.clientId);
      await cache.invalidateQueries({ queryKey: ['telegram', 'invitations'] });
    } catch (cause) {
      setError(cause instanceof Error ? cause : new Error(t('somethingWentWrong')));
    } finally {
      setBusy(false);
    }
  });
  return (
    <Modal
      open
      title={t(invitation ? 'pages.telegram.rotateInvitation' : 'pages.telegram.createCode')}
      onCancel={onClose}
      maskClosable={!busy}
      closable={!busy}
      keyboard={!busy}
      footer={null}
    >
      <FormProvider {...methods}>
        <Form layout="vertical" onFinish={submit}>
          {(error || clients.error) && (
            <Alert
              type="error"
              title={(error || clients.error)?.message}
              style={{ marginBottom: 12 }}
            />
          )}
          {invitation && !code && (
            <Alert
              type="warning"
              title={t('pages.telegram.rotateInvitationConfirm')}
              style={{ marginBottom: 12 }}
            />
          )}
          <FormField name="clientId" label={t('pages.telegram.client')}>
            <Select
              disabled={busy || !!invitation}
              placeholder={t('pages.telegram.selectClient')}
              showSearch={{ filterOption: false, onSearch: setSearch }}
              options={options}
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
          <FormField
            name="neverExpires"
            label={t('pages.telegram.neverExpires')}
            valueProp="checked"
          >
            <Switch disabled={busy} />
          </FormField>
          <Space align="start">
            <FormField name="duration" label={t('pages.telegram.inviteDuration')}>
              <InputNumber min={1} max={5256000} precision={0} disabled={neverExpires || busy} />
            </FormField>
            <FormField name="unit" label={t('pages.telegram.inviteUnit')}>
              <Select
                style={{ width: 120 }}
                disabled={neverExpires || busy}
                options={Object.keys(factors).map((unit) => ({
                  value: unit,
                  label: t(`pages.telegram.inviteUnits.${unit}`),
                }))}
              />
            </FormField>
          </Space>
          <Typography.Paragraph type="secondary">
            {t('pages.telegram.inviteValidityHelp')}
          </Typography.Paragraph>
          <Button type="primary" htmlType="submit" loading={busy}>
            {t('pages.telegram.generate')}
          </Button>
          {code && codeClient === selectedClient && (
            <Alert
              type="success"
              style={{ marginTop: 16 }}
              title={t('pages.telegram.codeReady')}
              description={
                <>
                  <Typography.Paragraph copyable code>
                    {code.code}
                  </Typography.Paragraph>
                  {code.url && (
                    <Typography.Paragraph copyable style={{ overflowWrap: 'anywhere' }}>
                      {code.url}
                    </Typography.Paragraph>
                  )}
                  <Typography.Text>
                    {t('pages.telegram.codeExpires')}{' '}
                    {code.expiresAt === 0
                      ? t('pages.telegram.neverExpires')
                      : new Date(code.expiresAt).toLocaleString()}
                  </Typography.Text>
                </>
              }
            />
          )}
        </Form>
      </FormProvider>
    </Modal>
  );
}
