import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Alert, Button, Pagination, Popconfirm, Select, Space, Table, Tag, Typography } from 'antd';
import { aiApi, TaskFormSchema, type AITask, type AIModelOption } from './api';
import AITaskEditor from './AITaskEditor';
import RunResult from './RunResult';
import type { z } from 'zod';

export default function AITasks({
  models,
  enabled,
}: {
  models: AIModelOption[];
  enabled: boolean;
}) {
  const { t } = useTranslation();
  const cache = useQueryClient();
  const [page, setPage] = useState(1);
  const [runPage, setRunPage] = useState(1);
  const [taskID, setTaskID] = useState('');
  const [edit, setEdit] = useState<z.infer<typeof TaskFormSchema> | null>(null);
  const tasks = useQuery({
    queryKey: ['ai', 'tasks', page],
    queryFn: () => aiApi.tasks(page),
    refetchInterval: 30000,
  });
  const runs = useQuery({
    queryKey: ['ai', 'runs', taskID, runPage],
    queryFn: () => aiApi.runs(runPage, taskID),
    refetchInterval: (query) =>
      query.state.data?.items.some((run) => run.state === 'running') ? 2000 : 30000,
  });
  const refresh = () => {
    void cache.invalidateQueries({ queryKey: ['ai', 'tasks'] });
    void cache.invalidateQueries({ queryKey: ['ai', 'runs'] });
  };
  const save = useMutation({
    mutationFn: aiApi.saveTask,
    onSuccess: () => {
      setEdit(null);
      refresh();
    },
  });
  const run = useMutation({
    mutationFn: aiApi.runTask,
    onSuccess: (value) => {
      setTaskID(value.taskId);
      setRunPage(1);
      refresh();
    },
  });
  const remove = useMutation({ mutationFn: aiApi.deleteTask, onSuccess: refresh });
  const cancel = useMutation({ mutationFn: aiApi.cancel, onSuccess: refresh });
  const error =
    tasks.error || runs.error || save.error || run.error || remove.error || cancel.error;
  return (
    <Space orientation="vertical" style={{ width: '100%' }} size="middle">
      {error && <Alert type="error" title={error.message} />}
      <Button
        type="primary"
        onClick={() =>
          setEdit({
            id: '',
            name: '',
            prompt: '',
            model: '',
            intervalHours: 12,
            enabled: true,
            telegram: true,
          })
        }
      >
        {t('pages.ai.newTask')}
      </Button>
      <Table<AITask>
        rowKey="id"
        loading={tasks.isFetching}
        dataSource={tasks.data?.items ?? []}
        scroll={{ x: 900 }}
        pagination={{
          current: page,
          total: tasks.data?.total ?? 0,
          pageSize: 20,
          showSizeChanger: false,
          onChange: setPage,
        }}
        columns={[
          { title: t('pages.ai.taskName'), dataIndex: 'name' },
          { title: t('pages.ai.goal'), dataIndex: 'prompt', ellipsis: true },
          { title: t('pages.ai.intervalHours'), dataIndex: 'intervalHours', width: 120 },
          {
            title: t('pages.ai.nextRun'),
            dataIndex: 'nextRun',
            render: (value: string) => new Date(value).toLocaleString(),
          },
          {
            title: t('pages.ai.status'),
            render: (_, row) => (
              <Tag color={row.enabled ? 'success' : 'default'}>
                {t(row.enabled ? 'pages.ai.enabled' : 'pages.ai.paused')}
              </Tag>
            ),
          },
          {
            title: t('pages.ai.actions'),
            width: 280,
            render: (_, row) => (
              <Space wrap>
                <Button
                  size="small"
                  onClick={() => {
                    save.reset();
                    setEdit(TaskFormSchema.parse(row));
                  }}
                >
                  {t('pages.ai.edit')}
                </Button>
                <Button
                  size="small"
                  disabled={!enabled}
                  loading={run.isPending && run.variables === row.id}
                  onClick={() => run.mutate(row.id)}
                >
                  {t('pages.ai.runNow')}
                </Button>
                <Button
                  size="small"
                  onClick={() => {
                    setTaskID(row.id);
                    setRunPage(1);
                  }}
                >
                  {t('pages.ai.history')}
                </Button>
                <Popconfirm
                  title={t('pages.ai.confirmDelete')}
                  onConfirm={() => remove.mutate(row.id)}
                >
                  <Button size="small" danger>
                    {t('pages.ai.delete')}
                  </Button>
                </Popconfirm>
              </Space>
            ),
          },
        ]}
      />
      <Typography.Title level={5}>{t('pages.ai.history')}</Typography.Title>
      <Select
        allowClear
        style={{ minWidth: 240 }}
        value={taskID || undefined}
        placeholder={t('pages.ai.allTasks')}
        onChange={(value) => {
          setTaskID(value ?? '');
          setRunPage(1);
        }}
        options={(tasks.data?.items ?? []).map((row) => ({ value: row.id, label: row.name }))}
      />
      {runs.data?.items.map((item) => (
        <RunResult key={item.id} run={item} onCancel={(id) => cancel.mutate(id)} />
      ))}
      <Pagination
        current={runPage}
        pageSize={20}
        total={runs.data?.total ?? 0}
        onChange={setRunPage}
      />
      {edit && (
        <AITaskEditor
          value={edit}
          models={models}
          saving={save.isPending}
          error={save.error?.message}
          onClose={() => setEdit(null)}
          onSave={(value) => save.mutate(value)}
        />
      )}
    </Space>
  );
}
