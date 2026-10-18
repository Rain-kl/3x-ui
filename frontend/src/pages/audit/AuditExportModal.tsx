import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Alert, Button, Input, Modal, Radio, Space } from 'antd';
import { DownloadOutlined } from '@ant-design/icons';

export default function AuditExportModal({
  client,
  onClose,
}: {
  client: string;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const [kind, setKind] = useState('sites');
  const [selectedClient, setClient] = useState(client);
  const params = new URLSearchParams({ kind });
  if (kind === 'client') params.set('client', selectedClient);
  const base = (window.X_UI_BASE_PATH ?? '/').replace(/\/+$/, '');
  return (
    <Modal title={t('pages.audit.exportTitle')} open onCancel={onClose} footer={null}>
      <Space orientation="vertical" style={{ width: '100%' }} size="middle">
        <Alert type="info" title={t('pages.audit.exportDescription')} />
        <Radio.Group value={kind} onChange={(event) => setKind(event.target.value)}>
          <Space orientation="vertical">
            <Radio value="sites">{t('pages.audit.exportSites')}</Radio>
            <Radio value="client">{t('pages.audit.exportClient')}</Radio>
            <Radio value="database">{t('pages.audit.exportDatabase')}</Radio>
          </Space>
        </Radio.Group>
        {kind === 'client' && (
          <Input
            value={selectedClient}
            onChange={(event) => setClient(event.target.value)}
            maxLength={256}
            placeholder={t('pages.audit.exportClientPlaceholder')}
            aria-label={t('pages.audit.exportClientPlaceholder')}
          />
        )}
        <Button
          type="primary"
          icon={<DownloadOutlined />}
          disabled={kind === 'client' && !selectedClient.trim()}
          href={`${base}/panel/api/audit/export?${params}`}
          target="_blank"
          rel="noopener noreferrer"
        >
          {t('pages.audit.exportDownload')}
        </Button>
      </Space>
    </Modal>
  );
}
