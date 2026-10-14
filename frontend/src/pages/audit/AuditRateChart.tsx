import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { Alert, Empty, Typography } from 'antd';
import Sparkline from '@/components/viz/Sparkline';
import type { AuditRateHistory, AuditRetention } from '@/schemas/audit';

export default function AuditRateChart({
  history,
  policy,
  client,
}: {
  history: AuditRateHistory;
  policy: AuditRetention;
  client: string;
}) {
  const { t } = useTranslation();
  const availableFrom = Date.parse(history.availableFrom);
  const points = useMemo(
    () => history.points.filter((point) => point.time * 1000 >= availableFrom),
    [history.points, availableFrom],
  );
  const connectionRates = useMemo(() => points.map((point) => point.connections / 60), [points]);
  const siteRates = useMemo(() => points.map((point) => point.sites / 60), [points]);
  const labels = useMemo(
    () =>
      points.map((point) =>
        new Date(point.time * 1000).toLocaleString(undefined, {
          month: '2-digit',
          day: '2-digit',
          hour: '2-digit',
          minute: '2-digit',
          hourCycle: 'h23',
        }),
      ),
    [points],
  );
  const lines = [
    {
      y: policy.connectionRateLimit,
      label: t('pages.audit.connectionRateLimit'),
      color: '#fa541c',
      dash: '4 4',
    },
    {
      y: policy.siteRateLimit,
      label: t('pages.audit.siteRateLimit'),
      color: '#722ed1',
      dash: '4 4',
    },
  ].filter((line) => client && line.y > 0);
  const valueMax =
    Math.max(1, ...connectionRates, ...siteRates, ...lines.map((line) => line.y)) * 1.1;

  return (
    <>
      <Typography.Paragraph type="secondary">
        {t('pages.audit.rateHistoryHelp')}
      </Typography.Paragraph>
      {Date.parse(history.availableFrom) > Date.parse(history.windowStart) && (
        <Alert
          type="info"
          showIcon
          title={t('pages.audit.rateHistoryPartial', {
            time: new Date(history.availableFrom).toLocaleString(),
          })}
          style={{ marginBottom: 12 }}
        />
      )}
      {points.length === 0 ? (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} />
      ) : (
        <Sparkline
          data={connectionRates}
          data2={siteRates}
          labels={labels}
          name1={t('pages.audit.connectionsPerSecond')}
          name2={t('pages.audit.sitesPerSecond')}
          height={260}
          maxPoints={1440}
          valueMin={0}
          valueMax={valueMax}
          showAxes
          showGrid
          showTooltip
          showLegend
          tickCountX={4}
          referenceLines={lines}
          tooltipFormatter={(value) => value.toFixed(3)}
          yFormatter={(value) => value.toFixed(2)}
          tooltipLabelFormatter={(label) => label}
        />
      )}
    </>
  );
}
