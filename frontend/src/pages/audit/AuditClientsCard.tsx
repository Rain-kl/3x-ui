import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Alert, Button, Card, Pagination, Table } from 'antd';

import { HttpUtil } from '@/utils';
import { AuditSitesPageSchema } from '@/schemas/audit';
import { formatAuditBytes } from './format';

interface Props {
  filters: { hours: number; day: string; nodeId: number; client: string; site: string };
  onChooseClient: (client: string) => void;
}

export default function AuditClientsCard({ filters, onChooseClient }: Props) {
  const { t } = useTranslation();
  const filterKey = JSON.stringify(filters);
  const [position, setPosition] = useState({ filterKey, page: 1 });
  const [pageSize, setPageSize] = useState<20 | 50 | 100>(20);
  const page = position.filterKey === filterKey ? position.page : 1;
  if (position.filterKey !== filterKey) setPosition({ filterKey, page: 1 });
  const { data, isFetching, error, refetch } = useQuery({
    queryKey: ['audit', 'clients', filters, page, pageSize],
    queryFn: async () => {
      const response = await HttpUtil.get(
        '/panel/api/audit/clients',
        { ...filters, page, pageSize },
        { silent: true },
      );
      if (!response.success) throw new Error(response.msg || t('pages.audit.operationFailed'));
      return AuditSitesPageSchema.parse(response.obj);
    },
    refetchInterval: 15000,
  });

  return (
    <Card size="small" className="audit-overview-card" title={t('pages.audit.clientRanking')}>
      {error && (
        <Alert
          type="error"
          showIcon
          title={error.message}
          action={<Button onClick={() => refetch()}>{t('refresh')}</Button>}
        />
      )}
      <div className="audit-client-ranking" role="region" aria-label={t('pages.audit.client')}>
        <Table
          size="small"
          rowKey="name"
          loading={isFetching}
          dataSource={data?.items ?? []}
          pagination={false}
          columns={[
            {
              title: t('pages.audit.client'),
              dataIndex: 'name',
              render: (value: string) => (
                <Button type="link" onClick={() => onChooseClient(value)}>
                  {value || '—'}
                </Button>
              ),
            },
            { title: t('pages.audit.connections'), dataIndex: 'connections' },
            {
              title: t('pages.audit.traffic'),
              render: (_, row) => formatAuditBytes(row.uplink + row.downlink),
            },
          ]}
        />
      </div>
      <Pagination
        size="small"
        current={page}
        total={data?.total ?? 0}
        pageSize={pageSize}
        showSizeChanger
        pageSizeOptions={[20, 50, 100]}
        disabled={isFetching}
        onChange={(nextPage, nextSize) => {
          if (nextSize !== 20 && nextSize !== 50 && nextSize !== 100) return;
          setPosition({ filterKey, page: nextSize === pageSize ? nextPage : 1 });
          setPageSize(nextSize);
        }}
        style={{ marginTop: 12, flexShrink: 0, justifyContent: 'flex-end' }}
      />
    </Card>
  );
}
