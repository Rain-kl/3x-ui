import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Alert, Card, Empty, Segmented, Space, Statistic, Tag, Typography } from 'antd';
import { AuditBandwidthCurrentSchema, AuditBandwidthHistorySchema } from '@/generated/zod';
import { HttpUtil } from '@/utils';
import { formatAuditBytes } from './format';
import AuditBandwidthPlot from './AuditBandwidthPlot';

export default function AuditBandwidthCard({ nodeId, client }: { nodeId: number; client: string }) {
  const { t } = useTranslation();
  const [mode, setMode] = useState('live');
  const current = useQuery({
    queryKey: ['audit', 'bandwidth', nodeId, client],
    queryFn: async () => {
      const response = await HttpUtil.get(
        '/panel/api/audit/bandwidth',
        { nodeId, client },
        { silent: true },
      );
      if (!response.success) throw new Error(response.msg);
      return AuditBandwidthCurrentSchema.parse(response.obj);
    },
    refetchInterval: 5000,
  });
  const history = useQuery({
    queryKey: ['audit', 'bandwidth-history', nodeId, client],
    queryFn: async () => {
      const response = await HttpUtil.get(
        '/panel/api/audit/bandwidth/history',
        { nodeId, client },
        { silent: true },
      );
      if (!response.success) throw new Error(response.msg);
      return AuditBandwidthHistorySchema.parse(response.obj);
    },
    refetchInterval: 30000,
    enabled: mode === 'history',
  });
  // FORK(audit-bandwidth): Use server samples for every render so reloads never substitute minute averages.
  const recent = useQuery({
    queryKey: ['audit', 'bandwidth-recent', nodeId, client],
    queryFn: async () => {
      const response = await HttpUtil.get(
        '/panel/api/audit/bandwidth/recent',
        { nodeId, client },
        { silent: true },
      );
      if (!response.success) throw new Error(response.msg);
      return AuditBandwidthHistorySchema.parse(response.obj);
    },
    refetchInterval: 5000,
    enabled: mode === 'live',
  });
  const points = ((mode === 'live' ? recent.data : history.data)?.points ?? []).map((point) => ({
    time: point.time,
    upload: point.coveredSeconds > 0 ? point.upload : null,
    download: point.coveredSeconds > 0 ? point.download : null,
  }));
  const error = current.error || (mode === 'live' ? recent.error : history.error);
  return (
    <Card
      size="small"
      title={t('pages.audit.bandwidthTitle')}
      extra={<Tag>{client || t('pages.audit.allClients')}</Tag>}
    >
      <Typography.Paragraph type="secondary">{t('pages.audit.bandwidthHelp')}</Typography.Paragraph>
      <Space wrap size="large" style={{ marginBottom: 16 }}>
        <Statistic
          title={t('pages.audit.bandwidthUpload')}
          value={
            current.data?.fresh && !current.isError
              ? `${formatAuditBytes(current.data.upload)}/s`
              : '—'
          }
        />
        <Statistic
          title={t('pages.audit.bandwidthDownload')}
          value={
            current.data?.fresh && !current.isError
              ? `${formatAuditBytes(current.data.download)}/s`
              : '—'
          }
        />
        <Segmented
          value={mode}
          onChange={(value) => setMode(String(value))}
          options={[
            { value: 'live', label: t('pages.audit.bandwidthLive') },
            { value: 'history', label: t('pages.audit.bandwidthDay') },
          ]}
        />
      </Space>
      {error && <Alert type="error" title={error.message} />}
      {(!current.data?.fresh || current.isError || !!current.data.staleNodes) && (
        <Alert type="info" title={t('pages.audit.bandwidthMissing')} style={{ marginBottom: 12 }} />
      )}
      {points.some((point) => point.upload !== null || point.download !== null) ? (
        <AuditBandwidthPlot points={points} windowSeconds={mode === 'live' ? 3600 : undefined} />
      ) : (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} />
      )}
    </Card>
  );
}
