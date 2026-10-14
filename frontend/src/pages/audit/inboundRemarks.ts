import type { InboundOption } from '@/schemas/client';

export type AuditInboundRemarks = Map<number, Map<string, string>>;

// Match the source node's native tag, not another node's identical tag.
export function buildAuditInboundRemarks(inbounds: readonly InboundOption[]): AuditInboundRemarks {
  const remarks: AuditInboundRemarks = new Map();
  for (const inbound of inbounds) {
    const remark = inbound.remark?.trim();
    if (!inbound.tag || !remark) continue;
    const nodeId = inbound.nodeId ?? 0;
    const prefix = `n${nodeId}-`;
    const tag =
      nodeId > 0 && inbound.tag.startsWith(prefix) ? inbound.tag.slice(prefix.length) : inbound.tag;
    let nodeRemarks = remarks.get(nodeId);
    if (!nodeRemarks) {
      nodeRemarks = new Map();
      remarks.set(nodeId, nodeRemarks);
    }
    nodeRemarks.set(tag, remark);
  }
  return remarks;
}
