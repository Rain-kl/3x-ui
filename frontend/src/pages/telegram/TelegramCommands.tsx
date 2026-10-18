import { useState } from 'react';
import { FormProvider } from 'react-hook-form';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Alert, Button, Form, Input, Modal, Popconfirm, Space, Switch, Table, message } from 'antd';
import { FormField, useZodForm } from '@/components/form/rhf';
import { TelegramCommandFormSchema, telegramApi, type TelegramCustomCommand } from './api';

function CommandEditor({
  value,
  onSave,
  onClose,
  saving,
}: {
  value: TelegramCustomCommand;
  onSave: (value: TelegramCustomCommand) => Promise<void>;
  onClose: () => void;
  saving: boolean;
}) {
  const { t } = useTranslation();
  const methods = useZodForm(TelegramCommandFormSchema, { defaultValues: value });
  return (
    <Modal
      open
      title={t('pages.telegram.customCommands')}
      onCancel={onClose}
      onOk={methods.handleSubmit(onSave)}
      confirmLoading={saving}
      width={720}
    >
      <FormProvider {...methods}>
        <Form layout="vertical" component="div">
          <FormField name="command" label={t('pages.telegram.commandName')}>
            <Input addonBefore="/" maxLength={33} placeholder="support" />
          </FormField>
          <FormField name="description" label={t('pages.telegram.commandDescription')}>
            <Input maxLength={256} showCount />
          </FormField>
          <FormField name="reply" label={t('pages.telegram.commandReply')}>
            <Input.TextArea rows={7} maxLength={4096} showCount />
          </FormField>
          <FormField name="enabled" label={t('pages.telegram.commandEnabled')} valueProp="checked">
            <Switch />
          </FormField>
        </Form>
      </FormProvider>
    </Modal>
  );
}

export default function TelegramCommands() {
  const { t } = useTranslation();
  const cache = useQueryClient();
  const [toast, holder] = message.useMessage();
  const [editing, setEditing] = useState<{ index: number; value: TelegramCustomCommand } | null>(
    null,
  );
  const [saving, setSaving] = useState(false);
  const query = useQuery({ queryKey: ['telegram', 'commands'], queryFn: telegramApi.commands });
  const rows = query.data?.commands ?? [];
  const save = async (commands: TelegramCustomCommand[]) => {
    setSaving(true);
    try {
      const result = await telegramApi.saveCommands({ commands });
      cache.setQueryData(['telegram', 'commands'], result);
      toast.success(t('pages.telegram.commandsSaved'));
      return true;
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('somethingWentWrong'));
      return false;
    } finally {
      setSaving(false);
    }
  };
  return (
    <>
      {holder}
      <Alert
        type="info"
        showIcon
        title={t('pages.telegram.commandsHelp')}
        style={{ marginBottom: 16 }}
      />
      {query.error && (
        <Alert type="error" title={query.error.message} style={{ marginBottom: 12 }} />
      )}
      {/* FORK(telegram-subscription-details): Reserve a native menu slot for /nodes. */}
      <Button
        type="primary"
        disabled={saving || !query.data || rows.length >= 93}
        style={{ marginBottom: 16 }}
        onClick={() =>
          setEditing({
            index: -1,
            value: {
              command: '',
              description: '',
              reply: '',
              enabled: true,
              sortOrder: rows.length,
            },
          })
        }
      >
        {t('pages.telegram.commandAdd')}
      </Button>
      <Table<TelegramCustomCommand>
        rowKey="command"
        dataSource={rows}
        loading={query.isPending}
        pagination={{ pageSize: 20 }}
        scroll={{ x: 760 }}
        columns={[
          {
            title: t('pages.telegram.commandName'),
            dataIndex: 'command',
            width: 160,
            render: (name: string) => `/${name}`,
          },
          {
            title: t('pages.telegram.commandDescription'),
            dataIndex: 'description',
            ellipsis: true,
          },
          { title: t('pages.telegram.commandReply'), dataIndex: 'reply', ellipsis: true },
          {
            title: t('pages.telegram.commandEnabled'),
            width: 100,
            render: (_, row) => (
              <Switch
                checked={row.enabled}
                disabled={saving}
                onChange={(enabled) => {
                  void save(
                    rows.map((value) =>
                      value.command === row.command ? { ...value, enabled } : value,
                    ),
                  );
                }}
              />
            ),
          },
          {
            title: t('pages.telegram.commandActions'),
            width: 170,
            render: (_, row) => (
              <Space>
                <Button
                  size="small"
                  disabled={saving}
                  onClick={() => setEditing({ index: rows.indexOf(row), value: row })}
                >
                  {t('pages.telegram.commandEdit')}
                </Button>
                <Popconfirm
                  title={t('pages.telegram.commandDeleteConfirm')}
                  onConfirm={async () => {
                    await save(rows.filter((item) => item.command !== row.command));
                  }}
                >
                  <Button danger size="small" disabled={saving}>
                    {t('pages.telegram.commandDelete')}
                  </Button>
                </Popconfirm>
              </Space>
            ),
          },
        ]}
      />
      {editing && (
        <CommandEditor
          key={`${editing.index}:${editing.value.command}`}
          value={editing.value}
          saving={saving}
          onClose={() => setEditing(null)}
          onSave={async (value) => {
            const next = [...rows];
            if (editing.index < 0) next.push(value);
            else next[editing.index] = value;
            if (await save(next)) setEditing(null);
          }}
        />
      )}
    </>
  );
}
