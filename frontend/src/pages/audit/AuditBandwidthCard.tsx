import { useEffect, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Alert, Card, Empty, Segmented, Space, Statistic, Tag, Typography } from 'antd';
import { AuditBandwidthCurrentSchema, AuditBandwidthHistorySchema } from '@/generated/zod';
import { HttpUtil } from '@/utils';
import { formatAuditBytes } from './format';
import AuditBandwidthPlot, { type BandwidthPoint } from './AuditBandwidthPlot';

export default function AuditBandwidthCard({ nodeId, client }: { nodeId: number; client: string }) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [mode, setMode] = useState('live');
  const [live, setLive] = useState<BandwidthPoint[]>([]);
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
    enabled: mode === 'history',
    refetchInterval: 30000,
  });
  useEffect(
    () =>
      queryClient.getQueryCache().subscribe((event) => {
        if (event.type !== 'updated' || !['success', 'error'].includes(event.action.type)) return;
        const key = event.query.queryKey;
        if (key[0] !== 'audit' || key[1] !== 'bandwidth' || key[2] !== nodeId || key[3] !== client)
          return;
        const value = AuditBandwidthCurrentSchema.safeParse(event.query.state.data);
        const fresh = event.action.type === 'success' && value.success && value.data.fresh;
        const point: BandwidthPoint = {
          time: fresh ? value.data.time / 1000 : Date.now() / 1000,
          upload: fresh ? value.data.upload : null,
          download: fresh ? value.data.download : null,
        };
        setLive((previous) =>
          previous.at(-1)?.time === point.time ? previous : [...previous, point].slice(-720),
        );
      }),
    [queryClient, nodeId, client],
  );
  const points =
    mode === 'live'
      ? live
      : (history.data?.points ?? []).map((point) => ({
          time: point.time,
          upload: point.coveredSeconds > 0 ? point.upload : null,
          download: point.coveredSeconds > 0 ? point.download : null,
        }));
  const error = current.error || history.error;
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
        <AuditBandwidthPlot points={points} />
      ) : (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} />
      )}
    </Card>
  );
}
