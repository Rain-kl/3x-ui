import { Card, Progress, Tag } from 'antd';
import { useTranslation } from 'react-i18next';
import { nodePercent } from './subPageModel';
import type { SubNodeLimit } from './subPageModel';

interface SubLimitedNodesProps {
  limitedNodes?: SubNodeLimit[];
}

// SubLimitedNodes displays per-node quota progress bars and depletion tags for subscriber nodes.
export default function SubLimitedNodes({ limitedNodes }: SubLimitedNodesProps) {
  const { t } = useTranslation();

  if (!limitedNodes || limitedNodes.length === 0) {
    return null;
  }

  return (
    <Card className="sub-limited-nodes-card" title={t('subscription.nodeQuota')}>
      <div className="sub-limited-nodes-list">
        {limitedNodes.map((node) => {
          const pct = nodePercent(node.percent);
          return (
            <div
              key={node.name}
              className={`sub-node-limit-item ${node.depleted ? 'is-depleted' : ''}`}
            >
              <div className="sub-node-limit-header">
                <span className="sub-node-limit-name">{node.name}</span>
                <span className="sub-node-limit-usage">
                  <span>
                    {node.used} / {node.total}
                  </span>
                  {node.depleted && (
                    <Tag color="error" className="sub-node-depleted-tag">
                      {t('subscription.depleted')}
                    </Tag>
                  )}
                </span>
              </div>
              <Progress
                percent={pct}
                status={node.depleted ? 'exception' : 'normal'}
                size={['100%', 8]}
              />
            </div>
          );
        })}
      </div>
    </Card>
  );
}
