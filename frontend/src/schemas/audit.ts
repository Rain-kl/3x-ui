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
  detailHours: z.number().int().min(1).max(720),
  summaryDays: z.number().int().min(1).max(365),
});
export type AuditRetention = z.infer<typeof AuditRetentionSchema>;
