import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button, Dropdown, Modal, Select, Space, Table, Tabs, message } from 'antd';
import {
  AimOutlined,
  ControlOutlined,
  ExportOutlined,
  ImportOutlined,
  MoreOutlined,
  PlusOutlined,
  SaveOutlined,
  UnorderedListOutlined,
} from '@ant-design/icons';

import { catTabLabel } from '@/pages/settings/catTabLabel';
import PromptModal from '@/components/feedback/PromptModal';
import TextModal from '@/components/feedback/TextModal';
import { isBalancerLoopbackTag } from '../balancers/balancer-loopback';
import RoutingBasic from './RoutingBasic';
import RouteTester from './RouteTester';
import RuleFormModal from './RuleFormModal';
import type { RoutingRule } from './RuleFormModal';
import RuleCardList from './RuleCardList';
import { useRoutingColumns } from './useRoutingColumns';
import { arrJoin, buildRemarkByTag, originalRuleIndex } from './helpers';
import type { RuleRow } from './types';
import type { XraySettingsValue, SetTemplate } from '@/hooks/useXraySetting';
import { useNodesQuery } from '@/api/queries/useNodesQuery';
import { useInboundOptions } from '@/api/queries/useInboundOptions';
import { HttpUtil } from '@/utils';
import './RoutingTab.css';

interface RoutingTabProps {
  templateSettings: XraySettingsValue | null;
  setTemplateSettings: SetTemplate;
  inboundTags: string[];
  clientReverseTags: string[];
  subscriptionOutboundTags?: string[];
  isMobile: boolean;
}

export default function RoutingTab({
  templateSettings,
  setTemplateSettings,
  clientReverseTags,
  subscriptionOutboundTags,
  isMobile,
}: RoutingTabProps) {
  const { t } = useTranslation();
  const [modal, modalContextHolder] = Modal.useModal();
  const [ruleModalOpen, setRuleModalOpen] = useState(false);
  const [editingRule, setEditingRule] = useState<RoutingRule | null>(null);
  const [editingIndex, setEditingIndex] = useState<number | null>(null);
  const [draggedIndex, setDraggedIndex] = useState<number | null>(null);
  const [dropTargetIndex, setDropTargetIndex] = useState<number | null>(null);
  const [selectedNodeId, setSelectedNodeId] = useState<number>(0);
  const [remoteRules, setRemoteRules] = useState<RoutingRule[]>([]);
  const [remoteLoading, setRemoteLoading] = useState(false);
  const [remoteSaving, setRemoteSaving] = useState(false);

  const { nodes: nodesList = [] } = useNodesQuery();
  const { data: allInboundOptions = [] } = useInboundOptions();

  const nodeInboundOptions = useMemo(() => {
    return allInboundOptions.filter((ib) =>
      selectedNodeId === 0 ? ib.nodeId == null : ib.nodeId === selectedNodeId,
    );
  }, [allInboundOptions, selectedNodeId]);

  const remarkByTag = useMemo(() => buildRemarkByTag(nodeInboundOptions), [nodeInboundOptions]);

  const fetchRemoteRules = useCallback(
    async (nodeId: number) => {
      if (nodeId <= 0) return;
      setRemoteLoading(true);
      try {
        const resp = await HttpUtil.get<{
          xraySetting?: { routing?: { rules?: RoutingRule[] } };
        }>(`/panel/api/xray/?nodeId=${nodeId}`, undefined, { silent: true });
        if (resp?.success && resp.obj?.xraySetting?.routing?.rules) {
          setRemoteRules(resp.obj.xraySetting.routing.rules);
        } else {
          setRemoteRules([]);
        }
      } catch {
        message.error(t('somethingWentWrong'));
      } finally {
        setRemoteLoading(false);
      }
    },
    [t],
  );

  const handleNodeChange = useCallback(
    (val: number) => {
      setSelectedNodeId(val);
      if (val > 0) {
        void fetchRemoteRules(val);
      }
    },
    [fetchRemoteRules],
  );

  const dragRef = useRef<{
    from: number | null;
    to: number | null;
    startY: number;
    moved: boolean;
  }>({
    from: null,
    to: null,
    startY: 0,
    moved: false,
  });

  const activeRules = useMemo(() => {
    if (selectedNodeId > 0) {
      return remoteRules;
    }
    return (templateSettings?.routing?.rules || []) as RoutingRule[];
  }, [selectedNodeId, remoteRules, templateSettings?.routing?.rules]);

  const rulesRef = useRef(activeRules);
  const rowsRef = useRef<RuleRow[]>([]);

  const rows: RuleRow[] = useMemo(
    () =>
      activeRules
        .map((rule, idx) => {
          const r: RuleRow = { key: idx };
          r.enabled = rule.enabled !== false;
          r.domain = arrJoin(rule.domain);
          r.ip = arrJoin(rule.ip);
          r.port = rule.port;
          r.sourcePort = rule.sourcePort;
          r.vlessRoute = rule.vlessRoute;
          r.network = rule.network;
          r.sourceIP = arrJoin(rule.sourceIP);
          r.user = arrJoin(rule.user);
          r.inboundTag = arrJoin(rule.inboundTag);
          r.protocol = arrJoin(rule.protocol);
          if (rule.attrs && typeof rule.attrs === 'object' && !Array.isArray(rule.attrs)) {
            r.attrs = JSON.stringify(rule.attrs, null, 2);
          }
          r.comment = rule.comment || undefined;
          r.outboundTag = rule.outboundTag;
          r.balancerTag = rule.balancerTag;
          return r;
        })
        .filter((r) => {
          const inboundTags = (activeRules[r.key]?.inboundTag || []) as string[];
          return !inboundTags.some(isBalancerLoopbackTag);
        }),
    [activeRules],
  );

  useEffect(() => {
    rulesRef.current = activeRules;
    rowsRef.current = rows;
  });

  const mutate = useCallback(
    (mutator: (nextRules: RoutingRule[]) => void) => {
      if (selectedNodeId > 0) {
        setRemoteRules((prev) => {
          const clone = JSON.parse(JSON.stringify(prev)) as RoutingRule[];
          mutator(clone);
          return clone;
        });
      } else {
        setTemplateSettings((prev) => {
          if (!prev) return prev;
          const clone = JSON.parse(JSON.stringify(prev)) as XraySettingsValue;
          if (!clone.routing) clone.routing = { rules: [] };
          if (!Array.isArray(clone.routing.rules)) clone.routing.rules = [];
          mutator(clone.routing.rules as RoutingRule[]);
          return clone;
        });
      }
    },
    [selectedNodeId, setTemplateSettings],
  );

  const saveRemoteRules = useCallback(async () => {
    if (selectedNodeId <= 0) return;
    setRemoteSaving(true);
    try {
      const payload = {
        nodeId: selectedNodeId,
        xraySetting: JSON.stringify({
          routing: {
            rules: remoteRules,
          },
        }),
      };
      const resp = await HttpUtil.post('/panel/api/xray/update', payload);
      if (resp?.success) {
        message.success(t('pages.settings.toasts.modifySettings'));
      }
    } catch {
      message.error(t('somethingWentWrong'));
    } finally {
      setRemoteSaving(false);
    }
  }, [selectedNodeId, remoteRules, t]);

  const inboundTagOptions = useMemo(() => {
    const seen = new Set<string>();
    const out: string[] = [];
    const push = (tag?: string) => {
      if (!tag || seen.has(tag) || isBalancerLoopbackTag(tag)) return;
      seen.add(tag);
      out.push(tag);
    };
    for (const ib of nodeInboundOptions) {
      if (ib.tag) push(ib.tag);
    }
    if (selectedNodeId === 0) {
      for (const ob of templateSettings?.outbounds || []) {
        const obx = ob as {
          reverse?: { tag?: string };
          settings?: { reverse?: { tag?: string }; inboundTag?: string };
        };
        push(obx?.reverse?.tag || obx?.settings?.reverse?.tag || obx?.settings?.inboundTag);
      }
      push((templateSettings?.dns as { tag?: string } | undefined)?.tag);
      for (const s of (templateSettings?.dns as { servers?: Array<{ tag?: string }> } | undefined)
        ?.servers || []) {
        if (typeof s === 'object' && s?.tag) push(s.tag);
      }
    }
    return out;
  }, [nodeInboundOptions, selectedNodeId, templateSettings]);

  const outboundTagOptions = useMemo(() => {
    const out = new Set<string>(['']);
    for (const ob of templateSettings?.outbounds || []) {
      if (ob?.tag) out.add(ob.tag);
    }
    for (const tag of clientReverseTags || []) {
      if (tag) out.add(tag);
    }
    for (const tag of subscriptionOutboundTags || []) {
      if (tag) out.add(tag);
    }
    return [...out];
  }, [templateSettings?.outbounds, clientReverseTags, subscriptionOutboundTags]);

  const balancerTagOptions = useMemo(() => {
    const out: string[] = [''];
    for (const b of (templateSettings?.routing?.balancers as Array<{ tag?: string }>) || []) {
      if (b?.tag) out.push(b.tag);
    }
    return out;
  }, [templateSettings?.routing?.balancers]);

  const [importOpen, setImportOpen] = useState(false);
  const [exportOpen, setExportOpen] = useState(false);
  const [exportContent, setExportContent] = useState('');

  function exportRules() {
    setExportContent(JSON.stringify(activeRules, null, 2));
    setExportOpen(true);
  }

  function importRules(value: string) {
    let parsed: unknown;
    try {
      parsed = JSON.parse(value);
    } catch {
      message.error(t('pages.xray.importInvalidJson'));
      return;
    }
    const obj = parsed as { rules?: unknown; routing?: { rules?: unknown } };
    const list = Array.isArray(parsed)
      ? parsed
      : Array.isArray(obj?.rules)
        ? obj.rules
        : Array.isArray(obj?.routing?.rules)
          ? obj.routing!.rules
          : null;
    if (!list) {
      message.error(t('pages.xray.importInvalidJson'));
      return;
    }
    mutate((listRules) => {
      listRules.push(...(list as RoutingRule[]));
    });
    setImportOpen(false);
  }

  function openAdd() {
    setEditingRule(null);
    setEditingIndex(null);
    setRuleModalOpen(true);
  }
  function openEdit(idx: number) {
    const target = originalRuleIndex(rowsRef.current, idx);
    setEditingRule(rulesRef.current[target]);
    setEditingIndex(target);
    setRuleModalOpen(true);
  }
  function onRuleConfirm(rule: Record<string, unknown>) {
    if (JSON.stringify(rule).length <= 3) {
      setRuleModalOpen(false);
      return;
    }
    mutate((listRules) => {
      const typed = rule as unknown as RoutingRule;
      if (editingIndex == null) listRules.push(typed);
      else listRules[editingIndex] = typed;
    });
    setRuleModalOpen(false);
  }

  function confirmDelete(idx: number) {
    const target = originalRuleIndex(rowsRef.current, idx);
    modal.confirm({
      title: `${t('delete')} ${t('pages.xray.Routings')} #${idx + 1}?`,
      okText: t('delete'),
      okType: 'danger',
      cancelText: t('cancel'),
      onOk: () =>
        mutate((listRules) => {
          listRules.splice(target, 1);
        }),
    });
  }

  function moveUp(idx: number) {
    if (idx <= 0) return;
    const target = originalRuleIndex(rowsRef.current, idx);
    const prev = originalRuleIndex(rowsRef.current, idx - 1);
    mutate((listRules) => {
      if (!listRules[target] || !listRules[prev]) return;
      [listRules[prev], listRules[target]] = [listRules[target], listRules[prev]];
    });
  }
  function moveDown(idx: number) {
    if (idx >= rowsRef.current.length - 1) return;
    const target = originalRuleIndex(rowsRef.current, idx);
    const next = originalRuleIndex(rowsRef.current, idx + 1);
    mutate((listRules) => {
      if (!listRules[target] || !listRules[next]) return;
      [listRules[next], listRules[target]] = [listRules[target], listRules[next]];
    });
  }
  function toggleRule(idx: number, enabled: boolean) {
    const target = originalRuleIndex(rowsRef.current, idx);
    mutate((listRules) => {
      if (!listRules[target]) return;
      listRules[target].enabled = enabled;
    });
  }

  function onHandlePointerDown(idx: number, ev: React.PointerEvent) {
    if (ev.button != null && ev.button !== 0) return;
    ev.preventDefault();
    try {
      (ev.currentTarget as Element).setPointerCapture(ev.pointerId);
    } catch {
      /* ignore */
    }
    dragRef.current = { from: idx, to: idx, startY: ev.clientY, moved: false };
    setDraggedIndex(idx);
    setDropTargetIndex(idx);

    const onMove = (e: PointerEvent) => {
      const state = dragRef.current;
      if (state.from == null) return;
      if (!state.moved && Math.abs(e.clientY - state.startY) < 5) return;
      state.moved = true;
      const el = document.elementFromPoint(e.clientX, e.clientY);
      if (!el) return;
      const target = el.closest('[data-row-key]');
      if (!target) return;
      const newIdx = Number(target.getAttribute('data-row-key'));
      if (Number.isFinite(newIdx) && newIdx !== state.to) {
        state.to = newIdx;
        setDropTargetIndex(newIdx);
      }
    };

    const onUp = () => {
      document.removeEventListener('pointermove', onMove);
      document.removeEventListener('pointerup', onUp);
      document.removeEventListener('pointercancel', onUp);
      const { from, to, moved } = dragRef.current;
      dragRef.current = { from: null, to: null, startY: 0, moved: false };
      setDraggedIndex(null);
      setDropTargetIndex(null);
      if (!moved || from == null || to == null || from === to) return;
      const fromOrig = originalRuleIndex(rowsRef.current, from);
      const toOrig = originalRuleIndex(rowsRef.current, to);
      mutate((listRules) => {
        const [movedItem] = listRules.splice(fromOrig, 1);
        listRules.splice(toOrig, 0, movedItem);
      });
    };

    document.addEventListener('pointermove', onMove);
    document.addEventListener('pointerup', onUp);
    document.addEventListener('pointercancel', onUp);
  }

  const hasSource = rows.some((r) => r.sourceIP || r.sourcePort || r.vlessRoute);
  const hasBalancer = rows.some((r) => r.balancerTag);

  const desktopColumns = useRoutingColumns({
    isMobile,
    rowsLength: rows.length,
    showSource: hasSource,
    showBalancer: hasBalancer,
    onHandlePointerDown,
    openEdit,
    moveUp,
    moveDown,
    confirmDelete,
    toggleRule,
    remarkByTag,
  });

  const tableScrollX = desktopColumns.reduce((sum, c) => {
    const col = c as { width?: number; hidden?: boolean };
    return col.hidden ? sum : sum + (typeof col.width === 'number' ? col.width : 0);
  }, 0);

  const nodeOptions = useMemo(() => {
    const items = [{ label: t('pages.inbounds.localPanel'), value: 0 }];
    for (const node of nodesList) {
      items.push({
        label: `${node.name || t('pages.inbounds.node')} (${node.address})`,
        value: node.id,
      });
    }
    return items;
  }, [nodesList, t]);

  return (
    <>
      {modalContextHolder}
      <Tabs
        defaultActiveKey="basic"
        items={[
          {
            key: 'basic',
            label: catTabLabel(<ControlOutlined />, t('pages.xray.basicRouting'), isMobile),
            children: (
              <RoutingBasic
                templateSettings={templateSettings}
                setTemplateSettings={setTemplateSettings}
              />
            ),
          },
          {
            key: 'rules',
            label: catTabLabel(<UnorderedListOutlined />, t('pages.xray.Routings'), isMobile),
            children: (
              <Space orientation="vertical" size="middle" style={{ width: '100%' }}>
                <Space wrap>
                  <Select
                    style={{ minWidth: 160 }}
                    value={selectedNodeId}
                    onChange={handleNodeChange}
                    options={nodeOptions}
                  />
                  <Button type="primary" icon={<PlusOutlined />} onClick={openAdd}>
                    {t('pages.xray.Routings')}
                  </Button>
                  {selectedNodeId > 0 && (
                    <Button
                      type="primary"
                      icon={<SaveOutlined />}
                      loading={remoteSaving}
                      onClick={saveRemoteRules}
                    >
                      {t('save')}
                    </Button>
                  )}
                  <Dropdown
                    trigger={['click']}
                    menu={{
                      items: [
                        {
                          key: 'import',
                          icon: <ImportOutlined />,
                          label: t('pages.xray.importRules'),
                          onClick: () => setImportOpen(true),
                        },
                        {
                          key: 'export',
                          icon: <ExportOutlined />,
                          label: t('pages.xray.exportRules'),
                          disabled: activeRules.length === 0,
                          onClick: exportRules,
                        },
                      ],
                    }}
                  >
                    <Button icon={<MoreOutlined />}>{t('more')}</Button>
                  </Dropdown>
                </Space>

                {isMobile ? (
                  <RuleCardList
                    rows={rows}
                    draggedIndex={draggedIndex}
                    dropTargetIndex={dropTargetIndex}
                    onHandlePointerDown={onHandlePointerDown}
                    openEdit={openEdit}
                    moveUp={moveUp}
                    moveDown={moveDown}
                    confirmDelete={confirmDelete}
                    toggleRule={toggleRule}
                    remarkByTag={remarkByTag}
                  />
                ) : (
                  <Table
                    columns={desktopColumns}
                    dataSource={rows}
                    loading={remoteLoading}
                    rowKey={(r) => r.key}
                    pagination={false}
                    scroll={{ x: tableScrollX }}
                    size="small"
                    className="routing-table"
                    onRow={(_record, index) => {
                      const classes: string[] = [];
                      const i = index ?? -1;
                      if (draggedIndex === i) classes.push('row-dragging');
                      if (dropTargetIndex === i && draggedIndex !== i && draggedIndex != null) {
                        classes.push(i > draggedIndex ? 'drop-after' : 'drop-before');
                      }
                      return {
                        className: classes.join(' '),
                        'data-row-key': i,
                      } as React.HTMLAttributes<HTMLElement>;
                    }}
                  />
                )}
              </Space>
            ),
          },
          {
            key: 'tester',
            label: catTabLabel(<AimOutlined />, t('pages.xray.routeTester'), isMobile),
            children: <RouteTester inboundTags={inboundTagOptions} isMobile={isMobile} />,
          },
        ]}
      />
      <RuleFormModal
        open={ruleModalOpen}
        rule={editingRule}
        inboundTags={inboundTagOptions}
        outboundTags={outboundTagOptions}
        balancerTags={balancerTagOptions}
        remarkByTag={remarkByTag}
        onClose={() => setRuleModalOpen(false)}
        onConfirm={onRuleConfirm}
      />
      <PromptModal
        open={importOpen}
        onClose={() => setImportOpen(false)}
        title={t('pages.xray.importRules')}
        okText={t('pages.xray.importRules')}
        type="textarea"
        json
        onConfirm={importRules}
      />
      <TextModal
        open={exportOpen}
        onClose={() => setExportOpen(false)}
        title={t('pages.xray.exportRules')}
        content={exportContent}
        fileName="routing-rules.json"
        json
      />
    </>
  );
}
