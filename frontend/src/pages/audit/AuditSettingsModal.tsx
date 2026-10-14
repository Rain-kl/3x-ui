import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Alert, Button, Divider, Form, InputNumber, Modal, Space, Typography } from 'antd';
import { HttpUtil } from '@/utils';
import type { AuditRetention } from '@/schemas/audit';
import './AuditSettingsModal.css';

export default function AuditSettingsModal({
  policy,
  onClose,
  onChanged,
}: {
  policy: AuditRetention;
  onClose: () => void;
  onChanged: (policy: AuditRetention) => void;
}) {
  const { t } = useTranslation();
  const [modal, contextHolder] = Modal.useModal();
  const [detailHours, setDetailHours] = useState<number | null>(policy.detailHours);
  const [summaryDays, setSummaryDays] = useState<number | null>(policy.summaryDays);
  // FORK(audit-workbench): Rate thresholds share the existing atomic audit settings operation.
  const [connectionRateLimit, setConnectionRateLimit] = useState<number | null>(
    policy.connectionRateLimit,
  );
  const [siteRateLimit, setSiteRateLimit] = useState<number | null>(policy.siteRateLimit);
  const [clearDays, setClearDays] = useState<number | null>(7);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState<{ type: 'success' | 'error'; text: string }>();
  const retentionValid =
    detailHours !== null &&
    summaryDays !== null &&
    Number.isInteger(detailHours) &&
    Number.isInteger(summaryDays) &&
    detailHours >= 1 &&
    detailHours <= 720 &&
    summaryDays >= 1 &&
    summaryDays <= 365 &&
    detailHours <= summaryDays * 24;
  const ratesValid = [connectionRateLimit, siteRateLimit].every(
    (value) => value !== null && Number.isFinite(value) && value >= 0 && value <= 1000000,
  );
  const valid = retentionValid && ratesValid;

  async function perform(path: string, body: unknown, next: AuditRetention) {
    setBusy(true);
    setNotice(undefined);
    try {
      const response = await HttpUtil.post(path, body, {
        headers: { 'Content-Type': 'application/json' },
        silent: true,
      });
      if (!response.success) throw new Error(response.msg || t('pages.audit.operationFailed'));
      setNotice({ type: 'success', text: t('pages.audit.operationComplete') });
      onChanged(next);
    } catch (error) {
      setNotice({ type: 'error', text: error instanceof Error ? error.message : String(error) });
    } finally {
      setBusy(false);
    }
  }

  function save() {
    if (
      !valid ||
      detailHours === null ||
      summaryDays === null ||
      connectionRateLimit === null ||
      siteRateLimit === null
    )
      return;
    const next = { detailHours, summaryDays, connectionRateLimit, siteRateLimit };
    const apply = () => perform('/panel/api/audit/settings', next, next);
    if (detailHours < policy.detailHours || summaryDays < policy.summaryDays) {
      modal.confirm({
        title: t('pages.audit.saveRetention'),
        content: t('pages.audit.retentionReduction'),
        okText: t('confirm'),
        cancelText: t('cancel'),
        onOk: apply,
      });
    } else void apply();
  }

  function clear(days: number) {
    modal.confirm({
      title: t(days === 0 ? 'pages.audit.clearAll' : 'pages.audit.clearOlder'),
      content: t(days === 0 ? 'pages.audit.clearAllConfirm' : 'pages.audit.clearOlderConfirm', {
        days,
      }),
      okText: t('confirm'),
      cancelText: t('cancel'),
      okButtonProps: { danger: true },
      onOk: () => perform('/panel/api/audit/clear', { olderThanDays: days }, policy),
    });
  }

  return (
    <Modal
      open
      centered
      width={760}
      className="audit-settings-modal"
      styles={{ body: { maxHeight: 'calc(100dvh - 160px)', overflowY: 'auto' } }}
      title={t('pages.audit.settings')}
      onCancel={() => {
        if (!busy) onClose();
      }}
      closable={!busy}
      maskClosable={!busy}
      footer={null}
    >
      {contextHolder}
      <Space direction="vertical" size={12} style={{ width: '100%' }}>
        {notice && <Alert type={notice.type} showIcon title={notice.text} />}
        <Alert type="info" showIcon title={t('pages.audit.retentionHelp')} />
        {/* FORK(audit-workbench): Paired fields use the available width while keeping mobile controls readable. */}
        <Form layout="vertical" className="audit-settings-form">
          <div className="audit-settings-fields">
            <Form.Item label={t('pages.audit.detailRetention')}>
              <InputNumber
                min={1}
                max={720}
                precision={0}
                value={detailHours}
                disabled={busy}
                onChange={setDetailHours}
                addonAfter={t('hour')}
              />
            </Form.Item>
            <Form.Item
              label={t('pages.audit.summaryRetention')}
              validateStatus={retentionValid ? undefined : 'error'}
              help={retentionValid ? undefined : t('pages.audit.invalidRetention')}
            >
              <InputNumber
                min={1}
                max={365}
                precision={0}
                value={summaryDays}
                disabled={busy}
                onChange={setSummaryDays}
                addonAfter={t('day')}
              />
            </Form.Item>
          </div>
          {/* FORK(audit-workbench): Explain the shared 60-second rate window and explicit zero-disable semantics. */}
          <Typography.Title level={5} style={{ margin: '0 0 8px' }}>
            {t('pages.audit.frequencySettings')}
          </Typography.Title>
          <Typography.Paragraph type="secondary">
            {t('pages.audit.frequencyHelp')}
          </Typography.Paragraph>
          <div className="audit-settings-fields">
            <Form.Item label={t('pages.audit.connectionRateLimit')}>
              <InputNumber
                min={0}
                max={1000000}
                precision={3}
                value={connectionRateLimit}
                disabled={busy}
                onChange={setConnectionRateLimit}
              />
            </Form.Item>
            <Form.Item
              label={t('pages.audit.siteRateLimit')}
              validateStatus={ratesValid ? undefined : 'error'}
              help={ratesValid ? undefined : t('pages.audit.invalidRateLimit')}
            >
              <InputNumber
                min={0}
                max={1000000}
                precision={3}
                value={siteRateLimit}
                disabled={busy}
                onChange={setSiteRateLimit}
              />
            </Form.Item>
          </div>
          <div className="audit-settings-save">
            <Button type="primary" loading={busy} disabled={!valid} onClick={save}>
              {t('pages.audit.saveSettings')}
            </Button>
          </div>
        </Form>
        <Divider style={{ margin: 0 }} />
        <Typography.Title level={5} style={{ margin: 0 }}>
          {t('pages.audit.maintenance')}
        </Typography.Title>
        <Typography.Text type="secondary">{t('pages.audit.clearHelp')}</Typography.Text>
        <Space wrap>
          <InputNumber
            style={{ width: 140 }}
            min={1}
            max={3650}
            precision={0}
            value={clearDays}
            disabled={busy}
            onChange={setClearDays}
            addonAfter={t('day')}
          />
          <Button
            danger
            disabled={busy || clearDays === null || clearDays < 1 || !Number.isInteger(clearDays)}
            onClick={() => {
              if (clearDays !== null) clear(clearDays);
            }}
          >
            {t('pages.audit.clearOlder')}
          </Button>
          <Button danger disabled={busy} onClick={() => clear(0)}>
            {t('pages.audit.clearAll')}
          </Button>
        </Space>
      </Space>
    </Modal>
  );
}
