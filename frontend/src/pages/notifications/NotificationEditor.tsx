import { useEffect, useMemo, useState } from 'react';
import { FormProvider, useWatch } from 'react-hook-form';
import { useInfiniteQuery, useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import {
  Alert,
  Button,
  Col,
  Form,
  Input,
  InputNumber,
  Modal,
  Radio,
  Row,
  Select,
  Space,
  Switch,
} from 'antd';
import { FormField, useZodForm } from '@/components/form/rhf';
import { GroupSummaryListSchema } from '@/schemas/client';
import { HttpUtil } from '@/utils';
import { NotificationFormSchema, notificationClients, type Notification } from './api';

interface Props {
  value: Notification;
  saving: boolean;
  onClose: () => void;
  onSave: (value: Notification) => Promise<void>;
}

export default function NotificationEditor({ value, saving, onClose, onSave }: Props) {
  const { t } = useTranslation();
  const methods = useZodForm(NotificationFormSchema, {
    defaultValues: { ...value, targetType: value.targetType === 'groups' ? 'groups' : 'clients' },
  });
  const targetType = useWatch({ control: methods.control, name: 'targetType' });
  const ids = useWatch({ control: methods.control, name: 'clientIds' });
  const [search, setSearch] = useState('');
  const [debounced, setDebounced] = useState('');
  useEffect(() => {
    const timer = setTimeout(() => setDebounced(search), 300);
    return () => clearTimeout(timer);
  }, [search]);
  const clients = useInfiniteQuery({
    queryKey: ['notification-clients', debounced],
    initialPageParam: 1,
    queryFn: ({ pageParam }) => notificationClients(debounced, pageParam),
    getNextPageParam: (last, pages) => (last.length === 50 ? pages.length + 1 : undefined),
    enabled: targetType === 'clients',
  });
  const selected = useQuery({
    queryKey: ['notification-selected-clients', ids],
    queryFn: () => notificationClients('', 1, ids),
    enabled: ids.length > 0 && targetType === 'clients',
  });
  const groups = useQuery({
    queryKey: ['notification-groups'],
    queryFn: async () => {
      const msg = await HttpUtil.get('/panel/api/clients/groups', undefined, { silent: true });
      if (!msg.success) throw new Error(msg.msg);
      return GroupSummaryListSchema.parse(msg.obj);
    },
    enabled: targetType === 'groups',
  });
  const clientOptions = useMemo(() => {
    const rows = new Map((selected.data ?? []).map((row) => [row.id, row]));
    for (const row of clients.data?.pages.flat() ?? []) rows.set(row.id, row);
    return [...rows.values()].map((row) => ({ value: row.id, label: row.email }));
  }, [selected.data, clients.data]);

  const submit = (published: boolean) => {
    methods.setValue('published', published);
    void methods.handleSubmit(async (data) => {
      if (
        published &&
        (data.targetType === 'clients' ? data.clientIds.length === 0 : data.groups.length === 0)
      ) {
        methods.setError(data.targetType === 'clients' ? 'clientIds' : 'groups', {
          message: 'pages.notifications.chooseAudience',
        });
        return;
      }
      await onSave(data);
    })();
  };
  const error = clients.error || selected.error || groups.error;
  return (
    <Modal
      open
      title={t(value.id ? 'pages.notifications.edit' : 'pages.notifications.create')}
      onCancel={onClose}
      width={840}
      maskClosable={!saving}
      closable={!saving}
      footer={
        <Space wrap>
          <Button disabled={saving} onClick={onClose}>
            {t('cancel')}
          </Button>
          <Button loading={saving} onClick={() => submit(false)}>
            {t('pages.notifications.saveDraft')}
          </Button>
          <Button type="primary" loading={saving} onClick={() => submit(true)}>
            {t('pages.notifications.publish')}
          </Button>
        </Space>
      }
    >
      <FormProvider {...methods}>
        <Form layout="vertical" component="div">
          {error && <Alert type="error" showIcon title={error.message} />}
          <Row gutter={16}>
            <Col xs={24} md={18}>
              <FormField name="title" label={t('pages.notifications.title')} required>
                <Input maxLength={120} showCount />
              </FormField>
            </Col>
            <Col xs={24} md={6}>
              <FormField name="sortIndex" label={t('pages.notifications.order')}>
                <InputNumber min={0} max={1000000} precision={0} style={{ width: '100%' }} />
              </FormField>
            </Col>
          </Row>
          <FormField name="body" label={t('pages.notifications.body')} required>
            <Input.TextArea autoSize={{ minRows: 4, maxRows: 10 }} maxLength={4000} showCount />
          </FormField>
          <Row gutter={16}>
            <Col xs={24} md={16}>
              <FormField name="targetType" label={t('pages.notifications.audience')}>
                <Radio.Group
                  options={[
                    { value: 'clients', label: t('menu.clients') },
                    { value: 'groups', label: t('menu.groups') },
                  ]}
                />
              </FormField>
              {targetType === 'clients' ? (
                <FormField name="clientIds" label={t('pages.notifications.selectClients')}>
                  <Select
                    mode="multiple"
                    maxTagCount="responsive"
                    showSearch={{ filterOption: false, onSearch: setSearch }}
                    options={clientOptions}
                    loading={clients.isFetching || selected.isFetching}
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
                            {t('pages.notifications.loadMore')}
                          </Button>
                        )}
                      </>
                    )}
                  />
                </FormField>
              ) : (
                <FormField name="groups" label={t('pages.notifications.selectGroups')}>
                  <Select
                    mode="multiple"
                    maxTagCount="responsive"
                    options={(groups.data ?? []).map((group) => ({
                      value: group.name,
                      label: group.name,
                    }))}
                    loading={groups.isFetching}
                  />
                </FormField>
              )}
            </Col>
            <Col xs={24} md={8}>
              <FormField
                name="publishToNodes"
                valueProp="checked"
                label={t('pages.notifications.toNodes')}
              >
                <Switch />
              </FormField>
              <Alert type="info" title={t('pages.notifications.nodeHelp')} />
            </Col>
          </Row>
        </Form>
      </FormProvider>
    </Modal>
  );
}
