import { z } from 'zod';
import { TrojanClientSchema } from './trojan';

export const MixedAuthSchema = z.enum(['password', 'noauth']);
export type MixedAuth = z.infer<typeof MixedAuthSchema>;

// SOCKS/HTTP combined inbound. When auth==='noauth' the `accounts` field is
// omitted from the wire payload (the panel writes `undefined`), so we accept
// either an array or absence here.
export const MixedAccountSchema = z.object({
  user: z.string().min(1),
  pass: z.string().min(1),
});
export type MixedAccount = z.infer<typeof MixedAccountSchema>;

// FORK(mixed-clients): Mixed reuses lifecycle fields while keeping its external username separate.
export const MixedClientSchema = TrojanClientSchema.extend({ username: z.string().optional() });
export type MixedClient = z.infer<typeof MixedClientSchema>;

export const MixedInboundSettingsSchema = z.object({
  auth: MixedAuthSchema.default('password'),
  clients: z.array(MixedClientSchema).optional(),
  accounts: z.array(MixedAccountSchema).optional(),
  udp: z.boolean().default(false),
  ip: z.string().default('127.0.0.1'),
});
export type MixedInboundSettings = z.infer<typeof MixedInboundSettingsSchema>;
