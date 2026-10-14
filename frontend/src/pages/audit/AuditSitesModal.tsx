import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Alert, Button, Modal, Pagination, Table } from 'antd';

import { HttpUtil } from '@/utils';
import { AuditSitesPageSchema } from '@/schemas/audit';
import { formatAuditBytes } from './format';

interface AuditSiteFilters {
  hours: number;
  day: string;
  nodeId: number;
  client: string;
  site: string;
}

interface Props {
  filters: AuditSiteFilters;
  onClose: () => void;
}

export default function AuditSitesModal({ filters, onClose }: Props) {
  const { t } = useTranslation();
  const filterKey = JSON.stringify(filters);
  const [pageState, setPage] = useState({ filterKey, page: 1 });
  const [pageSize, setPageSize] = useState<20 | 50 | 100>(20);
  // Derive the reset page during render, so changed filters never fetch the old page first.
  const page = pageState.filterKey === filterKey ? pageState.page : 1;
  if (pageState.filterKey !== filterKey) {
    setPage({ filterKey, page: 1 });
  }
  const { data, isFetching, error, refetch } = useQuery({
    queryKey: [
      'audit',
      'sites',
      filters.hours,
      filters.day,
      filters.nodeId,
      filters.client,
      filters.site,
      page,
      pageSize,
    ],
    queryFn: async () => {
      const response = await HttpUtil.get(
        '/panel/api/audit/sites',
        { ...filters, page, pageSize },
        { silent: true },
      );
      if (!response.success) throw new Error(response.msg || 'Audit site ranking failed');
      return AuditSitesPageSchema.parse(response.obj);
    },
  });

  return (
    <Modal open title={t('pages.audit.topSites')} onCancel={onClose} footer={null} width={900}>
      {error && (
        <Alert
          type="error"
          showIcon
          title={error.message}
          action={<Button onClick={() => refetch()}>{t('refresh')}</Button>}
        />
      )}
      {data?.centralized && <Alert type="info" showIcon title={t('pages.audit.centralizedHint')} />}
      <Table
        size="small"
        rowKey="name"
        loading={isFetching}
        dataSource={data?.items ?? []}
        scroll={{ x: 700, y: 480 }}
        columns={[
          {
            title: '#',
            key: 'rank',
            width: 60,
            render: (_, _row, index) => (page - 1) * pageSize + index + 1,
          },
          {
            title: t('pages.audit.sites'),
            dataIndex: 'name',
            ellipsis: true,
            render: (value: string) => value || '—',
          },
          {
            title: t('pages.audit.traffic'),
            key: 'traffic',
            render: (_, row) => formatAuditBytes(row.uplink + row.downlink),
          },
          { title: t('pages.audit.upload'), dataIndex: 'uplink', render: formatAuditBytes },
          { title: t('pages.audit.download'), dataIndex: 'downlink', render: formatAuditBytes },
          { title: t('pages.audit.connections'), dataIndex: 'connections' },
          {
            title: t('pages.audit.measured'),
            key: 'measured',
            render: (_, row) => `${row.measured}/${row.connections}`,
          },
        ]}
        pagination={false}
      />
      <Pagination
        current={page}
        pageSize={pageSize}
        total={data?.total ?? 0}
        showSizeChanger
        pageSizeOptions={[20, 50, 100]}
        disabled={isFetching}
        onChange={(nextPage, nextSize) => {
          if (nextSize !== 20 && nextSize !== 50 && nextSize !== 100) return;
          setPage({ filterKey, page: nextSize === pageSize ? nextPage : 1 });
          setPageSize(nextSize);
        }}
        style={{ marginTop: 16, textAlign: 'right' }}
      />
    </Modal>
  );
}
