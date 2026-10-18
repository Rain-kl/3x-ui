import { useEffect, useRef } from 'react';
import { useTranslation } from 'react-i18next';
import uPlot from 'uplot';
import 'uplot/dist/uPlot.min.css';
import { formatAuditBytes } from './format';

export interface BandwidthPoint {
  time: number;
  upload: number | null;
  download: number | null;
}
// FORK(audit-bandwidth): Keep the live viewport at one hour even when only a few samples exist.
export default function AuditBandwidthPlot({
  points,
  windowSeconds,
}: {
  points: BandwidthPoint[];
  windowSeconds?: number;
}) {
  const root = useRef<HTMLDivElement>(null);
  const chart = useRef<uPlot | null>(null);
  const { t } = useTranslation();
  useEffect(() => {
    if (!root.current) return;
    const format = (value: number) => `${formatAuditBytes(value)}/s`;
    const plot = new uPlot(
      {
        width: root.current.clientWidth || 400,
        height: 280,
        scales: {
          x: {
            time: true,
            ...(windowSeconds
              ? {
                  range: (): [number, number] => {
                    const now = Date.now() / 1000;
                    return [now - windowSeconds, now];
                  },
                }
              : {}),
          },
          y: { auto: true, range: (_plot, _min, max) => [0, Math.max(1, max) * 1.1] },
        },
        series: [
          {},
          {
            label: t('pages.audit.bandwidthUpload'),
            stroke: '#1677ff',
            spanGaps: false,
            value: (_plot, value) => (value == null ? '—' : format(value)),
          },
          {
            label: t('pages.audit.bandwidthDownload'),
            stroke: '#52c41a',
            spanGaps: false,
            value: (_plot, value) => (value == null ? '—' : format(value)),
          },
        ],
        axes: [
          { stroke: '#888', grid: { stroke: 'rgba(128,128,128,.15)' } },
          {
            stroke: '#888',
            size: 85,
            values: (_plot, values) => values.map(format),
            grid: { stroke: 'rgba(128,128,128,.15)' },
          },
        ],
      },
      [[], [], []],
      root.current,
    );
    chart.current = plot;
    const observer = new ResizeObserver((entries) => {
      const width = entries[0]?.contentRect.width;
      if (width) plot.setSize({ width, height: 280 });
    });
    observer.observe(root.current);
    return () => {
      observer.disconnect();
      plot.destroy();
      chart.current = null;
    };
  }, [t, windowSeconds]);
  useEffect(() => {
    chart.current?.setData([
      points.map((point) => point.time),
      points.map((point) => point.upload),
      points.map((point) => point.download),
    ]);
  }, [points, t, windowSeconds]);
  return <div ref={root} style={{ width: '100%' }} aria-label={t('pages.audit.bandwidthTitle')} />;
}
