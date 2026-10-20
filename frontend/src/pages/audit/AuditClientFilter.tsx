import { useEffect, useMemo, useState } from 'react';
import { useInfiniteQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Alert, Button, Select, Space } from 'antd';

import { ClientPageResponseSchema } from '@/schemas/client';
import { HttpUtil } from '@/utils';

export default function AuditClientFilter({
  value,
  onChange,
}: {
  value: string;
  onChange: (value: string) => void;
}) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState('');
  const [debounced, setDebounced] = useState('');
  useEffect(() => {
    const timer = setTimeout(() => setDebounced(search.trim()), 300);
    return () => clearTimeout(timer);
  }, [search]);
  const clients = useInfiniteQuery({
    queryKey: ['audit-client-options', debounced],
    initialPageParam: 1,
    enabled: open,
    staleTime: 30_000,
    queryFn: async ({ pageParam }) => {
      const response = await HttpUtil.get(
        '/panel/api/clients/list/paged',
        { search: debounced, page: pageParam, pageSize: 50, sort: 'email', order: 'ascend' },
        { silent: true },
      );
      if (!response.success) throw new Error(response.msg || t('pages.audit.operationFailed'));
      return ClientPageResponseSchema.parse(response.obj);
    },
    getNextPageParam: (last) =>
      last.page * last.pageSize < last.filtered ? last.page + 1 : undefined,
  });
  const options = useMemo(() => {
    const emails = new Set(
      clients.data?.pages.flatMap((page) => page.items.map((row) => row.email)),
    );
    if (value) emails.add(value);
    return [...emails].filter(Boolean).map((email) => ({ value: email, label: email }));
  }, [clients.data, value]);

  return (
    <Space orientation="vertical" size={4}>
      <Select
        value={value || undefined}
        onChange={(email) => onChange(email ?? '')}
        onOpenChange={setOpen}
        onSearch={setSearch}
        showSearch
        filterOption={false}
        allowClear
        options={options}
        loading={clients.isFetching}
        placeholder={t('pages.audit.clientFilter')}
        aria-label={t('pages.audit.clientFilter')}
        onPopupScroll={(event) => {
          const target = event.currentTarget;
          if (
            target.scrollTop + target.clientHeight >= target.scrollHeight - 24 &&
            clients.hasNextPage &&
            !clients.isFetching
          )
            void clients.fetchNextPage();
        }}
        style={{ width: 240 }}
      />
      {clients.error && (
        <Alert
          type="error"
          title={clients.error.message}
          action={
            <Button size="small" onClick={() => void clients.refetch()}>
              {t('refresh')}
            </Button>
          }
        />
      )}
    </Space>
  );
}
