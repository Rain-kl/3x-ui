import { lazy } from 'react';
import { BarChartOutlined, MessageOutlined, RobotOutlined, SendOutlined } from '@ant-design/icons';
import type { PanelRoute } from './panelRoutes';

export const FORK_PANEL_ROUTES: PanelRoute[] = [
  {
    path: '/notifications',
    titleKey: 'menu.notifications',
    icon: MessageOutlined,
    component: lazy(() => import('@/pages/notifications/NotificationsPage')),
  },
  {
    path: '/telegram',
    titleKey: 'menu.telegram',
    icon: SendOutlined,
    component: lazy(() => import('@/pages/telegram/TelegramPage')),
  },
  {
    path: '/ai',
    titleKey: 'menu.ai',
    icon: RobotOutlined,
    component: lazy(() => import('@/pages/ai/AIPage')),
  },
  {
    path: '/audit',
    titleKey: 'menu.audit',
    icon: BarChartOutlined,
    component: lazy(() => import('@/pages/audit/AuditPage')),
  },
];

export const FORK_PALETTE_KEYWORDS: Record<string, string[]> = {
  '/notifications': ['notifications', 'announcements', 'subscription'],
  '/telegram': ['telegram', 'support', 'bot', 'bindings'],
  '/ai': ['ai', 'assistant', 'opencode', 'tasks'],
  '/audit': ['audit', 'logs', 'traffic', 'websites', 'bandwidth'],
};
