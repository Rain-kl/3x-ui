import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Alert, Button, Collapse, Modal, Select, Space, Spin, Switch, Tag, Tooltip } from 'antd';
import { ReloadOutlined } from '@ant-design/icons';

import { HttpUtil } from '@/utils';
import { activateOnKey } from '@/utils/a11y';
import type { Status } from '@/models/status';
import GeodataSection from './GeodataSection';
import './VersionModal.css';

interface BusyEvent {
  busy: boolean;
  tip?: string;
}

interface VersionModalProps {
  open: boolean;
  status: Status;
  onClose: () => void;
  onBusy: (e: BusyEvent) => void;
}

const GEOFILES = [
  'geosite.dat',
  'geoip.dat',
  'geosite_IR.dat',
  'geoip_IR.dat',
  'geosite_RU.dat',
  'geoip_RU.dat',
];

export default function VersionModal({ open, status, onClose, onBusy }: VersionModalProps) {
  const { t } = useTranslation();
  const [modal, modalContextHolder] = Modal.useModal();
  const [activeKey, setActiveKey] = useState<string | string[]>('1');
  const [versions, setVersions] = useState<string[]>([]);
  const [loading, setLoading] = useState(false);
  const [selectedVersion, setSelectedVersion] = useState<string>('');
  const [isLocked, setIsLocked] = useState<boolean>(false);

  const fetchVersions = useCallback(async () => {
    try {
      const msg = await HttpUtil.get<string[]>('/panel/api/server/getXrayVersion');
      if (msg?.success) setVersions(msg.obj || []);
    } finally {
      setLoading(false);
    }
  }, []);

  const [wasOpen, setWasOpen] = useState(false);
  if (open !== wasOpen) {
    setWasOpen(open);
    if (open) {
      setLoading(true);
      const curVer = status?.xray?.version
        ? status.xray.version.startsWith('v')
          ? status.xray.version
          : `v${status.xray.version}`
        : '';
      setSelectedVersion(curVer);
      setIsLocked(!!status?.xray?.versionLock);
    }
  }

  useEffect(() => {
    if (open) void fetchVersions();
  }, [open, fetchVersions]);

  function switchXrayVersion(version: string, lock: boolean) {
    modal.confirm({
      title: t('pages.index.xraySwitchVersionDialog'),
      content: t('pages.index.xraySwitchVersionDialogDesc').replace('#version#', version),
      okText: t('confirm'),
      cancelText: t('cancel'),
      onOk: async () => {
        onClose();
        onBusy({ busy: true, tip: t('pages.index.dontRefresh') });
        try {
          await HttpUtil.post(`/panel/api/server/installXray/${encodeURIComponent(version)}`, {
            lock,
          });
        } finally {
          onBusy({ busy: false });
        }
      },
    });
  }

  function updateGeofile(fileName: string) {
    const isSingle = !!fileName;
    modal.confirm({
      title: t('pages.index.geofileUpdateDialog'),
      content: isSingle
        ? t('pages.index.geofileUpdateDialogDesc').replace('#filename#', fileName)
        : t('pages.index.geofilesUpdateDialogDesc'),
      okText: t('confirm'),
      cancelText: t('cancel'),
      onOk: async () => {
        onClose();
        onBusy({ busy: true, tip: t('pages.index.dontRefresh') });
        const url = isSingle
          ? `/panel/api/server/updateGeofile/${fileName}`
          : '/panel/api/server/updateGeofile';
        try {
          await HttpUtil.post(url);
        } finally {
          onBusy({ busy: false });
        }
      },
    });
  }

  const activeKeyStr = Array.isArray(activeKey) ? activeKey[0] : activeKey;

  const currentVersionStr = status?.xray?.version
    ? status.xray.version.startsWith('v')
      ? status.xray.version
      : `v${status.xray.version}`
    : '';

  const versionOptions = versions.map((v) => ({ label: v, value: v }));
  if (selectedVersion && !versions.includes(selectedVersion)) {
    versionOptions.unshift({ label: `${selectedVersion} (自定义)`, value: selectedVersion });
  }

  return (
    <Modal open={open} title={t('pages.index.xrayUpdates')} footer={null} onCancel={onClose}>
      {modalContextHolder}
      <Spin spinning={loading}>
        <Collapse
          accordion
          activeKey={activeKey}
          onChange={setActiveKey}
          items={[
            {
              key: '1',
              label: 'Xray',
              children: (
                <Space direction="vertical" size="middle" style={{ width: '100%' }}>
                  <Alert type="warning" title={t('pages.index.xraySwitchClickDesk')} showIcon />
                  <div>
                    <div style={{ marginBottom: 6, fontWeight: 500 }}>
                      选择或输入版本：
                      {currentVersionStr && (
                        <span
                          style={{
                            fontWeight: 'normal',
                            color: 'var(--ant-color-text-secondary)',
                            marginLeft: 8,
                          }}
                        >
                          (当前版本: {currentVersionStr})
                        </span>
                      )}
                    </div>
                    <Select
                      showSearch
                      style={{ width: '100%' }}
                      placeholder="请选择或手动输入版本号 (例: v26.7.28)"
                      value={selectedVersion || undefined}
                      onChange={setSelectedVersion}
                      onSearch={(val) => {
                        if (val && !versions.includes(val)) {
                          setSelectedVersion(val);
                        }
                      }}
                      options={versionOptions}
                      filterOption={(input, option) =>
                        (option?.label ?? '').toLowerCase().includes(input.toLowerCase())
                      }
                    />
                  </div>
                  <div
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      justifyContent: 'space-between',
                      padding: '10px 14px',
                      background: 'var(--ant-color-fill-quaternary)',
                      borderRadius: 6,
                    }}
                  >
                    <div>
                      <div style={{ fontWeight: 500 }}>是否锁定版本</div>
                      <div style={{ fontSize: 12, color: 'var(--ant-color-text-secondary)' }}>
                        开启后，更新 3X-UI 面板时不会自动升级 Xray 核心
                      </div>
                    </div>
                    <Switch checked={isLocked} onChange={setIsLocked} />
                  </div>
                  <div style={{ display: 'flex', justifyContent: 'flex-end', marginTop: 8 }}>
                    <Button
                      type="primary"
                      disabled={!selectedVersion}
                      onClick={() => switchXrayVersion(selectedVersion, isLocked)}
                    >
                      确定修改版本
                    </Button>
                  </div>
                </Space>
              ),
            },
            {
              key: '2',
              label: 'Geofiles',
              children: (
                <>
                  <div className="version-list">
                    {GEOFILES.map((file, index) => (
                      <div key={file} className="version-list-item">
                        <Tag color={index % 2 === 0 ? 'purple' : 'green'}>{file}</Tag>
                        <Tooltip title={t('update')}>
                          <ReloadOutlined
                            className="reload-icon"
                            role="button"
                            tabIndex={0}
                            aria-label={t('update')}
                            onClick={() => updateGeofile(file)}
                            onKeyDown={activateOnKey(() => updateGeofile(file))}
                          />
                        </Tooltip>
                      </div>
                    ))}
                  </div>
                  <div className="actions-row">
                    <Button onClick={() => updateGeofile('')}>
                      {t('pages.index.geofilesUpdateAll')}
                    </Button>
                  </div>
                </>
              ),
            },
            {
              key: '3',
              label: t('pages.index.geodataTitle'),
              children: (
                <GeodataSection active={activeKeyStr === '3'} onBusy={onBusy} onClose={onClose} />
              ),
            },
          ]}
        />
      </Spin>
    </Modal>
  );
}
