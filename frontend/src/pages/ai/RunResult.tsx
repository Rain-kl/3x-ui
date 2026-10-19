import { useEffect, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { withBasePath } from '@/api/http-init';
import { AIStreamSchema } from '@/generated/zod';
import { z } from 'zod';
import { useTranslation } from 'react-i18next';
import { Alert, Button, Card, Collapse, Popconfirm, Space, Tag, Typography } from 'antd';
import type { AIRun } from './api';

const TraceSchema = z.array(z.object({ tool: z.string(), status: z.string() }));
export default function RunResult({
  run: initialRun,
  onCancel,
  onDelete,
  deleting = false,
}: {
  run: AIRun;
  onCancel: (id: string) => void;
  onDelete?: (id: string) => void;
  deleting?: boolean;
}) {
  const { t } = useTranslation();
  const cache = useQueryClient();
  const [live, setLive] = useState<z.infer<typeof AIStreamSchema> | null>(null);
  const [streamError, setStreamError] = useState(false);
  const run = live?.run ?? initialRun;
  useEffect(() => {
    if (initialRun.state !== 'running') return;
    const stream = new EventSource(
      withBasePath(`/panel/api/ai/runs/${encodeURIComponent(initialRun.id)}/events`),
    );
    let pending: z.infer<typeof AIStreamSchema> | null = null;
    const flush = () => {
      if (pending) {
        setLive(pending);
        pending = null;
      }
    };
    const timer = window.setInterval(flush, 100);
    stream.addEventListener('open', () => setStreamError(false));
    stream.addEventListener('snapshot', (event: MessageEvent<string>) => {
      try {
        const snapshot = AIStreamSchema.parse(JSON.parse(event.data));
        if (snapshot.run.id !== initialRun.id) return;
        pending = snapshot;
        if (snapshot.run.state !== 'running') {
          flush();
          stream.close();
          void cache.invalidateQueries({ queryKey: ['ai'] });
        }
      } catch {
        setStreamError(true);
        stream.close();
      }
    });
    stream.onerror = () => setStreamError(true);
    return () => {
      window.clearInterval(timer);
      stream.close();
    };
  }, [initialRun.id, initialRun.state, cache]);
  const states: Record<string, string> = {
    running: t('pages.ai.running'),
    completed: t('pages.ai.completed'),
    failed: t('pages.ai.failed'),
    cancelled: t('pages.ai.cancelled'),
    interrupted: t('pages.ai.interrupted'),
  };
  let tools: z.infer<typeof TraceSchema> = [];
  try {
    tools = TraceSchema.parse(JSON.parse(run.tools || '[]'));
  } catch {
    /* Older rows may not contain a trace. */
  }
  return (
    <Card
      size="small"
      className="ai-run"
      title={
        <Space wrap>
          <Tag
            color={
              run.state === 'completed'
                ? 'success'
                : run.state === 'running'
                  ? 'processing'
                  : 'error'
            }
          >
            {states[run.state] ?? run.state}
          </Tag>
          <Typography.Text type="secondary">
            {new Date(run.startedAt).toLocaleString()}
          </Typography.Text>
        </Space>
      }
      extra={
        <Space>
          {run.state === 'running' && (
            <Button size="small" onClick={() => onCancel(run.id)}>
              {t('pages.ai.cancel')}
            </Button>
          )}
          {onDelete && (
            <Popconfirm
              title={t('pages.ai.confirmDelete')}
              onConfirm={() => onDelete(run.id)}
              disabled={run.state === 'running' || deleting}
            >
              <Button size="small" danger disabled={run.state === 'running'} loading={deleting}>
                {t('pages.ai.delete')}
              </Button>
            </Popconfirm>
          )}
        </Space>
      }
    >
      <Typography.Paragraph className="ai-text ai-prompt">{run.prompt}</Typography.Paragraph>
      {run.result && (
        <Typography.Paragraph className="ai-text" copyable>
          {run.result}
        </Typography.Paragraph>
      )}
      {live?.thought && (
        <Collapse
          size="small"
          items={[
            {
              key: 'thought',
              label: t('pages.ai.thinking'),
              children: (
                <Typography.Paragraph className="ai-text">{live.thought}</Typography.Paragraph>
              ),
            },
          ]}
        />
      )}
      {streamError && run.state === 'running' && (
        <Alert type="warning" title={t('pages.ai.streamReconnecting')} />
      )}
      {run.error && <Alert type="error" title={run.error} />}
      {run.notifyError && (
        <Alert type="warning" title={`${t('pages.ai.notifyFailed')}: ${run.notifyError}`} />
      )}
      {tools.length > 0 && (
        <Space wrap>
          {tools.map((tool, index) => (
            <Tag key={`${index}-${tool.tool}`}>
              {tool.tool} · {tool.status}
            </Tag>
          ))}
        </Space>
      )}
    </Card>
  );
}
