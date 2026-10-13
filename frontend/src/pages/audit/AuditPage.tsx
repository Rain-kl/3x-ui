import { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import {
  Alert,
  Button,
  Card,
  Col,
  ConfigProvider,
  Input,
  Layout,
  Pagination,
  Progress,
  Row,
  Select,
  Space,
  Statistic,
  Table,
  Tag,
  Typography,
} from 'antd';

import AppSidebar from '@/layouts/AppSidebar';
import { useTheme } from '@/hooks/useTheme';
import { HttpUtil } from '@/utils';
import { AuditSummarySchema, type AuditSummary } from '@/schemas/audit';
import './AuditPage.css';

function bytes(value: number): string {
  if (value < 1024) return `${value} B`;
  const units = ['KiB', 'MiB', 'GiB', 'TiB'];
  let scaled = value;
  let index = -1;
  do {
    scaled /= 1024;
    index++;
  } while (scaled >= 1024 && index < units.length - 1);
  return `${scaled.toFixed(1)} ${units[index]}`;
}

function Trend({ points }: { points: AuditSummary['hours'] }) {
  const max = Math.max(1, ...points.map((p) => p.connections));
  const coords = points
    .map(
      (p, i) => `${(i / Math.max(points.length - 1, 1)) * 100},${100 - (p.connections / max) * 90}`,
    )
    .join(' ');
  return (
    <div className="audit-trend">
      <svg
        viewBox="0 0 100 100"
        preserveAspectRatio="none"
        role="img"
        aria-label="Connections by hour"
      >
        <polyline
          points={coords}
          fill="none"
          stroke="var(--ant-color-primary)"
          strokeWidth="2"
          vectorEffect="non-scaling-stroke"
        />
      </svg>
      <div>
        <span>{points[0]?.name?.slice(11, 16) ?? ''}</span>
        <span>{points.at(-1)?.name?.slice(11, 16) ?? ''}</span>
      </div>
    </div>
  );
}

export default function AuditPage() {
  const { t } = useTranslation();
  const { antdThemeConfig } = useTheme();
  const [hours, setHours] = useState(24);
  const [nodeId, setNodeId] = useState(0);
  const [clientInput, setClientInput] = useState('');
  const [client, setClient] = useState('');
  const [siteInput, setSiteInput] = useState('');
  const [site, setSite] = useState('');
  const [offset, setOffset] = useState(0);
  const { data, isFetching, error, refetch } = useQuery({
    queryKey: ['audit', hours, nodeId, client, site, offset],
    queryFn: async () => {
      const response = await HttpUtil.get(
        '/panel/api/audit/summary',
        { hours, nodeId, client, site, offset },
        { silent: true },
      );
      if (!response.success) throw new Error(response.msg || 'Audit query failed');
      return AuditSummarySchema.parse(response.obj);
    },
    refetchInterval: 15000,
  });
  const nodes = useMemo(
    () => new Map(data?.nodes.map((node) => [node.id, node.name]) ?? []),
    [data?.nodes],
  );
  const totalMeasuredBytes = Math.max(1, (data?.uplink ?? 0) + (data?.downlink ?? 0));
  const suspicious = data?.alerts ?? [];
  const chooseClient = (value: string) => {
    setClient(value);
    setClientInput(value);
    setOffset(0);
  };
  const chooseSite = (value: string) => {
    setSite(value);
    setSiteInput(value);
    setOffset(0);
  };

  return (
    <ConfigProvider theme={antdThemeConfig}>
      <Layout className="audit-page">
        <AppSidebar />
        <Layout className="content-shell">
          <Layout.Content id="content-layout" className="content-area">
            <Space direction="vertical" size="large" style={{ width: '100%' }}>
              <div className="audit-header">
                <div>
                  <Typography.Title level={2}>{t('pages.audit.title')}</Typography.Title>
                  <Typography.Text type="secondary">{t('pages.audit.subtitle')}</Typography.Text>
                </div>
                <Button loading={isFetching} onClick={() => refetch()}>
                  {t('refresh')}
                </Button>
              </div>
              <Space wrap>
                <Select
                  value={hours}
                  onChange={(value) => {
                    setHours(value);
                    setOffset(0);
                  }}
                  options={[3, 12, 24].map((n) => ({ label: `${n} ${t('hour')}`, value: n }))}
                  style={{ width: 145 }}
                />
                <Select
                  value={nodeId}
                  onChange={(value) => {
                    setNodeId(value);
                    setOffset(0);
                  }}
                  options={[
                    { label: t('pages.audit.allNodes'), value: 0 },
                    ...(data?.nodes.map((node) => ({ label: node.name, value: node.id })) ?? []),
                  ]}
                  style={{ minWidth: 150 }}
                />
                <Input.Search
                  value={clientInput}
                  onChange={(e) => setClientInput(e.target.value)}
                  onSearch={chooseClient}
                  placeholder={t('pages.audit.clientFilter')}
                  allowClear
                  style={{ width: 210 }}
                />
                <Input.Search
                  value={siteInput}
                  onChange={(e) => setSiteInput(e.target.value)}
                  onSearch={chooseSite}
                  placeholder={t('pages.audit.siteFilter')}
                  allowClear
                  style={{ width: 210 }}
                />
              </Space>
              {error && <Alert type="error" showIcon title={error.message} />}
              {data && (
                <>
                  {data.measured < data.connections && (
                    <Alert
                      type="warning"
                      showIcon
                      title={t('pages.audit.unmeasured', {
                        count: data.connections - data.measured,
                      })}
                    />
                  )}
                  <Row gutter={[16, 16]}>
                    <Col xs={12} lg={6}>
                      <Card>
                        <Statistic title={t('pages.audit.connections')} value={data.connections} />
                      </Card>
                    </Col>
                    <Col xs={12} lg={6}>
                      <Card>
                        <Statistic title={t('pages.audit.upload')} value={bytes(data.uplink)} />
                      </Card>
                    </Col>
                    <Col xs={12} lg={6}>
                      <Card>
                        <Statistic title={t('pages.audit.download')} value={bytes(data.downlink)} />
                      </Card>
                    </Col>
                    <Col xs={12} lg={6}>
                      <Card>
                        <Statistic
                          title={t('pages.audit.measured')}
                          value={`${data.measured}/${data.connections}`}
                        />
                      </Card>
                    </Col>
                  </Row>
                  {suspicious.length > 0 && (
                    <Alert
                      type="warning"
                      showIcon
                      title={t('pages.audit.reviewHighFrequency')}
                      description={suspicious.map((item) => (
                        <Button key={item.name} type="link" onClick={() => chooseClient(item.name)}>
                          {item.name}: {item.connections}{' '}
                          {t('pages.audit.connections').toLowerCase()}, {item.uniqueSites ?? 0}{' '}
                          {t('pages.audit.sites').toLowerCase()}
                        </Button>
                      ))}
                    />
                  )}
                  <Row gutter={[16, 16]}>
                    <Col xs={24} xl={12}>
                      <Card title={t('pages.audit.topSites')}>
                        <div className="audit-ranking">
                          {data.sites.map((item) => (
                            <button
                              className="audit-rank"
                              key={item.name}
                              onClick={() => chooseSite(item.name)}
                            >
                              <span>{item.name || '—'}</span>
                              <span>
                                {bytes(item.uplink + item.downlink)} ·{' '}
                                {Math.round(
                                  ((item.uplink + item.downlink) / totalMeasuredBytes) * 100,
                                )}
                                % · {item.connections}
                              </span>
                              <Progress
                                percent={Math.round(
                                  ((item.uplink + item.downlink) / totalMeasuredBytes) * 100,
                                )}
                                showInfo={false}
                                size="small"
                              />
                            </button>
                          ))}
                        </div>
                      </Card>
                    </Col>
                    <Col xs={24} xl={12}>
                      <Card title={t('pages.audit.hourlyTrend')}>
                        <Trend points={data.hours} />
                        <Table
                          size="small"
                          pagination={false}
                          rowKey="name"
                          dataSource={data.clients.slice(0, 8)}
                          columns={[
                            {
                              title: t('pages.audit.client'),
                              dataIndex: 'name',
                              render: (value: string) => (
                                <Button type="link" onClick={() => chooseClient(value)}>
                                  {value || '—'}
                                </Button>
                              ),
                            },
                            { title: t('pages.audit.connections'), dataIndex: 'connections' },
                            {
                              title: t('pages.audit.traffic'),
                              render: (_, row) => bytes(row.uplink + row.downlink),
                            },
                          ]}
                        />
                      </Card>
                    </Col>
                  </Row>
                  <Card title={t('pages.audit.recentEvents')}>
                    <Table
                      size="small"
                      rowKey="id"
                      pagination={false}
                      scroll={{ x: 1100 }}
                      dataSource={data.events}
                      columns={[
                        {
                          title: t('pages.audit.time'),
                          dataIndex: 'time',
                          render: (value: string) => new Date(value).toLocaleString(),
                        },
                        {
                          title: t('pages.audit.node'),
                          dataIndex: 'nodeId',
                          render: (value: number) => nodes.get(value) || t('pages.audit.localNode'),
                        },
                        { title: t('pages.audit.inbound'), dataIndex: 'inbound' },
                        {
                          title: t('pages.audit.client'),
                          dataIndex: 'client',
                          render: (value: string) => (
                            <Button type="link" onClick={() => chooseClient(value)}>
                              {value || '—'}
                            </Button>
                          ),
                        },
                        { title: t('pages.audit.source'), dataIndex: 'source' },
                        {
                          title: t('pages.audit.destination'),
                          dataIndex: 'destination',
                          // FORK(sniffed-audit-domains): Show domain attribution beside the original target.
                          render: (value: string, record: AuditSummary['events'][number]) => (
                            <div>
                              <div>{value}</div>
                              {record.host && !value.includes(record.host) && (
                                <Button type="link" onClick={() => chooseSite(record.site)}>
                                  {record.host}
                                </Button>
                              )}
                            </div>
                          ),
                        },
                        {
                          title: t('pages.audit.upload'),
                          dataIndex: 'uplink',
                          render: (value: number | null) =>
                            value === null ? (
                              <Tag>{t('pages.audit.unavailable')}</Tag>
                            ) : (
                              bytes(value)
                            ),
                        },
                        {
                          title: t('pages.audit.download'),
                          dataIndex: 'downlink',
                          render: (value: number | null) =>
                            value === null ? (
                              <Tag>{t('pages.audit.unavailable')}</Tag>
                            ) : (
                              bytes(value)
                            ),
                        },
                      ]}
                    />
                    <Pagination
                      current={Math.floor(offset / 100) + 1}
                      pageSize={100}
                      total={data.total}
                      showSizeChanger={false}
                      onChange={(page) => setOffset((page - 1) * 100)}
                      style={{ marginTop: 16, textAlign: 'right' }}
                    />
                  </Card>
                </>
              )}
            </Space>
          </Layout.Content>
        </Layout>
      </Layout>
    </ConfigProvider>
  );
}
