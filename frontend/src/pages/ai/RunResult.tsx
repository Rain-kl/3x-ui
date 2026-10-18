import { z } from 'zod';
import { useTranslation } from 'react-i18next';
import { Alert, Button, Card, Space, Tag, Typography } from 'antd';
import type { AIRun } from './api';

const TraceSchema = z.array(z.object({ tool: z.string(), status: z.string() }));
export default function RunResult({
  run,
  onCancel,
}: {
  run: AIRun;
  onCancel: (id: string) => void;
}) {
  const { t } = useTranslation();
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
        run.state === 'running' && (
          <Button size="small" onClick={() => onCancel(run.id)}>
            {t('pages.ai.cancel')}
          </Button>
        )
      }
    >
      <Typography.Paragraph className="ai-text ai-prompt">{run.prompt}</Typography.Paragraph>
      {run.result && (
        <Typography.Paragraph className="ai-text" copyable>
          {run.result}
        </Typography.Paragraph>
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
