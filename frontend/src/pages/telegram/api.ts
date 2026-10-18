import { z } from 'zod';
import { HttpUtil } from '@/utils';
import {
  BindCodeSchema,
  CustomCommandsSchema,
  TelegramCustomCommandSchema,
  ConversationPageSchema,
  NotificationClientSchema,
  TelegramBindingPageSchema,
  TelegramLogPageSchema,
  TelegramMessagePageSchema,
  TelegramMessageSchema,
  UserTelegramSettingsSchema,
  UserTelegramStatusSchema,
} from '@/generated/zod';

export type TelegramSettings = z.infer<typeof UserTelegramSettingsSchema>;
export type TelegramMessage = z.infer<typeof TelegramMessageSchema>;

export async function getTelegram<T extends z.ZodType>(
  path: string,
  schema: T,
): Promise<z.infer<T>> {
  const result = await HttpUtil.get(`/panel/api/telegram${path}`, undefined, { silent: true });
  if (!result.success) throw new Error(result.msg);
  return schema.parse(result.obj);
}

export async function postTelegram<T extends z.ZodType>(
  path: string,
  body: unknown,
  schema: T,
): Promise<z.infer<T>> {
  const result = await HttpUtil.post(`/panel/api/telegram${path}`, body, {
    ...(body instanceof FormData ? {} : { headers: { 'Content-Type': 'application/json' } }),
    silent: true,
  });
  if (!result.success) throw new Error(result.msg);
  return schema.parse(result.obj);
}

// FORK(telegram-custom-commands): Reuse authenticated Telegram API and generated command contracts.
export type TelegramCustomCommand = z.infer<typeof TelegramCustomCommandSchema>;
export const TelegramCommandFormSchema = TelegramCustomCommandSchema.extend({
  command: z.string().regex(/^\/?[a-zA-Z0-9_]{1,32}$/, 'pages.telegram.commandNameInvalid'),
  description: z
    .string()
    .trim()
    .min(1, 'pages.telegram.commandDescriptionInvalid')
    .max(256, 'pages.telegram.commandDescriptionInvalid'),
  reply: z
    .string()
    .refine(
      (value) => value.trim().length > 0 && Array.from(value).length <= 4096,
      'pages.telegram.commandReplyInvalid',
    ),
});
export const telegramApi = {
  commands: () => getTelegram('/commands', CustomCommandsSchema),
  saveCommands: (body: z.infer<typeof CustomCommandsSchema>) =>
    postTelegram('/commands', body, CustomCommandsSchema),
  status: () => getTelegram('/status', UserTelegramStatusSchema),
  settings: () => getTelegram('/settings', UserTelegramSettingsSchema),
  saveSettings: (body: TelegramSettings) =>
    postTelegram('/settings', body, UserTelegramSettingsSchema),
  conversations: (page: number, search: string) =>
    getTelegram(
      `/conversations?page=${page}&pageSize=20&search=${encodeURIComponent(search)}`,
      ConversationPageSchema,
    ),
  messages: (tgId: number, page: number) =>
    getTelegram(`/messages?tgId=${tgId}&page=${page}&pageSize=50`, TelegramMessagePageSchema),
  send: (tgId: number, text: string, replyId: number) =>
    postTelegram('/messages/send', { tgId, text, replyId }, TelegramMessageSchema),
  upload: (body: FormData) => postTelegram('/messages/upload', body, TelegramMessageSchema),
  read: (tgId: number, throughId: number) =>
    postTelegram(`/conversations/${tgId}/read`, { throughId }, z.unknown()),
  bindings: (page: number) =>
    getTelegram(`/bindings?page=${page}&pageSize=20`, TelegramBindingPageSchema),
  clients: (search: string, page: number) =>
    getTelegram(
      `/clients?page=${page}&pageSize=50&search=${encodeURIComponent(search)}`,
      z.array(NotificationClientSchema),
    ),
  bindCode: (clientId: number) => postTelegram('/bind-code', { clientId }, BindCodeSchema),
  unbind: (tgId: number) => postTelegram(`/bindings/${tgId}/unbind`, undefined, z.unknown()),
  logs: (page: number, size: number, event: string, tgId: string) =>
    getTelegram(
      `/logs?page=${page}&pageSize=${size}&event=${encodeURIComponent(event)}${tgId ? `&tgId=${encodeURIComponent(tgId)}` : ''}`,
      TelegramLogPageSchema,
    ),
  clear: (kind: string, days: number) => postTelegram('/clear', { kind, days }, z.unknown()),
};

export function telegramMediaURL(id: number) {
  return `${(window.X_UI_BASE_PATH ?? '/').replace(/\/+$/, '')}/panel/api/telegram/media/${id}`;
}

export const TelegramSettingsFormSchema = UserTelegramSettingsSchema.extend({
  logDays: z
    .number()
    .int()
    .min(1, 'pages.telegram.retentionLimit')
    .max(3650, 'pages.telegram.retentionLimit'),
  messageDays: z
    .number()
    .int()
    .min(1, 'pages.telegram.retentionLimit')
    .max(3650, 'pages.telegram.retentionLimit'),
  publicSubUrl: z
    .string()
    .trim()
    .refine((value) => {
      if (!value) return true;
      try {
        const url = new URL(value);
        return (
          ['https:', 'http:'].includes(url.protocol) &&
          !url.username &&
          !url.password &&
          !url.search &&
          !url.hash &&
          value.length <= 2048
        );
      } catch {
        return false;
      }
    }, 'pages.telegram.urlError'),
}).superRefine((value, context) => {
  if (value.enabled && !value.publicSubUrl)
    context.addIssue({
      code: 'custom',
      path: ['publicSubUrl'],
      message: 'pages.telegram.required',
    });
  if (value.enabled && !value.token && !value.hasToken)
    context.addIssue({ code: 'custom', path: ['token'], message: 'pages.telegram.required' });
});

export const TelegramComposerSchema = z.object({
  text: z
    .string()
    .trim()
    .min(1, 'pages.telegram.required')
    .max(3500, 'pages.telegram.messageLimit'),
});
