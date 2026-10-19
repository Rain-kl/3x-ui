import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import {
  Alert,
  Button,
  Col,
  Empty,
  Input,
  Pagination,
  Popconfirm,
  Row,
  Select,
  Space,
  Typography,
} from 'antd';
import { aiApi, type AIModelOption } from './api';
import RunResult from './RunResult';

export default function AIChat({
  models,
  defaultModel,
  enabled,
}: {
  models: AIModelOption[];
  defaultModel: string;
  enabled: boolean;
}) {
  const { t } = useTranslation();
  const cache = useQueryClient();
  const [id, setID] = useState('');
  const [listPage, setListPage] = useState(1);
  const [page, setPage] = useState(1);
  const [prompt, setPrompt] = useState('');
  const [model, setModel] = useState('');
  const sessions = useQuery({
    queryKey: ['ai', 'sessions', listPage],
    queryFn: () => aiApi.sessions(listPage),
  });
  const conversation = useQuery({
    queryKey: ['ai', 'session', id, page],
    queryFn: () => aiApi.session(id, page),
    enabled: !!id,
  });
  const create = useMutation({
    mutationFn: () => aiApi.createSession(t('pages.ai.newChat')),
    onSuccess: (row) => {
      setID(row.id);
      setPage(1);
      void cache.invalidateQueries({ queryKey: ['ai', 'sessions'] });
    },
  });
  const send = useMutation({
    mutationFn: () => aiApi.send(id, prompt, model || defaultModel),
    onSuccess: () => {
      setPrompt('');
      setPage(1);
      void cache.invalidateQueries({ queryKey: ['ai', 'session', id] });
      void cache.invalidateQueries({ queryKey: ['ai', 'tasks'] });
    },
  });
  const cancel = useMutation({
    mutationFn: aiApi.cancel,
    onSuccess: () => {
      void cache.invalidateQueries({ queryKey: ['ai', 'session', id] });
    },
  });
  const remove = useMutation({
    mutationFn: () => aiApi.deleteSession(id),
    onSuccess: () => {
      setID('');
      void cache.invalidateQueries({ queryKey: ['ai', 'sessions'] });
    },
  });
  const error =
    sessions.error ||
    conversation.error ||
    create.error ||
    send.error ||
    cancel.error ||
    remove.error;
  const active = conversation.data?.runs.items.some((run) => run.state === 'running');
  return (
    <Space orientation="vertical" style={{ width: '100%' }} size="middle">
      {error && <Alert type="error" title={error.message} />}
      <Row gutter={[16, 16]}>
        <Col xs={24} md={6}>
          <Space orientation="vertical" style={{ width: '100%' }}>
            <Button block onClick={() => create.mutate()} loading={create.isPending}>
              {t('pages.ai.newChat')}
            </Button>
            <div className="ai-session-list">
              {sessions.data?.items.map((row) => (
                <Button
                  key={row.id}
                  block
                  type={id === row.id ? 'primary' : 'text'}
                  onClick={() => {
                    setID(row.id);
                    setPage(1);
                  }}
                >
                  {row.title}
                </Button>
              ))}
            </div>
            <Pagination
              simple
              size="small"
              current={listPage}
              pageSize={20}
              total={sessions.data?.total ?? 0}
              onChange={setListPage}
            />
          </Space>
        </Col>
        <Col xs={24} md={18}>
          <Space orientation="vertical" style={{ width: '100%' }} size="middle">
            {!id ? (
              <Empty description={t('pages.ai.selectChat')} />
            ) : (
              <>
                <Space wrap>
                  <Typography.Text strong>{conversation.data?.conversation.title}</Typography.Text>
                  <Popconfirm title={t('pages.ai.confirmDelete')} onConfirm={() => remove.mutate()}>
                    <Button danger size="small" disabled={active}>
                      {t('pages.ai.delete')}
                    </Button>
                  </Popconfirm>
                </Space>
                <div className="ai-chat-history">
                  {[...(conversation.data?.runs.items ?? [])].reverse().map((run) => (
                    <RunResult key={run.id} run={run} onCancel={(value) => cancel.mutate(value)} />
                  ))}
                </div>
                <Pagination
                  current={page}
                  pageSize={20}
                  total={conversation.data?.runs.total ?? 0}
                  onChange={setPage}
                  size="small"
                />
                <Select
                  style={{ width: '100%' }}
                  allowClear
                  showSearch
                  optionFilterProp="label"
                  value={model || undefined}
                  placeholder={defaultModel || t('pages.ai.openCodeDefault')}
                  onChange={(value) => setModel(value ?? '')}
                  options={models.map((item) => ({
                    value: item.id,
                    label: `${item.name} (${item.id})`,
                  }))}
                />
                <Input.TextArea
                  value={prompt}
                  onChange={(event) => setPrompt(event.target.value)}
                  autoSize={{ minRows: 3, maxRows: 8 }}
                  maxLength={16000}
                  placeholder={t('pages.ai.chatPlaceholder')}
                />
                <Button
                  type="primary"
                  loading={send.isPending}
                  disabled={!enabled || active || !prompt.trim()}
                  onClick={() => send.mutate()}
                >
                  {t('pages.ai.send')}
                </Button>
              </>
            )}
          </Space>
        </Col>
      </Row>
    </Space>
  );
}
