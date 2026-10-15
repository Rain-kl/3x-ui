export interface RuleRow {
  key: number;
  enabled?: boolean;
  // FORK(routing-rule-sync): Inherited rules retain explicit parent ownership.
  sync?: boolean;
  panelSyncSource?: string;
  comment?: string;
  domain?: string;
  ip?: string;
  port?: string;
  sourcePort?: string;
  vlessRoute?: string;
  network?: string;
  sourceIP?: string;
  user?: string;
  inboundTag?: string;
  protocol?: string;
  attrs?: string;
  outboundTag?: string;
  balancerTag?: string;
}
