import { z } from 'zod';
import { HttpUtil } from '@/utils';
import {
  AISettingsSchema,
  AIDeploymentSchema,
  AIProviderConfigSchema,
  AIStatusSchema,
  AIModelOptionSchema,
  AIConversationSchema,
  AIConversationPageSchema,
  AIConversationViewSchema,
  AITaskSchema,
  AITaskInputSchema,
  AITaskPageSchema,
  AIRunSchema,
  AIRunPageSchema,
} from '@/generated/zod';

async function get<T extends z.ZodType>(path: string, schema: T): Promise<z.infer<T>> {
  const response = await HttpUtil.get(`/panel/api/ai${path}`, undefined, { silent: true });
  if (!response.success) throw new Error(response.msg);
  return schema.parse(response.obj);
}
async function post<T extends z.ZodType>(
  path: string,
  body: unknown,
  schema: T,
): Promise<z.infer<T>> {
  const response = await HttpUtil.post(`/panel/api/ai${path}`, body, {
    silent: true,
    headers: { 'Content-Type': 'application/json' },
  });
  if (!response.success) throw new Error(response.msg);
  return schema.parse(response.obj);
}
export const aiApi = {
  deployment: () => get('/deployment', AIDeploymentSchema),
  deploy: () => post('/deployment', {}, AIDeploymentSchema),
  providerConfig: () => get('/provider-config', AIProviderConfigSchema),
  saveProviderConfig: (value: z.infer<typeof AIProviderConfigSchema>) =>
    post('/provider-config', value, AIProviderConfigSchema),
  settings: () => get('/settings', AISettingsSchema),
  saveSettings: (value: z.infer<typeof AISettingsSchema>) =>
    post('/settings', value, AISettingsSchema),
  status: () => get('/status', AIStatusSchema),
  reconnect: () => post('/reconnect', {}, AIStatusSchema),
  models: () => get('/models', z.array(AIModelOptionSchema)),
  sessions: (page: number) => get(`/sessions?page=${page}`, AIConversationPageSchema),
  createSession: (title: string) => post('/sessions', { title }, AIConversationSchema),
  session: (id: string, page: number) =>
    get(`/sessions/${encodeURIComponent(id)}?page=${page}`, AIConversationViewSchema),
  deleteSession: (id: string) =>
    post(`/sessions/${encodeURIComponent(id)}/delete`, {}, z.unknown()),
  send: (id: string, prompt: string, model: string) =>
    post(`/sessions/${encodeURIComponent(id)}/messages`, { prompt, model }, AIRunSchema),
  cancel: (id: string) => post(`/runs/${encodeURIComponent(id)}/cancel`, {}, z.unknown()),
  tasks: (page: number) => get(`/tasks?page=${page}`, AITaskPageSchema),
  saveTask: (value: z.infer<typeof AITaskInputSchema>) => post('/tasks', value, AITaskSchema),
  deleteTask: (id: string) => post(`/tasks/${encodeURIComponent(id)}/delete`, {}, z.unknown()),
  runTask: (id: string) => post(`/tasks/${encodeURIComponent(id)}/run`, {}, AIRunSchema),
  runs: (page: number, taskId = '') =>
    get(`/runs?page=${page}&taskId=${encodeURIComponent(taskId)}`, AIRunPageSchema),
};
export type AIRun = z.infer<typeof AIRunSchema>;
export type AITask = z.infer<typeof AITaskSchema>;
export type AIModelOption = z.infer<typeof AIModelOptionSchema>;
export const SettingsFormSchema = AISettingsSchema.extend({
  retentionDays: z
    .number()
    .int()
    .min(1, 'pages.ai.retentionLimit')
    .max(3650, 'pages.ai.retentionLimit'),
});
export const TaskFormSchema = AITaskInputSchema.extend({
  name: z.string().trim().min(1, 'pages.ai.required').max(200, 'pages.ai.nameLimit'),
  prompt: z.string().trim().min(1, 'pages.ai.required').max(16000, 'pages.ai.promptLimit'),
  intervalHours: z
    .number()
    .int()
    .min(1, 'pages.ai.intervalLimit')
    .max(8760, 'pages.ai.intervalLimit'),
});
