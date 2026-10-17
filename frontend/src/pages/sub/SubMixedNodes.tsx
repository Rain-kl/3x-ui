import { Alert, Card, Descriptions, Space, Typography } from 'antd';
import { useTranslation } from 'react-i18next';
import type { SubPageData } from './subPageModel';

interface Props {
  nodes: NonNullable<SubPageData['mixedNodes']>;
}

export default function SubMixedNodes({ nodes }: Props) {
  const { t } = useTranslation();
  return (
    <Space orientation="vertical" style={{ width: '100%' }}>
      <Alert type="info" showIcon title={t('subscription.mixedManualHelp')} />
      {nodes.map((node, index) => (
        <Card size="small" title={node.name} key={`${node.address}:${node.port}:${index}`}>
          <Descriptions
            size="small"
            column={1}
            items={[
              {
                key: 'address',
                label: t('address'),
                children: (
                  <Typography.Text copyable>
                    {node.address.includes(':') ? `[${node.address}]` : node.address}:{node.port}
                  </Typography.Text>
                ),
              },
              {
                key: 'username',
                label: t('username'),
                children: <Typography.Text copyable>{node.username}</Typography.Text>,
              },
              {
                key: 'password',
                label: t('password'),
                children: <Typography.Text copyable>{node.password}</Typography.Text>,
              },
              ...(['http', 'socks5'] as const).map((protocol) => ({
                key: protocol,
                label: protocol.toUpperCase(),
                children: (
                  <Typography.Text style={{ wordBreak: 'break-all' }} copyable>
                    {node[protocol]}
                  </Typography.Text>
                ),
              })),
            ]}
          />
        </Card>
      ))}
    </Space>
  );
}
