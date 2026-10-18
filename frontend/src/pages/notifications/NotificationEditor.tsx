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
    defaultValues: {
      ...value,
      targetType: 'both',
      clientIds: value.targetType === 'groups' ? [] : value.clientIds,
      groups: value.targetType === 'clients' ? [] : value.groups,
    },
  });
  const ids = useWatch({ control: methods.control, name: 'clientIds' });
  const [selectingAll, setSelectingAll] = useState(false);
  const [bulkError, setBulkError] = useState<Error | null>(null);
  const [allClients, setAllClients] = useState<Awaited<ReturnType<typeof notificationClients>>>([]);
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
  });
  const selected = useQuery({
    queryKey: ['notification-selected-clients', ids],
    queryFn: () => notificationClients('', 1, ids),
    enabled: ids.length > 0,
  });
  const groups = useQuery({
    queryKey: ['notification-groups'],
    queryFn: async () => {
      const msg = await HttpUtil.get('/panel/api/clients/groups', undefined, { silent: true });
      if (!msg.success) throw new Error(msg.msg);
      return GroupSummaryListSchema.parse(msg.obj);
    },
  });
  const clientOptions = useMemo(() => {
    const rows = new Map((selected.data ?? []).map((row) => [row.id, row]));
    for (const row of allClients) rows.set(row.id, row);
    for (const row of clients.data?.pages.flat() ?? []) rows.set(row.id, row);
    return [...rows.values()].map((row) => ({ value: row.id, label: row.email }));
  }, [selected.data, clients.data, allClients]);

  const selectAllClients = async () => {
    setSelectingAll(true);
    setBulkError(null);
    try {
      const rows = new Map<number, Awaited<ReturnType<typeof notificationClients>>[number]>();
      for (let page = 1; ; page++) {
        const batch = await notificationClients('', page);
        for (const row of batch) rows.set(row.id, row);
        if (rows.size > 2000) throw new Error(t('pages.notifications.audienceLimit'));
        if (batch.length < 50) break;
      }
      setAllClients([...rows.values()]);
      methods.setValue('clientIds', [...rows.keys()], { shouldDirty: true, shouldValidate: true });
    } catch (error) {
      setBulkError(error instanceof Error ? error : new Error(t('somethingWentWrong')));
    } finally {
      setSelectingAll(false);
    }
  };
  const selectAllGroups = () => {
    const names = (groups.data ?? []).map((group) => group.name).filter(Boolean);
    if (names.length > 2000) {
      setBulkError(new Error(t('pages.notifications.audienceLimit')));
      return;
    }
    setBulkError(null);
    methods.setValue('groups', names, { shouldDirty: true, shouldValidate: true });
  };
  const submit = (published: boolean) => {
    methods.setValue('published', published);
    void methods.handleSubmit(async (data) => {
      if (published && data.clientIds.length === 0 && data.groups.length === 0) {
        methods.setError('clientIds', { message: 'pages.notifications.chooseAudience' });
        return;
      }
      await onSave(data);
    })();
  };
  const error = bulkError || clients.error || selected.error || groups.error;
  return (
    <Modal
      open
      title={t(value.id ? 'pages.notifications.edit' : 'pages.notifications.create')}
      onCancel={onClose}
      width={840}
      maskClosable={!saving && !selectingAll}
      closable={!saving && !selectingAll}
      keyboard={!saving && !selectingAll}
      footer={
        <Space wrap>
          <Button disabled={saving || selectingAll} onClick={onClose}>
            {t('cancel')}
          </Button>
          <Button loading={saving} disabled={selectingAll} onClick={() => submit(false)}>
            {t('pages.notifications.saveDraft')}
          </Button>
          <Button
            type="primary"
            loading={saving}
            disabled={selectingAll}
            onClick={() => submit(true)}
          >
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
              <Alert
                type="info"
                title={t('pages.notifications.audienceHelp')}
                style={{ marginBottom: 12 }}
              />
              <FormField
                name="clientIds"
                label={t('pages.notifications.selectClients')}
                extra={
                  <Space>
                    <Button
                      size="small"
                      loading={selectingAll}
                      disabled={saving}
                      onClick={() => void selectAllClients()}
                    >
                      {t('pages.notifications.selectAllClients')}
                    </Button>
                    <Button
                      size="small"
                      disabled={saving || selectingAll}
                      onClick={() =>
                        methods.setValue('clientIds', [], {
                          shouldDirty: true,
                          shouldValidate: true,
                        })
                      }
                    >
                      {t('pages.notifications.clearSelection')}
                    </Button>
                  </Space>
                }
              >
                <Select
                  mode="multiple"
                  maxTagCount="responsive"
                  disabled={saving || selectingAll}
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
              <FormField
                name="groups"
                label={t('pages.notifications.selectGroups')}
                extra={
                  <Space>
                    <Button
                      size="small"
                      disabled={saving || groups.isFetching || !!groups.error}
                      onClick={selectAllGroups}
                    >
                      {t('pages.notifications.selectAllGroups')}
                    </Button>
                    <Button
                      size="small"
                      disabled={saving}
                      onClick={() =>
                        methods.setValue('groups', [], { shouldDirty: true, shouldValidate: true })
                      }
                    >
                      {t('pages.notifications.clearSelection')}
                    </Button>
                  </Space>
                }
              >
                <Select
                  mode="multiple"
                  maxTagCount="responsive"
                  disabled={saving}
                  showSearch={{ optionFilterProp: 'label' }}
                  options={(groups.data ?? []).map((group) => ({
                    value: group.name,
                    label: group.name,
                  }))}
                  loading={groups.isFetching}
                />
              </FormField>
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
