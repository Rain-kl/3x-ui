import { z } from 'zod';
import { HttpUtil } from '@/utils';
import {
  NotificationClientSchema,
  NotificationPageSchema,
  SubscriptionNotificationSchema,
} from '@/generated/zod';

export type Notification = z.infer<typeof SubscriptionNotificationSchema>;

export async function notificationList(page: number, pageSize: number) {
  const msg = await HttpUtil.get(
    `/panel/api/notifications?page=${page}&pageSize=${pageSize}`,
    undefined,
    { silent: true },
  );
  if (!msg.success) throw new Error(msg.msg);
  return NotificationPageSchema.parse(msg.obj);
}

export async function notificationClients(search: string, page: number, ids: number[] = []) {
  const query = new URLSearchParams({ search, page: String(page), pageSize: '50' });
  if (ids.length) query.set('ids', ids.join(','));
  const msg = await HttpUtil.get(`/panel/api/notifications/clients?${query}`, undefined, {
    silent: true,
  });
  if (!msg.success) throw new Error(msg.msg);
  return z.array(NotificationClientSchema).parse(msg.obj);
}

export async function saveNotification(body: Notification) {
  const msg = await HttpUtil.post('/panel/api/notifications/save', body, {
    headers: { 'Content-Type': 'application/json' },
    silent: true,
  });
  if (!msg.success) throw new Error(msg.msg);
  return SubscriptionNotificationSchema.parse(msg.obj);
}

export async function deleteNotification(id: number) {
  const msg = await HttpUtil.post(`/panel/api/notifications/${id}/delete`, undefined, {
    silent: true,
  });
  if (!msg.success) throw new Error(msg.msg);
}

export const NotificationFormSchema = SubscriptionNotificationSchema.extend({
  title: z.string().trim().max(120, 'pages.notifications.titleLimit'),
  body: z
    .string()
    .trim()
    .min(1, 'pages.notifications.required')
    .max(4000, 'pages.notifications.bodyLimit'),
  targetType: z.enum(['clients', 'groups', 'both']),
  sortIndex: z
    .number()
    .int()
    .min(0, 'pages.notifications.orderLimit')
    .max(1000000, 'pages.notifications.orderLimit'),
  clientIds: z.array(z.number().int().positive()).max(2000, 'pages.notifications.audienceLimit'),
  groups: z.array(z.string()).max(2000, 'pages.notifications.audienceLimit'),
});

export const emptyNotification = {
  id: 0,
  title: '',
  body: '',
  targetType: 'both',
  clientIds: [],
  groups: [],
  publishToNodes: false,
  published: false,
  sortIndex: 0,
  publishedAt: 0,
  createdAt: 0,
  updatedAt: 0,
} satisfies Notification;
