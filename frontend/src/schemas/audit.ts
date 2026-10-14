import { z } from 'zod';

const AggregateSchema = z.object({
  name: z.string(),
  connections: z.number(),
  uplink: z.number(),
  downlink: z.number(),
  measured: z.number(),
  uniqueSites: z.number().optional(),
});

const EventSchema = z.object({
  id: z.string(),
  nodeId: z.number(),
  time: z.string(),
  inbound: z.string(),
  outbound: z.string(),
  client: z.string(),
  source: z.string(),
  destination: z.string(),
  host: z.string(),
  site: z.string(),
  uplink: z.number().nullable(),
  downlink: z.number().nullable(),
});

export const AuditSummarySchema = z.object({
  centralized: z.boolean().default(false),
  dailyTotal: z.number().default(0),
  days: z
    .array(
      z.object({
        day: z.string(),
        nodeId: z.number(),
        client: z.string(),
        site: z.string(),
        connections: z.number(),
        uplink: z.number(),
        downlink: z.number(),
        measured: z.number(),
      }),
    )
    .default([]),
  connections: z.number(),
  uplink: z.number(),
  downlink: z.number(),
  measured: z.number(),
  sites: z.array(AggregateSchema),
  clients: z.array(AggregateSchema),
  alerts: z.array(AggregateSchema),
  hours: z.array(AggregateSchema),
  events: z.array(EventSchema),
  total: z.number(),
  nodes: z.array(z.object({ id: z.number(), name: z.string() })),
});

export type AuditSummary = z.infer<typeof AuditSummarySchema>;

export const AuditRetentionSchema = z.object({
  // FORK(audit-workbench): Older responses use the new default thresholds until their panel is upgraded.
  connectionRateLimit: z.number().min(0).max(1000000).default(10),
  siteRateLimit: z.number().min(0).max(1000000).default(1),
  detailHours: z.number().int().min(1).max(720),
  summaryDays: z.number().int().min(1).max(365),
});
export type AuditRetention = z.infer<typeof AuditRetentionSchema>;

// FORK(audit-workbench): Fixed-window rates are separate from historical totals and pagination.
const RateCountsSchema = z.object({
  connections: z.number().int().nonnegative(),
  sites: z.number().int().nonnegative(),
});

export const AuditRatesSchema = z.object({
  windowStart: z.string(),
  windowEnd: z.string(),
  current: RateCountsSchema,
  alerts: z.array(RateCountsSchema.extend({ client: z.string() })),
  centralized: z.boolean().default(false),
});

export const AuditRateHistorySchema = z.object({
  windowStart: z.string(),
  windowEnd: z.string(),
  availableFrom: z.string(),
  points: z.array(RateCountsSchema.extend({ time: z.number().int() })),
  centralized: z.boolean().default(false),
});

export type AuditRateHistory = z.infer<typeof AuditRateHistorySchema>;

// FORK(audit-workbench): Validate full site rankings independently of the dashboard's top 20.
export const AuditSitesPageSchema = z.object({
  items: z.array(AggregateSchema),
  total: z.number().int().nonnegative(),
  page: z.number().int().min(1),
  pageSize: z.union([z.literal(20), z.literal(50), z.literal(100)]),
  centralized: z.boolean().default(false),
});
